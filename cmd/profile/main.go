// Command profile runs the profile service.
package main

import (
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/profile"
)

func main() {
	platform.Run("profile", ":"+platform.Env("PORT", "8884"), profile.Handler())
}
