// Command inventory runs the room-stock service on Postgres.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

func main() {
	dsn := platform.Env("DATABASE_URL", "postgres://tailhunt:tailhunt@localhost:54389/tailhunt?application_name=tailhunt-inventory")
	db, err := inventory.Open(dsn, platform.EnvInt("DB_MAX_OPEN", 6))
	if err != nil {
		slog.Error("open", "err", err)
		os.Exit(1)
	}
	if err := inventory.Migrate(context.Background(), db); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	s := &inventory.Server{
		DB:    db,
		Quote: &inventory.QuoteClient{URL: platform.Env("QUOTE_URL", "http://localhost:8883"), HTTP: &http.Client{}},
	}
	ctx, stop := context.WithCancel(context.Background())
	go inventory.RunSweeper(ctx, s, 10*time.Second)
	platform.Run("inventory", ":"+platform.Env("PORT", "8882"), s.Handler(), func(context.Context) { stop(); _ = db.Close() })
}
