package inventory

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/quote"
)

func quoteStub(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	inner := (&quote.Server{}).Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		inner.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func postHold(t *testing.T, url string, req HoldRequest) (*http.Response, Hold) {
	t.Helper()
	b, _ := json.Marshal(req)
	resp, err := http.Post(url+"/holds", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var h Hold
	_ = json.NewDecoder(resp.Body).Decode(&h)
	return resp, h
}

func TestHoldTakesARoomAndPricesIt(t *testing.T) {
	db := testDB(t, 4)
	_, _ = db.Exec(`UPDATE room_nights SET rooms_left = 5 WHERE hotel_id = 7 AND night = '2026-12-20'`)
	s := &Server{DB: db, Quote: &QuoteClient{URL: quoteStub(t, 0).URL, HTTP: http.DefaultClient}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	resp, h := postHold(t, srv.URL, HoldRequest{HotelID: 7, Night: "2026-12-20", Guest: "ann", Tier: "gold"})
	if resp.StatusCode != 201 || h.ID == 0 || h.PriceCents != quote.Price(7, "2026-12-20", "gold") {
		t.Fatalf("status %d hold %+v", resp.StatusCode, h)
	}
	var left int
	_ = db.QueryRow(`SELECT rooms_left FROM room_nights WHERE hotel_id = 7 AND night = '2026-12-20'`).Scan(&left)
	if left != 4 {
		t.Fatalf("rooms_left %d, want 4", left)
	}
}

func TestHoldOnTheLastRoomThenSoldOut(t *testing.T) {
	db := testDB(t, 4)
	_, _ = db.Exec(`UPDATE room_nights SET rooms_left = 1 WHERE hotel_id = 8 AND night = '2026-12-21'`)
	s := &Server{DB: db, Quote: &QuoteClient{URL: quoteStub(t, 0).URL, HTTP: http.DefaultClient}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	if resp, _ := postHold(t, srv.URL, HoldRequest{HotelID: 8, Night: "2026-12-21", Guest: "a"}); resp.StatusCode != 201 {
		t.Fatalf("first hold %d", resp.StatusCode)
	}
	if resp, _ := postHold(t, srv.URL, HoldRequest{HotelID: 8, Night: "2026-12-21", Guest: "b"}); resp.StatusCode != 409 {
		t.Fatalf("second hold %d, want 409 sold out", resp.StatusCode)
	}
	if resp, _ := postHold(t, srv.URL, HoldRequest{HotelID: 9999, Night: "2026-12-21", Guest: "b"}); resp.StatusCode != 404 {
		t.Fatalf("unknown hotel %d, want 404", resp.StatusCode)
	}
}

func TestSweepExpiresHoldsAndReturnsRooms(t *testing.T) {
	db := testDB(t, 4)
	_, _ = db.Exec(`UPDATE holds SET status = 'expired' WHERE status = 'held'`) // forget earlier tests
	_, _ = db.Exec(`UPDATE room_nights SET rooms_left = 3 WHERE hotel_id = 9 AND night = '2026-12-22'`)
	s := &Server{DB: db, Quote: &QuoteClient{URL: quoteStub(t, 0).URL, HTTP: http.DefaultClient}, HoldTTL: time.Millisecond}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	for _, g := range []string{"a", "b"} {
		if resp, _ := postHold(t, srv.URL, HoldRequest{HotelID: 9, Night: "2026-12-22", Guest: g}); resp.StatusCode != 201 {
			t.Fatal(resp.StatusCode)
		}
	}
	time.Sleep(20 * time.Millisecond)
	if _, err := Sweep(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	var left, open int
	_ = db.QueryRow(`SELECT rooms_left FROM room_nights WHERE hotel_id = 9 AND night = '2026-12-22'`).Scan(&left)
	_ = db.QueryRow(`SELECT count(*) FROM holds WHERE status = 'held' AND hotel_id = 9`).Scan(&open)
	if left != 3 || open != 0 {
		t.Fatalf("rooms_left %d open holds %d; want 3 and 0", left, open)
	}
}
