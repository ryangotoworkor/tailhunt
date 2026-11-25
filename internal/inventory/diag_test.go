package inventory

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/pgtest"
)

// A hold that waits on a slow quote must show up in pg_stat_activity as
// "idle in transaction" holding the FOR UPDATE: that is the signature the
// runbook tells on-call to look for.
func TestSlowQuoteShowsAsIdleInTransaction(t *testing.T) {
	db := testDB(t, 4)
	diag, err := Open(pgtest.DSN(t, "tailhunt_test", "tailhunt-diag"), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer diag.Close()
	_, _ = db.Exec(`UPDATE room_nights SET rooms_left = 50 WHERE hotel_id = 11 AND night = '2026-12-23'`)
	s := &Server{DB: db, Diag: diag, AppName: "tailhunt-test", Quote: &QuoteClient{URL: quoteStub(t, 600*time.Millisecond).URL, HTTP: http.DefaultClient}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	done := make(chan int)
	go func() {
		resp, _ := postHold(t, srv.URL, HoldRequest{HotelID: 11, Night: "2026-12-23", Guest: "slow"})
		done <- resp.StatusCode
	}()
	time.Sleep(300 * time.Millisecond)
	resp, err := http.Get(srv.URL + "/debug/pg-activity")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Sessions []Session
		Stats    DBStats
	}
	_ = json.NewDecoder(resp.Body).Decode(&body)
	resp.Body.Close()
	found := false
	for _, x := range body.Sessions {
		if x.State == "idle in transaction" && strings.Contains(x.Query, "FOR UPDATE") && x.XactAgeMs > 200 {
			found = true
		}
	}
	if !found {
		t.Fatalf("no idle-in-transaction FOR UPDATE session: %+v", body.Sessions)
	}
	if body.Stats.InUse < 1 || body.Stats.MaxOpen != 4 {
		t.Fatalf("stats %+v", body.Stats)
	}
	if code := <-done; code != 201 {
		t.Fatalf("hold %d", code)
	}
}

func TestPoolWaitIsCountedWhenThePoolIsFull(t *testing.T) {
	db := testDB(t, 1)
	s := &Server{DB: db}
	c, err := s.acquire(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	go func() { time.Sleep(150 * time.Millisecond); c.Close() }()
	before := db.Stats().WaitCount
	c2, err := s.acquire(t.Context(), "test")
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
	st := statsOf(db)
	if st.WaitCount != before+1 || st.WaitDurationMs < 100 {
		t.Fatalf("stats %+v", st)
	}
}
