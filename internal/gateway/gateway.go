// Package gateway is the single entry point: the web UI and the load
// generator talk only to it. It authenticates, forwards to search or
// inventory, and (from step 3 on) owns the ops console.
package gateway

import (
	"net/http"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Server is the gateway.
type Server struct {
	SearchURL, InventoryURL string
	Client                  *http.Client
}

// Handler mounts the public API.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("GET /api/search", s.search)
	return mux
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	s.forward(w, r, s.SearchURL+"/search?"+r.URL.RawQuery)
}

// forward relays a GET to a sibling and copies status and body back.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, url string) {
	req, err := http.NewRequestWithContext(r.Context(), r.Method, url, r.Body)
	if err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	req.Header.Set("Content-Type", r.Header.Get("Content-Type"))
	resp, err := s.Client.Do(req)
	if err != nil {
		platform.Error(w, http.StatusBadGateway, err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.WriteHeader(resp.StatusCode)
	_, _ = copyBody(w, resp)
}
