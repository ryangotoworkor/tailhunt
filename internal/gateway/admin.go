package gateway

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// mountAdmin adds the on-call console API. Every route needs role oncall.
func (s *Server) mountAdmin(mux *http.ServeMux) {
	on := func(h http.HandlerFunc) http.HandlerFunc { return s.authed("oncall", h) }
	mux.HandleFunc("GET /api/admin/fault", on(s.proxy(http.MethodGet, func() string { return s.QuoteURL + "/admin/fault" })))
	mux.HandleFunc("POST /api/admin/fault", on(s.proxy(http.MethodPost, func() string { return s.QuoteURL + "/admin/fault" })))
	mux.HandleFunc("GET /api/admin/mode", on(s.proxy(http.MethodGet, func() string { return s.InventoryURL + "/admin/config" })))
	mux.HandleFunc("POST /api/admin/mode", on(s.proxy(http.MethodPost, func() string { return s.InventoryURL + "/admin/config" })))
	mux.HandleFunc("GET /api/admin/dbstats", on(s.proxy(http.MethodGet, func() string { return s.InventoryURL + "/debug/dbstats" })))
	mux.HandleFunc("GET /api/admin/pg-activity", on(s.proxy(http.MethodGet, func() string { return s.InventoryURL + "/debug/pg-activity" })))
	mux.HandleFunc("GET /api/admin/alerts", on(s.proxy(http.MethodGet, func() string { return s.PromURL + "/api/v1/alerts" })))
	mux.HandleFunc("GET /api/admin/traffic", on(func(w http.ResponseWriter, r *http.Request) { platform.JSON(w, 200, s.Traffic.Get()) }))
	mux.HandleFunc("POST /api/admin/traffic", on(func(w http.ResponseWriter, r *http.Request) {
		var c TrafficConfig
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil || c.SearchRPS < 0 || c.SearchRPS > 200 || c.HoldRPS < 0 || c.HoldRPS > 100 {
			platform.Error(w, http.StatusBadRequest, "search_rps 0..200, hold_rps 0..100")
			return
		}
		s.Traffic.Set(c)
		platform.JSON(w, 200, c)
	}))
	mux.HandleFunc("GET /api/admin/window", on(func(w http.ResponseWriter, r *http.Request) {
		n, _ := strconv.Atoi(r.URL.Query().Get("seconds"))
		if n <= 0 {
			n = 60
		}
		slow := s.Window.Slowest(time.Now().Add(-30 * time.Second))
		out := map[string]any{"series": s.Window.Series(n), "slowest": slow}
		if slow.TraceID != "" && s.Learn != nil {
			out["slowest_url"] = s.Learn.JaegerPublic + "/trace/" + slow.TraceID
		}
		platform.JSON(w, 200, out)
	}))
}

// proxy relays the request body (if any) to a sibling URL.
func (s *Server) proxy(method string, target func() string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body []byte
		if method == http.MethodPost {
			body, _ = io.ReadAll(io.LimitReader(r.Body, 8192))
		}
		u := target()
		if r.URL.RawQuery != "" {
			u += "?" + r.URL.RawQuery
		}
		s.forward(w, r, method, u, body)
	}
}
