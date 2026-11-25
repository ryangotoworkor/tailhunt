// Command quote runs the pricing service.
package main

import (
	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/quote"
)

func main() {
	stop := obs.Init("quote")
	s := &quote.Server{}
	platform.Run("quote", ":"+platform.Env("PORT", "8883"), obs.Wrap("quote", s.Handler()), stop)
}
