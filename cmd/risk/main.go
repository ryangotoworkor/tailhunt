// Command risk runs the risk service.
package main

import (
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/risk"
)

func main() {
	platform.Run("risk", ":"+platform.Env("PORT", "8885"), risk.Handler())
}
