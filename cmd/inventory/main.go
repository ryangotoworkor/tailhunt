// Command inventory runs the room-stock service on Postgres.
package main

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"

	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

func main() {
	stopTrace := obs.Init("inventory")
	dsn := platform.Env("DATABASE_URL", "postgres://tailhunt:tailhunt@localhost:54389/tailhunt?application_name=tailhunt-inventory")
	db, err := inventory.Open(dsn, platform.EnvInt("DB_MAX_OPEN", 6))
	if err != nil {
		slog.Error("open", "err", err)
		os.Exit(1)
	}
	// A separate one-connection pool for diagnostics (pg_stat_activity).
	diag, err := inventory.Open(strings.Replace(dsn, "application_name=tailhunt-inventory", "application_name=tailhunt-diag", 1), 1)
	if err != nil {
		slog.Error("open diag", "err", err)
		os.Exit(1)
	}
	if err := inventory.Migrate(context.Background(), db); err != nil {
		slog.Error("migrate", "err", err)
		os.Exit(1)
	}
	// go_sql_* metrics: max_open, in_use, idle, wait_count_total, wait_duration_seconds_total.
	prometheus.MustRegister(collectors.NewDBStatsCollector(db, "inventory"))
	s := &inventory.Server{
		DB:    db,
		Diag:  diag,
		Quote: &inventory.QuoteClient{URL: platform.Env("QUOTE_URL", "http://localhost:8883"), HTTP: obs.Client(0)},
	}
	if platform.Env("INVENTORY_MODE", "fixed") == "legacy" {
		s.SetConfig(inventory.Legacy)
	}
	ctx, stop := context.WithCancel(context.Background())
	go inventory.RunSweeper(ctx, s, 10*time.Second)
	platform.Run("inventory", ":"+platform.Env("PORT", "8882"), obs.Wrap("inventory", s.Handler()), func(c context.Context) {
		stop()
		stopTrace(c)
		_ = db.Close()
		_ = diag.Close()
	})
}
