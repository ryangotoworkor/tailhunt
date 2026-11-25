// Command quote runs the pricing service.
package main

import (
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/quote"
)

func main() {
	s := &quote.Server{}
	platform.Run("quote", ":"+platform.Env("PORT", "8883"), s.Handler())
}
