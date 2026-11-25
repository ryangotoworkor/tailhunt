package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ryangotoworkor/tailhunt/internal/pgtest"
)

func testDB(t *testing.T, maxOpen int) *sql.DB {
	t.Helper()
	db, err := Open(pgtest.DSN(t, "tailhunt_test", "tailhunt-test"), maxOpen)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestAvailabilityReturnsHotelsFreeForTheWholeStay(t *testing.T) {
	db := testDB(t, 4)
	srv := httptest.NewServer((&Server{DB: db}).Handler())
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/availability?city=kyoto&checkin=2026-12-10&nights=3")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct{ Hotels []Hotel }
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 || len(body.Hotels) != 40 {
		t.Fatalf("status %d, %d hotels; want 200 and the 40 kyoto hotels", resp.StatusCode, len(body.Hotels))
	}
	for _, h := range body.Hotels {
		if h.PriceCents <= 0 || h.RoomsLeft <= 0 || h.Name == "" {
			t.Fatalf("bad row %+v", h)
		}
	}
}

func TestAvailabilityRejectsBadInput(t *testing.T) {
	srv := httptest.NewServer((&Server{}).Handler())
	defer srv.Close()
	for _, q := range []string{"", "city=kyoto", "city=kyoto&checkin=2026-12-10&nights=0", "city=kyoto&checkin=12/10&nights=2"} {
		resp, err := http.Get(srv.URL + "/availability?" + q)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Errorf("%q: status %d, want 400", q, resp.StatusCode)
		}
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	db := testDB(t, 2)
	if err := Migrate(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	var hotels, nights int
	_ = db.QueryRow(`SELECT count(*) FROM hotels`).Scan(&hotels)
	_ = db.QueryRow(`SELECT count(*) FROM room_nights`).Scan(&nights)
	if hotels != 200 || nights != 200*60 {
		t.Fatalf("hotels %d room_nights %d", hotels, nights)
	}
}
