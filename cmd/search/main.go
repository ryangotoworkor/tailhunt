// Command search runs the guest-facing search service.
package main

import (
	"net/http"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/search"
)

func main() {
	s := &search.Server{
		ProfileURL:   platform.Env("PROFILE_URL", "http://localhost:8884"),
		RiskURL:      platform.Env("RISK_URL", "http://localhost:8885"),
		InventoryURL: platform.Env("INVENTORY_URL", "http://localhost:8882"),
		Client:       &http.Client{Timeout: 10 * time.Second},
	}
	platform.Run("search", ":"+platform.Env("PORT", "8881"), s.Handler())
}
