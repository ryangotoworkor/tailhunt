//go:build ignore

// First version of the readiness check in stageTrace
// (internal/gateway/learn.go): it accepted the trace as soon as the
// db.pool.acquire span was in Jaeger. The gateway's own server span ends
// last and is exported last, so the waterfall sometimes started at
// "search GET /search" and the total was one hop short.
package snippets

// @learn:trace-without-root
func (s *Server) stageTrace(ctx context.Context, slow Slow) (any, error) {
	for {
		spans, err := s.fetchTrace(ctx, slow.TraceID)
		var acquire *Span
		for i := range spans {
			if spans[i].Name == "db.pool.acquire" {
				acquire = &spans[i]
			}
		}
		if err == nil && acquire != nil && len(spans) > 0 {
			total := spans[0].DurMs
			return map[string]any{"total_ms": total, "acquire_ms": acquire.DurMs}, nil
		}
		sleep(ctx, time.Second)
	}
}

// @learn:end trace-without-root
