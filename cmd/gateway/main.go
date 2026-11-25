// Command gateway runs the public entry point.
package main

import (
	"net/http"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/gateway"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

func main() {
	s := &gateway.Server{
		SearchURL:    platform.Env("SEARCH_URL", "http://localhost:8881"),
		InventoryURL: platform.Env("INVENTORY_URL", "http://localhost:8882"),
		Client:       &http.Client{Timeout: 10 * time.Second},
	}
	platform.Run("gateway", ":"+platform.Env("PORT", "8880"), s.Handler())
}
