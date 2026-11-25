package gateway

import (
	"testing"
	"time"
)

func TestWindowKeepsPerSecondP99AndTheSlowestTrace(t *testing.T) {
	w := &Window{}
	for i := 1; i <= 100; i++ {
		w.Observe(searchRoute, 200, time.Duration(i)*time.Millisecond, "t"+string(rune('a'+i%26)))
	}
	w.Observe(searchRoute, 503, 900*time.Millisecond, "slowest")
	w.Observe("POST /api/holds", 200, 5*time.Second, "ignored") // only searches count
	pts := w.Series(3)
	last := pts[len(pts)-1]
	if len(pts) != 3 || last.N != 101 || last.Errors != 1 || last.P99 != 100 || last.P50 != 51 {
		t.Fatalf("series %+v", pts)
	}
	if s := w.Slowest(time.Now().Add(-time.Minute)); s.TraceID != "slowest" || s.Status != 503 {
		t.Fatalf("slowest %+v", s)
	}
	r := w.Range(time.Now().Add(-time.Minute), time.Now())
	if r.Requests != 101 || r.MaxMs != 900 {
		t.Fatalf("range %+v", r)
	}
}
