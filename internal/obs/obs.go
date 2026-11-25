// Package obs wires the three signals every service emits: OpenTelemetry
// traces (exported to Jaeger over OTLP/HTTP), Prometheus metrics on
// /metrics, and one JSON line per request in a request log.
package obs

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Init installs the global tracer provider for svc. With no
// OTEL_EXPORTER_OTLP_ENDPOINT (unit tests) spans are created but dropped.
// The returned func flushes and stops the exporter.
func Init(svc string) func(context.Context) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(resource.NewSchemaless(semconv.ServiceName(svc))),
	}
	if ep := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"); ep != "" {
		exp, err := otlptracehttp.New(context.Background())
		if err != nil {
			slog.Error("otlp exporter", "err", err)
		} else {
			// A short batch timeout so a trace is in Jaeger about a second
			// after the request ends; the learn page reads it back.
			opts = append(opts, sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(500*time.Millisecond)))
		}
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	return func(ctx context.Context) { _ = tp.Shutdown(ctx) }
}

// Tracer returns the tracer for package-level spans.
func Tracer() trace.Tracer { return otel.Tracer("github.com/ryangotoworkor/tailhunt") }

// Client is an HTTP client that propagates the trace to the callee.
func Client(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)}
}

var serverDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "tailhunt_http_server_duration_seconds",
	Help:    "Server-side request latency by service, route and status code.",
	Buckets: []float64{.0025, .005, .01, .025, .05, .075, .1, .15, .2, .25, .3, .4, .5, .75, 1, 1.5, 2, 3, 5},
}, []string{"svc", "route", "code"})

// ReqInfo carries per-request facts that handlers discover and the
// request log records (today: time spent waiting for a DB connection).
type ReqInfo struct {
	mu       sync.Mutex
	dbWaitMs float64
}

// AddDBWait records time spent in the connection pool's wait queue.
func (i *ReqInfo) AddDBWait(d time.Duration) {
	if i == nil {
		return
	}
	i.mu.Lock()
	i.dbWaitMs += float64(d.Microseconds()) / 1000
	i.mu.Unlock()
}

type infoKey struct{}

// Info returns the request's ReqInfo (nil outside a wrapped handler).
func Info(ctx context.Context) *ReqInfo { i, _ := ctx.Value(infoKey{}).(*ReqInfo); return i }

// Observer is called once per finished request (the gateway uses it to
// keep a live latency window). Route is the mux pattern.
type Observer func(route string, status int, dur time.Duration, traceID string)

// Wrap mounts /metrics and /debug/pprof/ on mux and returns the mux wrapped
// in tracing, the latency histogram and the request log.
// @learn:obs-wrap
func Wrap(svc string, mux *http.ServeMux, observers ...Observer) http.Handler {
	mux.Handle("GET /metrics", promhttp.Handler())
	mux.HandleFunc("GET /debug/pprof/", pprof.Index)
	mux.HandleFunc("GET /debug/pprof/profile", pprof.Profile)
	logw := NewRequestLog(svc)
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, route := mux.Handler(r)
		if route == "" {
			route = "unmatched"
		}
		info := &ReqInfo{}
		rec := &statusRecorder{ResponseWriter: w, status: 200}
		start := time.Now()
		mux.ServeHTTP(rec, r.WithContext(context.WithValue(r.Context(), infoKey{}, info)))
		dur := time.Since(start)
		if route == "GET /metrics" || route == "GET /healthz" {
			return // scrapes and health checks would drown the log
		}
		serverDuration.WithLabelValues(svc, route, strconv.Itoa(rec.status)).Observe(dur.Seconds())
		tid := trace.SpanContextFromContext(r.Context()).TraceID()
		traceID := ""
		if tid.IsValid() {
			traceID = tid.String()
		}
		info.mu.Lock()
		wait := info.dbWaitMs
		info.mu.Unlock()
		logw.Write(Line{TS: start.UTC(), Svc: svc, Route: route, Status: rec.status, DurMs: ms(dur), DBWaitMs: wait, TraceID: traceID})
		for _, o := range observers {
			o(route, rec.status, dur, traceID)
		}
	})
	return otelhttp.NewHandler(inner, svc, otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
		_, route := mux.Handler(r)
		return route
	}), otelhttp.WithFilter(func(r *http.Request) bool { return r.URL.Path != "/metrics" && r.URL.Path != "/healthz" }))
}

// @learn:end obs-wrap

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}
