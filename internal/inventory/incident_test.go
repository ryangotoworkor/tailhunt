package inventory

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/loadgen"
)

// The incident, in process: quote answers in 300 ms, holds arrive at
// 25/s, and each legacy hold keeps a pooled connection for the whole
// quote call. Six connections serve at most 6/0.3 = 20 holds a second,
// so the pool stays full and every availability query queues behind them.
const (
	incidentPool     = 6
	incidentQuote    = 300 * time.Millisecond
	incidentHoldRPS  = 25
	incidentSearchPS = 40
	incidentFor      = 5 * time.Second
)

type incidentResult struct {
	avail, holds loadgen.Summary
	stats        DBStats
}

func runIncident(t *testing.T, configure func(*Server)) incidentResult {
	t.Helper()
	if testing.Short() {
		t.Skip("load-shaped test; skipped with -short")
	}
	db := testDB(t, incidentPool)
	_, _ = db.Exec(`UPDATE room_nights SET rooms_left = 400`)
	_, _ = db.Exec(`DELETE FROM holds`)
	s := &Server{DB: db, Quote: &QuoteClient{URL: quoteStub(t, incidentQuote).URL, HTTP: &http.Client{}}}
	if configure != nil {
		configure(s)
	}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	client := &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 200}, Timeout: 30 * time.Second}
	defer client.CloseIdleConnections()
	ctx := context.Background()
	holdsDone := make(chan []loadgen.Sample)
	go func() {
		holdsDone <- loadgen.Run(ctx, incidentHoldRPS, incidentFor+time.Second, func(ctx context.Context, i int) int {
			body := fmt.Appendf(nil, `{"hotel_id":%d,"night":"2026-12-%02d","guest":"u%d","tier":"basic"}`, 1+rand.IntN(200), 1+rand.IntN(30), i)
			resp, err := client.Post(srv.URL+"/holds", "application/json", bytes.NewReader(body))
			if err != nil {
				return 0
			}
			resp.Body.Close()
			return resp.StatusCode
		})
	}()
	time.Sleep(time.Second) // let the holds fill the pool first
	avail := loadgen.Run(ctx, incidentSearchPS, incidentFor, func(ctx context.Context, i int) int {
		resp, err := client.Get(fmt.Sprintf("%s/availability?city=kyoto&checkin=2026-12-%02d&nights=2", srv.URL, 1+i%25))
		if err != nil {
			return 0
		}
		resp.Body.Close()
		return resp.StatusCode
	})
	stats := statsOf(db)
	r := incidentResult{avail: loadgen.Summarize(avail), holds: loadgen.Summarize(<-holdsDone), stats: stats}
	t.Logf("availability: n=%d p50=%.1fms p99=%.1fms err=%.2f%% | holds: p99=%.0fms err=%.2f%% | pool wait_count=%d wait=%.0fms",
		r.avail.Requests, r.avail.P50Ms, r.avail.P99Ms, 100*r.avail.ErrorRate, r.holds.P99Ms, 100*r.holds.ErrorRate, stats.WaitCount, stats.WaitDurationMs)
	return r
}

// @learn:test-reproduce
func TestIncidentReproduces_LegacyHoldsStarveAvailability(t *testing.T) {
	r := runIncident(t, func(s *Server) { s.SetConfig(Legacy) })
	if r.avail.P99Ms < 300 {
		t.Fatalf("the reproduction did not reproduce: availability p99 %.1f ms < 300 ms", r.avail.P99Ms)
	}
	if r.stats.WaitCount == 0 {
		t.Fatalf("pool never made anyone wait")
	}
}

// @learn:end test-reproduce

// The fix, layer by layer. Each test turns on exactly one layer (or all of
// them) under the same incident load and asserts what that layer is for.
// TAILHUNT_SABOTAGE=<layer> switches the layer off behind the test's back;
// the matching test must then fail (see scripts/failing-controls.sh).
const fastP99 = 100.0 // ms; availability is ~2 ms when it does not queue

// @learn:test-fixed
func TestFix_AllLayersKeepAvailabilityFast(t *testing.T) {
	r := runIncident(t, func(s *Server) { s.SetConfig(Fixed) })
	if r.avail.P99Ms > fastP99 || r.avail.ErrorRate > 0.01 {
		t.Fatalf("availability p99 %.1f ms (want <= %.0f), errors %.2f%% (want <= 1%%)", r.avail.P99Ms, fastP99, 100*r.avail.ErrorRate)
	}
	if r.holds.ErrorRate > 0.05 {
		t.Fatalf("holds error rate %.2f%%: the fix should not cost the holds", 100*r.holds.ErrorRate)
	}
}

// @learn:end test-fixed

func TestFix_QuoteOutsideTxReleasesTheConnection(t *testing.T) {
	r := runIncident(t, func(s *Server) { s.SetConfig(Config{QuoteOutsideTx: true}) })
	if r.avail.P99Ms > fastP99 {
		t.Fatalf("availability p99 %.1f ms with quote outside the tx; want <= %.0f", r.avail.P99Ms, fastP99)
	}
}

func TestFix_BulkheadReservesConnectionsForAvailability(t *testing.T) {
	// quote still inside the tx: holds are slow and many get 429, but they
	// can only ever take 2 of the 6 connections.
	r := runIncident(t, func(s *Server) { s.SetConfig(Config{Bulkhead: true}) })
	if r.avail.P99Ms > fastP99 || r.avail.ErrorRate > 0.01 {
		t.Fatalf("availability p99 %.1f ms, errors %.2f%% with the bulkhead", r.avail.P99Ms, 100*r.avail.ErrorRate)
	}
}

func TestFix_AcquireTimeoutFailsFastInsteadOfQueueing(t *testing.T) {
	// No other layer: the pool is full and stays full. The timeout cannot
	// make searches succeed; it can make them fail in ~80 ms instead of
	// waiting seconds, which is what keeps the gateway's threads free.
	r := runIncident(t, func(s *Server) { s.SetConfig(Config{AcquireTimeout: true}) })
	if r.avail.MaxMs > 300 {
		t.Fatalf("slowest availability %.0f ms with an 80 ms acquire timeout; want <= 300", r.avail.MaxMs)
	}
	if r.avail.Status[503] == 0 {
		t.Fatalf("expected fast 503s while the pool is full, got %v", r.avail.Status)
	}
}

func TestFix_QuoteGuardBoundsHoldLatencyAndOpensTheBreaker(t *testing.T) {
	db := testDB(t, 4)
	_, _ = db.Exec(`UPDATE room_nights SET rooms_left = 400 WHERE hotel_id = 12`)
	s := &Server{DB: db, Quote: &QuoteClient{URL: quoteStub(t, 2*time.Second).URL, HTTP: &http.Client{}}}
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()
	s.SetConfig(Fixed)
	var took []string
	defer func() { t.Logf("hold latencies against a 2 s quote: %v, breaker %s", took, s.breaker.State()) }()
	for i := 0; i < 6; i++ {
		start := time.Now()
		resp, _ := postHold(t, srv.URL, HoldRequest{HotelID: 12, Night: "2026-12-24", Guest: "g"})
		d := time.Since(start)
		took = append(took, d.Round(time.Millisecond).String())
		if d > 1200*time.Millisecond {
			t.Fatalf("hold %d took %v against a 2 s quote; the 800 ms quote timeout is not there", i, d)
		}
		if resp.StatusCode == 201 {
			t.Fatalf("hold %d succeeded although quote never answered in time", i)
		}
		if i == 5 && d > 100*time.Millisecond {
			t.Fatalf("after 5 failures the breaker should reject at once; hold %d took %v", i, d)
		}
	}
	if st := s.breaker.State(); st != "open" {
		t.Fatalf("breaker %s, want open", st)
	}
}
