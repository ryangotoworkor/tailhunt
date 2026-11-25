package search

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryangotoworkor/tailhunt/internal/inventory"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
	"github.com/ryangotoworkor/tailhunt/internal/profile"
	"github.com/ryangotoworkor/tailhunt/internal/risk"
)

func stub(status int, body any) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { platform.JSON(w, status, body) }))
}

func TestSearchFansOutPricesAndRanks(t *testing.T) {
	prof := stub(200, profile.Profile{User: "ann", Tier: "gold"})
	rsk := stub(200, risk.Score{User: "ann", Score: 3})
	inv := stub(200, map[string]any{"hotels": []inventory.Hotel{
		{ID: 1, Name: "B", Stars: 3, PriceCents: 50000, RoomsLeft: 4},
		{ID: 2, Name: "A", Stars: 4, PriceCents: 40000, RoomsLeft: 9},
		{ID: 3, Name: "C", Stars: 5, PriceCents: 40000, RoomsLeft: 1},
	}})
	defer prof.Close()
	defer rsk.Close()
	defer inv.Close()
	s := &Server{ProfileURL: prof.URL, RiskURL: rsk.URL, InventoryURL: inv.URL, Client: http.DefaultClient}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/search?user=ann&city=kyoto&checkin=2026-12-10&nights=2")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var got Response
	_ = json.NewDecoder(resp.Body).Decode(&got)
	if resp.StatusCode != 200 || len(got.Results) != 3 {
		t.Fatalf("status %d results %d", resp.StatusCode, len(got.Results))
	}
	// gold = 12% off; equal price breaks on more stars first.
	if got.Results[0].ID != 3 || got.Results[1].ID != 2 || got.Results[2].ID != 1 {
		t.Fatalf("order %v", got.Results)
	}
	if got.Results[0].PriceCents != 35200 || !got.Results[0].MemberPrice || !got.Results[0].PayAtHotel {
		t.Fatalf("pricing %+v", got.Results[0])
	}
}

func TestSearchKeepsInventoryBusyAs503(t *testing.T) {
	prof := stub(200, profile.Profile{User: "ann", Tier: "basic"})
	rsk := stub(200, risk.Score{User: "ann"})
	inv := stub(503, map[string]string{"error": "db pool busy"})
	defer prof.Close()
	defer rsk.Close()
	defer inv.Close()
	s := &Server{ProfileURL: prof.URL, RiskURL: rsk.URL, InventoryURL: inv.URL, Client: http.DefaultClient}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, _ := http.Get(srv.URL + "/search?user=ann&city=kyoto&checkin=2026-12-10&nights=2")
	resp.Body.Close()
	if resp.StatusCode != 503 {
		t.Fatalf("status %d, want 503 passed through", resp.StatusCode)
	}
}
