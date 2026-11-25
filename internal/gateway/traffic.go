package gateway

import (
	"bytes"
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/loadgen"
)

// TrafficConfig is the background load the console and the learn page
// switch on: searches from guests, and holds from guests checking out.
type TrafficConfig struct {
	SearchRPS float64 `json:"search_rps"`
	HoldRPS   float64 `json:"hold_rps"`
}

// Traffic drives that load through the gateway's own public API, so it
// crosses exactly the code a real guest's request crosses.
type Traffic struct {
	Base   string // the gateway's own URL
	Token  func() string
	Client *http.Client

	mu     sync.Mutex
	cfg    TrafficConfig
	cancel context.CancelFunc
}

// Set replaces the running load (zero rates stop it).
func (t *Traffic) Set(cfg TrafficConfig) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cancel != nil {
		t.cancel()
		t.cancel = nil
	}
	t.cfg = cfg
	if cfg.SearchRPS <= 0 && cfg.HoldRPS <= 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.cancel = cancel
	tok := t.Token()
	go loadgen.Run(ctx, cfg.SearchRPS, 24*time.Hour, func(ctx context.Context, i int) int {
		return t.send(ctx, http.MethodGet, SearchURL(t.Base, i), nil, tok)
	})
	go loadgen.Run(ctx, cfg.HoldRPS, 24*time.Hour, func(ctx context.Context, i int) int {
		return t.send(ctx, http.MethodPost, t.Base+"/api/holds", HoldBody(i), tok)
	})
}

// Get returns the running config.
func (t *Traffic) Get() TrafficConfig { t.mu.Lock(); defer t.mu.Unlock(); return t.cfg }

func (t *Traffic) send(ctx context.Context, method, url string, body []byte, tok string) int {
	req, _ := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := t.Client.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}

var cities = []string{"hangzhou", "chengdu", "xian", "kyoto", "lisbon"}

// SearchURL is the i-th search of the synthetic guest population.
func SearchURL(base string, i int) string {
	return fmt.Sprintf("%s/api/search?city=%s&checkin=2026-12-%02d&nights=%d", base, cities[i%len(cities)], 1+rand.IntN(25), 1+rand.IntN(3))
}

// HoldBody is the i-th hold: a random hotel night, on behalf of guest u<i>.
func HoldBody(i int) []byte {
	return fmt.Appendf(nil, `{"hotel_id":%d,"night":"2026-12-%02d","as":"u%d"}`, 1+rand.IntN(200), 1+rand.IntN(30), i%5000)
}
