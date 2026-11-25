// Command search runs the guest-facing search service.
package main

import (
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/search"
)

func main() {
	stop := obs.Init("search")
	s := &search.Server{
		ProfileURL:   platform.Env("PROFILE_URL", "http://localhost:8884"),
		RiskURL:      platform.Env("RISK_URL", "http://localhost:8885"),
		InventoryURL: platform.Env("INVENTORY_URL", "http://localhost:8882"),
		Client:       obs.Client(10 * time.Second),
	}
	if impl := platform.Env("SEARCH_RANK", "fixed"); !s.SetRank(impl) {
		panic("SEARCH_RANK must be legacy or fixed")
	}
	platform.Run("search", ":"+platform.Env("PORT", "8881"), obs.Wrap("search", s.Handler()), stop)
}
