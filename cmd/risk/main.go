// Command risk runs the risk service.
package main

import (
	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/risk"
)

func main() {
	stop := obs.Init("risk")
	platform.Run("risk", ":"+platform.Env("PORT", "8885"), obs.Wrap("risk", risk.Handler()), stop)
}
