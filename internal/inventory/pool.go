package inventory

import (
	"context"
	"database/sql"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/ryangotoworkor/tailhunt/internal/obs"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

var acquireSeconds = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "tailhunt_db_acquire_seconds",
	Help:    "Time spent waiting for a pooled connection, by code path.",
	Buckets: []float64{.0005, .001, .0025, .005, .01, .025, .05, .1, .2, .3, .5, .75, 1, 2},
}, []string{"path"})

// acquire checks a connection out of the pool explicitly, so the wait is
// visible: a span named db.pool.acquire in the trace, a histogram in
// Prometheus, and db_wait_ms in the request log. database/sql hides this
// wait inside QueryContext otherwise. The caller must Close the conn.
// @learn:pool-acquire
func (s *Server) acquire(ctx context.Context, path string) (*sql.Conn, error) {
	ctx, span := obs.Tracer().Start(ctx, "db.pool.acquire")
	defer span.End()
	before := s.DB.Stats()
	start := time.Now()
	conn, err := s.DB.Conn(ctx)
	wait := time.Since(start)
	acquireSeconds.WithLabelValues(path).Observe(wait.Seconds())
	obs.Info(ctx).AddDBWait(wait)
	span.SetAttributes(
		attribute.String("db.path", path),
		attribute.Float64("db.pool.wait_ms", float64(wait.Microseconds())/1000),
		attribute.Int("db.pool.in_use_at_start", before.InUse),
		attribute.Int("db.pool.max_open", before.MaxOpenConnections),
	)
	if err != nil {
		span.SetStatus(codes.Error, err.Error())
		return nil, err
	}
	return conn, nil
}

// @learn:end pool-acquire

// DBStats is sql.DBStats as JSON, plus the configured cap.
type DBStats struct {
	MaxOpen        int     `json:"max_open"`
	Open           int     `json:"open"`
	InUse          int     `json:"in_use"`
	Idle           int     `json:"idle"`
	WaitCount      int64   `json:"wait_count"`
	WaitDurationMs float64 `json:"wait_duration_ms"`
	At             string  `json:"at"`
}

// @learn:dbstats
func statsOf(db *sql.DB) DBStats {
	st := db.Stats()
	return DBStats{
		MaxOpen: st.MaxOpenConnections, Open: st.OpenConnections, InUse: st.InUse, Idle: st.Idle,
		WaitCount: st.WaitCount, WaitDurationMs: float64(st.WaitDuration.Microseconds()) / 1000,
		At: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

// @learn:end dbstats

func (s *Server) dbstats(w http.ResponseWriter, _ *http.Request) {
	platform.JSON(w, http.StatusOK, statsOf(s.DB))
}

// Session is one row of pg_stat_activity for this service's connections.
type Session struct {
	PID       int     `json:"pid"`
	State     string  `json:"state"`
	XactAgeMs float64 `json:"xact_age_ms"`
	QueryMs   float64 `json:"query_age_ms"`
	WaitEvent string  `json:"wait_event"`
	Query     string  `json:"query"`
}

// The diagnostic query runs on its own one-connection pool (Server.Diag).
// If it shared the main pool it would queue behind the very requests it
// is trying to explain.
// @learn:pg-activity
const activitySQL = `
SELECT pid, coalesce(state, ''),
       coalesce(extract(epoch FROM now() - xact_start) * 1000, 0),
       coalesce(extract(epoch FROM now() - query_start) * 1000, 0),
       coalesce(wait_event_type || ':' || wait_event, ''),
       left(regexp_replace(query, '\s+', ' ', 'g'), 160)
FROM pg_stat_activity
WHERE datname = current_database() AND application_name = $1 AND pid <> pg_backend_pid()
ORDER BY xact_start NULLS LAST`

// @learn:end pg-activity

func (s *Server) pgActivity(w http.ResponseWriter, r *http.Request) {
	if s.Diag == nil {
		platform.Error(w, http.StatusServiceUnavailable, "no diagnostic pool")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	rows, err := s.Diag.QueryContext(ctx, activitySQL, s.appName())
	if err != nil {
		platform.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()
	out := []Session{}
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.PID, &x.State, &x.XactAgeMs, &x.QueryMs, &x.WaitEvent, &x.Query); err != nil {
			platform.Error(w, http.StatusInternalServerError, err.Error())
			return
		}
		out = append(out, x)
	}
	platform.JSON(w, http.StatusOK, map[string]any{"sessions": out, "stats": statsOf(s.DB)})
}

func (s *Server) appName() string {
	if s.AppName != "" {
		return s.AppName
	}
	return "tailhunt-inventory"
}
