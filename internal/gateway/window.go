package gateway

import (
	"sort"
	"sync"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/loadgen"
)

// Window keeps the last two minutes of search latencies at the gateway,
// one bucket per second of request START time. Bucketing by start (not by
// finish) matters for before/after comparisons: a request that queued for
// 9 s under the old code and finished after the fix belongs to "before". The console's live p99 curve and the learn
// page's "before / after" numbers are read from here.
type Window struct {
	mu      sync.Mutex
	buckets [120]bucket
}

type bucket struct {
	sec  int64
	lat  []float64
	errs int
	slow Slow // slowest request in this second
}

// Slow is a request worth opening in Jaeger.
type Slow struct {
	TraceID string    `json:"trace_id"`
	Ms      float64   `json:"ms"`
	At      time.Time `json:"at"`
	Status  int       `json:"status"`
}

// Point is one second of the curve.
type Point struct {
	T      int64   `json:"t"` // unix seconds
	N      int     `json:"n"`
	P50    float64 `json:"p50_ms"`
	P99    float64 `json:"p99_ms"`
	Errors int     `json:"errors"`
}

const searchRoute = "GET /api/search"

// Observe is an obs.Observer.
// @learn:window-observe
func (w *Window) Observe(route string, status int, d time.Duration, traceID string) {
	if route != searchRoute {
		return
	}
	start := time.Now().Add(-d)
	ms := float64(d.Microseconds()) / 1000
	w.mu.Lock()
	defer w.mu.Unlock()
	b := &w.buckets[start.Unix()%int64(len(w.buckets))]
	if b.sec != start.Unix() {
		if b.sec > start.Unix() {
			return // older than the ring
		}
		*b = bucket{sec: start.Unix()}
	}
	if len(b.lat) < 4000 {
		b.lat = append(b.lat, ms)
	}
	if status >= 500 || status == 0 {
		b.errs++
	}
	// slowest request that still completed: a timed-out one never ran its
	// query, so its trace cannot show where the time would have gone
	if ms > b.slow.Ms && traceID != "" && status < 400 {
		b.slow = Slow{TraceID: traceID, Ms: ms, At: start, Status: status}
	}
}

// @learn:end window-observe

// Series returns the last n seconds, oldest first (seconds without traffic have N=0).
func (w *Window) Series(n int) []Point {
	if n > len(w.buckets) {
		n = len(w.buckets)
	}
	now := time.Now().Unix()
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Point, 0, n)
	for s := now - int64(n) + 1; s <= now; s++ {
		b := w.buckets[s%int64(len(w.buckets))]
		p := Point{T: s}
		if b.sec == s && len(b.lat) > 0 {
			lat := append([]float64(nil), b.lat...)
			sort.Float64s(lat)
			p.N, p.P50, p.P99, p.Errors = len(lat), loadgen.Pct(lat, 50), loadgen.Pct(lat, 99), b.errs
		}
		out = append(out, p)
	}
	return out
}

// Range summarises every request whose second is in [from, to].
func (w *Window) Range(from, to time.Time) loadgen.Summary {
	w.mu.Lock()
	defer w.mu.Unlock()
	var lat []float64
	errs := 0
	for _, b := range w.buckets {
		if b.sec >= from.Unix() && b.sec <= to.Unix() {
			lat = append(lat, b.lat...)
			errs += b.errs
		}
	}
	sort.Float64s(lat)
	s := loadgen.Summary{Requests: len(lat)}
	if len(lat) > 0 {
		s.P50Ms, s.P95Ms, s.P99Ms, s.MaxMs = loadgen.Pct(lat, 50), loadgen.Pct(lat, 95), loadgen.Pct(lat, 99), lat[len(lat)-1]
		s.ErrorRate = float64(int(float64(errs)/float64(len(lat))*10000)) / 10000
	}
	return s
}

// Slowest returns the slowest traced request since t.
func (w *Window) Slowest(since time.Time) Slow {
	w.mu.Lock()
	defer w.mu.Unlock()
	var best Slow
	for _, b := range w.buckets {
		if b.sec >= since.Unix() && b.slow.Ms > best.Ms {
			best = b.slow
		}
	}
	return best
}
