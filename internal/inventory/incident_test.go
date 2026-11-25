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
	r := runIncident(t, nil)
	if r.avail.P99Ms < 300 {
		t.Fatalf("the reproduction did not reproduce: availability p99 %.1f ms < 300 ms", r.avail.P99Ms)
	}
	if r.stats.WaitCount == 0 {
		t.Fatalf("pool never made anyone wait")
	}
}

// @learn:end test-reproduce
