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
	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

func main() {
	stop := obs.Init("gateway")
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
		Client:       obs.Client(10 * time.Second),
		Accounts:     accounts,
		Secret:       []byte(platform.Env("JWT_SECRET", gateway.DevSecret)),
		QuoteURL:     platform.Env("QUOTE_URL", "http://localhost:8883"),
		PromURL:      platform.Env("PROM_URL", "http://localhost:9198"),
		JaegerURL:    platform.Env("JAEGER_QUERY_URL", "http://localhost:16698"),
		Window:       &gateway.Window{},
		Learn: &gateway.Learn{
			AnalysisDir:  platform.Env("ANALYSIS_DIR", "analysis/out"),
			JaegerPublic: platform.Env("JAEGER_PUBLIC_URL", "http://localhost:16698"),
		},
	}
	s.Traffic = &gateway.Traffic{
		Base:   "http://localhost:" + platform.Env("PORT", "8880"),
		Client: &http.Client{Timeout: 10 * time.Second},
		Token: func() string {
			t, _ := s.Issue("loadgen", "oncall", "basic", 24*time.Hour)
			return t
		},
	}
	platform.Run("gateway", ":"+platform.Env("PORT", "8880"), obs.Wrap("gateway", s.Handler(), s.Window.Observe), stop, func(context.Context) { _ = accounts.Close() })
}
