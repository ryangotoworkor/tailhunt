// Package loadgen is an open-model load generator: requests are started
// on a fixed schedule whether or not earlier ones have finished. A closed
// loop (send, wait, send) slows down when the system slows down and hides
// the very tail we are hunting; an open model keeps arriving.
package loadgen

import (
	"context"
	"math"
	"sort"
	"sync"
	"time"
)

// Fire sends one request and reports its HTTP status (0 for a transport error).
type Fire func(ctx context.Context, i int) int

// Sample is one finished request.
type Sample struct {
	Start   time.Time
	Latency time.Duration
	Status  int
}

// Run starts rps requests per second for d, then waits for all of them.
// @learn:open-model
func Run(ctx context.Context, rps float64, d time.Duration, fire Fire) []Sample {
	if rps <= 0 {
		return nil
	}
	every := time.Duration(float64(time.Second) / rps)
	n := int(d / every)
	out := make([]Sample, n)
	var wg sync.WaitGroup
	start := time.Now()
	for i := 0; i < n; i++ {
		at := start.Add(time.Duration(i) * every)
		if wait := time.Until(at); wait > 0 {
			select {
			case <-ctx.Done():
				n = i
				goto done
			case <-time.After(wait):
			}
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			t := time.Now()
			code := fire(ctx, i)
			out[i] = Sample{Start: t, Latency: time.Since(t), Status: code}
		}(i)
	}
done:
	wg.Wait()
	return out[:n]
}

// @learn:end open-model

// Summary is what a load-test run reports.
type Summary struct {
	Requests  int            `json:"requests"`
	P50Ms     float64        `json:"p50_ms"`
	P95Ms     float64        `json:"p95_ms"`
	P99Ms     float64        `json:"p99_ms"`
	MaxMs     float64        `json:"max_ms"`
	ErrorRate float64        `json:"error_rate"` // share of non-2xx/3xx (incl. transport errors)
	Status    map[int]int    `json:"status"`
	Extra     map[string]any `json:"extra,omitempty"`
}

// Summarize computes percentiles (nearest rank) over all samples.
func Summarize(ss []Sample) Summary {
	s := Summary{Requests: len(ss), Status: map[int]int{}}
	if len(ss) == 0 {
		return s
	}
	lat := make([]float64, len(ss))
	errs := 0
	for i, x := range ss {
		lat[i] = float64(x.Latency.Microseconds()) / 1000
		s.Status[x.Status]++
		if x.Status == 0 || x.Status >= 400 {
			errs++
		}
	}
	sort.Float64s(lat)
	s.P50Ms, s.P95Ms, s.P99Ms = Pct(lat, 50), Pct(lat, 95), Pct(lat, 99)
	s.MaxMs = lat[len(lat)-1]
	s.ErrorRate = math.Round(float64(errs)/float64(len(ss))*10000) / 10000
	return s
}

// Pct is the nearest-rank percentile of sorted values.
func Pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	k := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if k < 0 {
		k = 0
	}
	return math.Round(sorted[k]*100) / 100
}
