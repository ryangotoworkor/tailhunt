// Package search is the guest-facing search service. One search fans out
// to profile (tier), risk (pay-at-hotel allowed?) and inventory (what is
// free), then prices and ranks the hotels.
package search

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sync"

	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/profile"
	"github.com/ryangotoworkor/tailhunt/internal/risk"
)

// Server is the search service.
type Server struct {
	ProfileURL, RiskURL, InventoryURL string
	Client                            *http.Client
}

// Response is the search answer.
type Response struct {
	City    string   `json:"city"`
	Tier    string   `json:"tier"`
	Results []Result `json:"results"`
}

// Handler mounts GET /search?city=&checkin=&nights=&user=.
func (s *Server) Handler() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", platform.Health)
	mux.HandleFunc("GET /search", s.search)
	return mux
}

// @learn:search-fanout
func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	city, checkin, _, msg := inventory.ParseStay(r)
	user := r.URL.Query().Get("user")
	if msg != "" || user == "" {
		platform.Error(w, http.StatusBadRequest, "user, city, checkin and nights are required")
		return
	}
	ctx := r.Context()
	var (
		wg       sync.WaitGroup
		prof     profile.Profile
		score    risk.Score
		avail    struct{ Hotels []inventory.Hotel }
		errs     [3]error
		stayArgs = url.Values{"city": {city}, "checkin": {checkin.Format("2006-01-02")}, "nights": {r.URL.Query().Get("nights")}}
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		errs[0] = platform.GetJSON(ctx, s.Client, s.ProfileURL+"/profile?user="+url.QueryEscape(user), &prof)
	}()
	go func() {
		defer wg.Done()
		errs[1] = platform.GetJSON(ctx, s.Client, s.RiskURL+"/score?user="+url.QueryEscape(user)+"&city="+url.QueryEscape(city), &score)
	}()
	go func() {
		defer wg.Done()
		errs[2] = platform.GetJSON(ctx, s.Client, s.InventoryURL+"/availability?"+stayArgs.Encode(), &avail)
	}()
	wg.Wait()
	if err := errors.Join(errs[:]...); err != nil {
		passError(w, err)
		return
	}
	platform.JSON(w, http.StatusOK, Build(city, prof, score, avail.Hotels))
}

// @learn:end search-fanout

// Build prices and ranks the hotels inventory returned.
func Build(city string, prof profile.Profile, score risk.Score, hotels []inventory.Hotel) Response {
	rs := make([]Result, 0, len(hotels))
	for _, h := range hotels {
		rs = append(rs, Result{
			ID: h.ID, Name: h.Name, Stars: h.Stars, RoomsLeft: h.RoomsLeft,
			PriceCents:  memberPrice(h.PriceCents, prof.Tier),
			MemberPrice: prof.Tier != "basic",
			PayAtHotel:  !score.Flagged,
		})
	}
	Rank(rs, prof.Tier)
	return Response{City: city, Tier: prof.Tier, Results: rs}
}

// passError keeps an upstream status (a 503 from inventory stays a 503) so
// the caller can tell "busy" from "broken".
func passError(w http.ResponseWriter, err error) {
	var se *platform.StatusError
	switch {
	case errors.As(err, &se) && se.Status < 500:
		platform.Error(w, http.StatusBadGateway, err.Error())
	case errors.As(err, &se):
		platform.Error(w, se.Status, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		platform.Error(w, http.StatusGatewayTimeout, err.Error())
	default:
		platform.Error(w, http.StatusBadGateway, err.Error())
	}
}
