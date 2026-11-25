//go:build ignore

// First version of Window.Observe (internal/gateway/window.go): requests
// were bucketed by the second they FINISHED. A search that queued for 9 s
// under the legacy code and finished after the fix was counted as "after
// the fix", and the learn page reported p99 9029 ms after the fix.
package snippets

// @learn:window-by-finish
func (w *Window) Observe(route string, status int, d time.Duration, traceID string) {
	if route != searchRoute {
		return
	}
	now := time.Now()
	ms := float64(d.Microseconds()) / 1000
	w.mu.Lock()
	defer w.mu.Unlock()
	b := &w.buckets[now.Unix()%int64(len(w.buckets))]
	if b.sec != now.Unix() {
		*b = bucket{sec: now.Unix()}
	}
	b.lat = append(b.lat, ms)
}

// @learn:end window-by-finish
