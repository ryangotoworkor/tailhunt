// Package gateway is the single entry point: the web UI and the load
// generator talk only to it. It authenticates, forwards to search or
// inventory, and (from step 3 on) owns the ops console.
package gateway

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/url"

	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Server is the gateway.
type Server struct {
	SearchURL, InventoryURL, QuoteURL string
	PromURL, JaegerURL                string
	Client                            *http.Client
	Accounts                          *sql.DB // its own small pool; never shares inventory's
	Secret                            []byte
	Window                            *Window
	Traffic                           *Traffic
	Learn                             *Learn
}

// Handler mounts the public API.
func (s *Server) Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("GET /api/me", s.authed("", func(w http.ResponseWriter, r *http.Request) { platform.JSON(w, 200, ClaimsFrom(r.Context())) }))
	mux.HandleFunc("GET /api/search", s.authed("", s.search))
	mux.HandleFunc("POST /api/holds", s.authed("", s.hold))
	s.mountAdmin(mux)
	s.mountLearn(mux)
	return mux
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("user", ClaimsFrom(r.Context()).Subject) // a guest can only search as themselves
	s.forward(w, r, http.MethodGet, s.SearchURL+"/search?"+q.Encode(), nil)
}

func (s *Server) hold(w http.ResponseWriter, r *http.Request) {
	var in struct {
		HotelID int    `json:"hotel_id"`
		Night   string `json:"night"`
		As      string `json:"as"` // the load generator holds on behalf of many guests
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&in); err != nil {
		platform.Error(w, http.StatusBadRequest, "bad json")
		return
	}
	c := ClaimsFrom(r.Context())
	guest := c.Subject
	if in.As != "" && c.Role == "oncall" {
		guest = in.As
	}
	body, _ := json.Marshal(inventory.HoldRequest{HotelID: in.HotelID, Night: in.Night, Guest: guest, Tier: c.Tier})
	s.forward(w, r, http.MethodPost, s.InventoryURL+"/holds", body)
}

// forward relays a request to a sibling and copies status and body back.
func (s *Server) forward(w http.ResponseWriter, r *http.Request, method, target string, body []byte) {
	if _, err := url.Parse(target); err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	var rd io.Reader
	if body != nil {
		rd = bytesReader(body)
	}
	req, err := http.NewRequestWithContext(r.Context(), method, target, rd)
	if err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
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
