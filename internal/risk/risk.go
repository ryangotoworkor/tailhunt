// Package risk is the risk-scoring service: a cheap score per user and
// city. Search hides "pay at the hotel" for flagged guests.
package risk

import (
	"hash/fnv"
	"net/http"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Score is the risk verdict for one search.
type Score struct {
	User    string `json:"user"`
	Score   int    `json:"score"` // 0..99
	Flagged bool   `json:"flagged"`
}

// Assess is deterministic so tests and replays agree.
func Assess(user, city string) Score {
	h := fnv.New32a()
	_, _ = h.Write([]byte(user + "|" + city))
	s := int(h.Sum32() % 100)
	return Score{User: user, Score: s, Flagged: s >= 95}
}

// Handler serves GET /score?user=&city=.
func Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("GET /score", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("user") == "" {
			platform.Error(w, http.StatusBadRequest, "user is required")
			return
		}
		platform.JSON(w, http.StatusOK, Assess(q.Get("user"), q.Get("city")))
	})
	return mux
}
