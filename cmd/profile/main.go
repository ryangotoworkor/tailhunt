// Command profile runs the profile service.
package main

import (
	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/profile"
)

func main() {
	stop := obs.Init("profile")
	platform.Run("profile", ":"+platform.Env("PORT", "8884"), obs.Wrap("profile", profile.Handler()), stop)
}
