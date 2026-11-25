// Package profile is the user-profile service: who the guest is and which
// membership tier they have. Search uses the tier for member prices.
package profile

import (
	"hash/fnv"
	"net/http"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Profile is what other services need to know about a user.
type Profile struct {
	User     string `json:"user"`
	Tier     string `json:"tier"` // basic | silver | gold
	HomeCity string `json:"home_city"`
}

var known = map[string]Profile{
	"oncall": {User: "oncall", Tier: "gold", HomeCity: "hangzhou"},
	"guest":  {User: "guest", Tier: "basic", HomeCity: "lisbon"},
}

var tiers = []string{"basic", "basic", "silver", "gold"}
var cities = []string{"hangzhou", "chengdu", "xian", "kyoto", "lisbon"}

// Lookup returns the profile for user. Unknown users (the load generator
// uses u1…uN) get a stable profile derived from the name.
func Lookup(user string) Profile {
	if p, ok := known[user]; ok {
		return p
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(user))
	n := h.Sum32()
	return Profile{User: user, Tier: tiers[n%uint32(len(tiers))], HomeCity: cities[(n/7)%uint32(len(cities))]}
}

// Handler serves GET /profile?user=.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("GET /profile", func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if user == "" {
			platform.Error(w, http.StatusBadRequest, "user is required")
			return
		}
		platform.JSON(w, http.StatusOK, Lookup(user))
	})
	return mux
}
