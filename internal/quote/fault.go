package quote

import (
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Fault is the injected slowness: Ratio of requests (0..1) get LatencyMs
// extra. It stands in for whatever makes a real pricing service slow (a
// cold cache, a GC pause, its own database).
type Fault struct {
	LatencyMs int     `json:"latency_ms"`
	Ratio     float64 `json:"ratio"`
}

type faultBox struct {
	mu sync.RWMutex
	f  Fault
}

func (b *faultBox) get() Fault { b.mu.RLock(); defer b.mu.RUnlock(); return b.f }

// @learn:quote-fault
func (b *faultBox) delay() time.Duration {
	f := b.get()
	if f.LatencyMs <= 0 || rand.Float64() >= f.Ratio {
		return 0
	}
	return time.Duration(f.LatencyMs) * time.Millisecond
}

// @learn:end quote-fault

func (b *faultBox) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var f Fault
		if err := json.NewDecoder(r.Body).Decode(&f); err != nil || f.LatencyMs < 0 || f.LatencyMs > 10000 || f.Ratio < 0 || f.Ratio > 1 {
			platform.Error(w, http.StatusBadRequest, "latency_ms 0..10000, ratio 0..1")
			return
		}
		b.mu.Lock()
		b.f = f
		b.mu.Unlock()
	}
	platform.JSON(w, http.StatusOK, b.get())
}
