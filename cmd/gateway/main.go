// Command gateway runs the public entry point.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/gateway"
	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

func main() {
	dsn := platform.Env("DATABASE_URL", "postgres://tailhunt:tailhunt@localhost:54389/tailhunt?application_name=tailhunt-gateway")
	accounts, err := inventory.Open(dsn, 2)
	if err != nil {
		slog.Error("open", "err", err)
		os.Exit(1)
	}
	if err := gateway.MigrateAccounts(context.Background(), accounts); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	s := &gateway.Server{
		SearchURL:    platform.Env("SEARCH_URL", "http://localhost:8881"),
		InventoryURL: platform.Env("INVENTORY_URL", "http://localhost:8882"),
		Client:       &http.Client{Timeout: 10 * time.Second},
		Accounts:     accounts,
		Secret:       []byte(platform.Env("JWT_SECRET", gateway.DevSecret)),
	}
	platform.Run("gateway", ":"+platform.Env("PORT", "8880"), s.Handler(), func(context.Context) { _ = accounts.Close() })
}
