package loadgen

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunKeepsTheScheduleWhenRequestsAreSlow(t *testing.T) {
	var inflight, peak atomic.Int32
	start := time.Now()
	ss := Run(context.Background(), 100, 500*time.Millisecond, func(ctx context.Context, i int) int {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(200 * time.Millisecond) // each request is slower than the gap between starts
		inflight.Add(-1)
		return 200
	})
	if len(ss) != 50 {
		t.Fatalf("%d samples, want 50", len(ss))
	}
	// open model: ~20 requests overlap; a closed loop would have peak 1 and take 10 s
	if peak.Load() < 10 || time.Since(start) > 1500*time.Millisecond {
		t.Fatalf("peak in flight %d, took %v", peak.Load(), time.Since(start))
	}
}

func TestSummarizePercentilesAndErrors(t *testing.T) {
	var ss []Sample
	for i := 1; i <= 100; i++ {
		code := 200
		if i > 97 {
			code = 503
		}
		ss = append(ss, Sample{Latency: time.Duration(i) * time.Millisecond, Status: code})
	}
	s := Summarize(ss)
	if s.P50Ms != 50 || s.P99Ms != 99 || s.MaxMs != 100 || s.ErrorRate != 0.03 || s.Status[503] != 3 {
		t.Fatalf("%+v", s)
	}
}
