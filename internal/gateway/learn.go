package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/loadgen"
	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// The learn page walks one investigation on the live stack. Each stage is
// real work against the real services (inject the fault, wait for
// Prometheus to fire, pull the slowest trace from Jaeger, read
// sql.DBStats and pg_stat_activity, flip the fix, measure again). The page
// polls GET /api/learn/runs/{id}; the run pauses after every stage until
// the page calls POST .../next, so the viewer reads one step at a time.

// Stage is one step of the investigation.
type Stage struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"` // pending | active | done | failed
	Started  time.Time `json:"started_at,omitzero"`
	Finished time.Time `json:"finished_at,omitzero"`
	Data     any       `json:"data,omitempty"`
	Error    string    `json:"error,omitempty"`
}

// Run is one walk through the stages.
type Run struct {
	ID       string    `json:"id"`
	Started  time.Time `json:"started_at"`
	Stages   []*Stage  `json:"stages"`
	Waiting  bool      `json:"waiting"` // paused after a stage, until /next
	Finished bool      `json:"finished"`

	next   chan struct{}
	cancel chan struct{}
}

// StageIDs are the stages in order (the page and the recorder use the ids).
var StageIDs = []struct{ ID, Title string }{
	{"alert", "告警触发"},
	{"p99", "看 p99 曲线"},
	{"trace", "打开一条慢 trace"},
	{"dbstats", "看 sql.DBStats"},
	{"culprit", "找到占着连接的事务"},
	{"fix", "打开修复"},
	{"verify", "修复前后对比"},
	{"history", "告警历史里的排名"},
	{"runbook", "沉淀成 runbook"},
}

// Learn holds the current run.
type Learn struct {
	mu  sync.Mutex
	run *Run
	n   int
	// StageTimeout bounds each stage; AnalysisDir holds summary.json.
	StageTimeout time.Duration
	AnalysisDir  string
	JaegerPublic string // URL the browser can open
	HealthyFor   time.Duration
}

func (s *Server) mountLearn(mux *http.ServeMux) {
	on := func(h http.HandlerFunc) http.HandlerFunc { return s.authed("oncall", h) }
	mux.HandleFunc("POST /api/learn/runs", on(s.learnStart))
	mux.HandleFunc("GET /api/learn/runs/{id}", on(s.learnGet))
	mux.HandleFunc("POST /api/learn/runs/{id}/next", on(s.learnNext))
	mux.HandleFunc("POST /api/learn/runs/{id}/cancel", on(s.learnCancel))
	mux.HandleFunc("GET /api/analysis/summary", on(s.analysisSummary))
}

func (s *Server) learnStart(w http.ResponseWriter, r *http.Request) {
	l := s.Learn
	l.mu.Lock()
	if l.run != nil && !l.run.Finished {
		id := l.run.ID
		l.mu.Unlock()
		platform.JSON(w, http.StatusConflict, map[string]string{"error": "a run is in progress", "id": id})
		return
	}
	l.n++
	run := &Run{ID: fmt.Sprintf("run-%d", l.n), Started: time.Now().UTC(), next: make(chan struct{}, 1), cancel: make(chan struct{})}
	for _, st := range StageIDs {
		run.Stages = append(run.Stages, &Stage{ID: st.ID, Title: st.Title, Status: "pending"})
	}
	l.run = run
	l.mu.Unlock()
	go s.drive(run)
	platform.JSON(w, http.StatusCreated, s.snapshot(run))
}

func (s *Server) learnGet(w http.ResponseWriter, r *http.Request) {
	l := s.Learn
	l.mu.Lock()
	run := l.run
	l.mu.Unlock()
	if run == nil || (r.PathValue("id") != "latest" && r.PathValue("id") != run.ID) {
		platform.Error(w, http.StatusNotFound, "no such run")
		return
	}
	platform.JSON(w, http.StatusOK, s.snapshot(run))
}

func (s *Server) learnNext(w http.ResponseWriter, r *http.Request) {
	l := s.Learn
	l.mu.Lock()
	run := l.run
	l.mu.Unlock()
	if run == nil || r.PathValue("id") != run.ID {
		platform.Error(w, http.StatusNotFound, "no such run")
		return
	}
	select {
	case run.next <- struct{}{}:
	default:
	}
	platform.JSON(w, http.StatusOK, s.snapshot(run))
}

// learnCancel abandons a paused run (a page reload, a recorder restart);
// the driver stops at its next pause and switches the load and fault off.
func (s *Server) learnCancel(w http.ResponseWriter, r *http.Request) {
	l := s.Learn
	l.mu.Lock()
	run := l.run
	if run != nil && r.PathValue("id") == run.ID && !run.Finished {
		select {
		case <-run.cancel:
		default:
			close(run.cancel)
		}
	}
	l.mu.Unlock()
	if run == nil {
		platform.Error(w, http.StatusNotFound, "no such run")
		return
	}
	platform.JSON(w, http.StatusOK, map[string]string{"cancelled": run.ID})
}

// snapshot copies the run under the lock so the JSON encoder never races
// the driver goroutine.
func (s *Server) snapshot(run *Run) Run {
	s.Learn.mu.Lock()
	defer s.Learn.mu.Unlock()
	cp := *run
	cp.Stages = make([]*Stage, len(run.Stages))
	for i, st := range run.Stages {
		c := *st
		cp.Stages[i] = &c
	}
	return cp
}

func (s *Server) set(run *Run, f func()) { s.Learn.mu.Lock(); f(); s.Learn.mu.Unlock() }

// drive runs the stages one after another, pausing between them.
func (s *Server) drive(run *Run) {
	defer s.cleanup()
	steps := map[string]func(context.Context, *learnCtx) (any, error){
		"alert": s.stageAlert, "p99": s.stageP99, "trace": s.stageTrace, "dbstats": s.stageDBStats,
		"culprit": s.stageCulprit, "fix": s.stageFix, "verify": s.stageVerify, "history": s.stageHistory, "runbook": s.stageRunbook,
	}
	lc := &learnCtx{}
	for i, st := range run.Stages {
		s.set(run, func() { st.Status, st.Started = "active", time.Now().UTC() })
		ctx, cancel := context.WithTimeout(context.Background(), s.Learn.timeout())
		data, err := steps[st.ID](ctx, lc)
		cancel()
		s.set(run, func() {
			st.Finished, st.Data = time.Now().UTC(), data
			if err != nil {
				st.Status, st.Error = "failed", err.Error()
			} else {
				st.Status = "done"
			}
		})
		if err != nil || i == len(run.Stages)-1 {
			break
		}
		s.set(run, func() { run.Waiting = true })
		select {
		case <-run.next:
		case <-run.cancel:
			s.set(run, func() { run.Waiting = false; run.Finished = true })
			return
		case <-time.After(10 * time.Minute): // nobody is watching; do not leave the fault on forever
			s.set(run, func() { run.Waiting = false; st.Error = "abandoned" })
			s.set(run, func() { run.Finished = true })
			return
		}
		s.set(run, func() { run.Waiting = false })
	}
	s.set(run, func() { run.Finished = true })
}

func (l *Learn) timeout() time.Duration {
	if l.StageTimeout > 0 {
		return l.StageTimeout
	}
	return 90 * time.Second
}

// learnCtx carries facts from one stage to the next.
type learnCtx struct {
	trafficOn, faultOn, fixedAt time.Time
	spikeAt                     time.Time
	before                      loadgen.Summary
	beforeStats                 map[string]any
}

// cleanup stops the load and the fault; the fix stays on.
func (s *Server) cleanup() {
	s.Traffic.Set(TrafficConfig{})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = platform.PostJSON(ctx, s.Client, s.QuoteURL+"/admin/fault", map[string]any{"latency_ms": 0, "ratio": 0}, nil)
}

// @learn:stage-alert
func (s *Server) stageAlert(ctx context.Context, lc *learnCtx) (any, error) {
	if err := platform.PostJSON(ctx, s.Client, s.InventoryURL+"/admin/config", map[string]string{"mode": "legacy"}, nil); err != nil {
		return nil, fmt.Errorf("set legacy mode: %w", err)
	}
	_ = platform.PostJSON(ctx, s.Client, s.QuoteURL+"/admin/fault", map[string]any{"latency_ms": 0, "ratio": 0}, nil)
	traffic := TrafficConfig{SearchRPS: 30, HoldRPS: 25}
	s.Traffic.Set(traffic)
	lc.trafficOn = time.Now()
	healthy := s.Learn.HealthyFor
	if healthy == 0 {
		healthy = 8 * time.Second
	}
	if err := sleep(ctx, healthy); err != nil { // a healthy stretch first, so the curve has a "before"
		return nil, err
	}
	fault := map[string]any{"latency_ms": 300, "ratio": 1}
	if err := platform.PostJSON(ctx, s.Client, s.QuoteURL+"/admin/fault", fault, nil); err != nil {
		return nil, fmt.Errorf("inject fault: %w", err)
	}
	lc.faultOn = time.Now()
	for {
		var resp struct {
			Data struct {
				Alerts []struct {
					Labels      map[string]string `json:"labels"`
					Annotations map[string]string `json:"annotations"`
					State       string            `json:"state"`
					ActiveAt    time.Time         `json:"activeAt"`
					Value       string            `json:"value"`
				} `json:"alerts"`
			} `json:"data"`
		}
		if err := platform.GetJSON(ctx, s.Client, s.PromURL+"/api/v1/alerts", &resp); err == nil {
			var firing []string
			var hit any
			for _, a := range resp.Data.Alerts {
				if a.State != "firing" {
					continue
				}
				firing = append(firing, a.Labels["alertname"])
				if a.Labels["alertname"] == "SearchP99High" {
					hit = map[string]any{"alertname": "SearchP99High", "state": a.State, "active_at": a.ActiveAt, "severity": a.Labels["severity"],
						"summary": a.Annotations["summary"], "runbook": a.Annotations["runbook"], "value": a.Value}
				}
			}
			if hit != nil {
				return map[string]any{"alert": hit, "firing": firing, "fault": fault, "traffic": traffic, "mode": "legacy",
					"seconds_to_fire": int(time.Since(lc.faultOn).Seconds())}, nil
			}
		}
		if err := sleep(ctx, time.Second); err != nil {
			return nil, fmt.Errorf("SearchP99High did not fire: %w", err)
		}
	}
}

// @learn:end stage-alert

// @learn:stage-p99
func (s *Server) stageP99(ctx context.Context, lc *learnCtx) (any, error) {
	n := int(time.Since(lc.trafficOn).Seconds()) + 2
	if n > 90 {
		n = 90
	}
	// The newest two seconds are still filling (buckets are by start time
	// and slow requests have not finished), so leave them out.
	series := s.Window.Series(n)
	series = series[:max(0, len(series)-2)]
	var calm []float64
	peak := 0.0
	for _, p := range series {
		if p.N == 0 {
			continue
		}
		at := time.Unix(p.T, 0)
		if at.Before(lc.faultOn) {
			calm = append(calm, p.P99)
		}
		if p.P99 > 300 && lc.spikeAt.IsZero() {
			lc.spikeAt = at
		}
		peak = max(peak, p.P99)
	}
	sort.Float64s(calm)
	return map[string]any{"series": series, "fault_at": lc.faultOn.Unix(), "spike_at": lc.spikeAt.Unix(),
		"calm_p99_ms": loadgen.Pct(calm, 50), "peak_p99_ms": peak}, nil
}

// @learn:end stage-p99

// Span is a trace span reduced to what the page shows.
type Span struct {
	Service string  `json:"service"`
	Name    string  `json:"name"`
	StartMs float64 `json:"start_ms"`
	DurMs   float64 `json:"dur_ms"`
	Depth   int     `json:"depth"`
}

// @learn:stage-trace
func (s *Server) stageTrace(ctx context.Context, lc *learnCtx) (any, error) {
	slow := s.Window.Slowest(lc.faultOn)
	if slow.TraceID == "" {
		return nil, fmt.Errorf("no slow search recorded since the fault")
	}
	for {
		spans, err := s.fetchTrace(ctx, slow.TraceID)
		var acquire *Span
		root := false
		for i := range spans {
			if spans[i].Name == "db.pool.acquire" {
				acquire = &spans[i]
			}
			root = root || (spans[i].Service == "gateway" && spans[i].Depth == 0)
		}
		// The gateway's server span ends last, so it reaches Jaeger last:
		// wait for it, or the waterfall would start one hop down.
		if err == nil && acquire != nil && root {
			total := spans[0].DurMs
			return map[string]any{"trace_id": slow.TraceID, "jaeger_url": s.Learn.JaegerPublic + "/trace/" + slow.TraceID,
				"total_ms": total, "acquire_ms": acquire.DurMs, "acquire_share": round3(acquire.DurMs / total), "spans": spans, "status": slow.Status}, nil
		}
		if err := sleep(ctx, time.Second); err != nil { // the exporter batches for up to 500 ms
			return nil, fmt.Errorf("trace %s not complete in Jaeger: %w", slow.TraceID, err)
		}
	}
}

// @learn:end stage-trace

func round3(x float64) float64 { return float64(int(x*1000+0.5)) / 1000 }

// fetchTrace reads a trace from Jaeger's query API and flattens it into
// spans ordered by start time, with their depth in the call tree.
func (s *Server) fetchTrace(ctx context.Context, id string) ([]Span, error) {
	var resp struct {
		Data []struct {
			Spans []struct {
				SpanID     string `json:"spanID"`
				Operation  string `json:"operationName"`
				StartTime  int64  `json:"startTime"`
				Duration   int64  `json:"duration"`
				ProcessID  string `json:"processID"`
				References []struct {
					RefType string `json:"refType"`
					SpanID  string `json:"spanID"`
				} `json:"references"`
			} `json:"spans"`
			Processes map[string]struct {
				ServiceName string `json:"serviceName"`
			} `json:"processes"`
		} `json:"data"`
	}
	if err := platform.GetJSON(ctx, s.Client, s.JaegerURL+"/api/traces/"+id, &resp); err != nil {
		return nil, err
	}
	if len(resp.Data) == 0 {
		return nil, fmt.Errorf("trace not found yet")
	}
	t := resp.Data[0]
	parent := map[string]string{}
	for _, sp := range t.Spans {
		for _, r := range sp.References {
			if r.RefType == "CHILD_OF" {
				parent[sp.SpanID] = r.SpanID
			}
		}
	}
	depth := func(id string) int {
		d := 0
		for p, ok := parent[id]; ok && d < 20; p, ok = parent[p] {
			d++
		}
		return d
	}
	sort.Slice(t.Spans, func(i, j int) bool { return t.Spans[i].StartTime < t.Spans[j].StartTime })
	var t0 int64
	out := []Span{}
	for i, sp := range t.Spans {
		if i == 0 {
			t0 = sp.StartTime
		}
		name := sp.Operation
		if strings.HasPrefix(name, "HTTP ") { // client spans: the server span right below says the same
			continue
		}
		out = append(out, Span{Service: t.Processes[sp.ProcessID].ServiceName, Name: name,
			StartMs: float64(sp.StartTime-t0) / 1000, DurMs: float64(sp.Duration) / 1000, Depth: depth(sp.SpanID)})
	}
	return out, nil
}

// @learn:stage-dbstats
func (s *Server) stageDBStats(ctx context.Context, lc *learnCtx) (any, error) {
	var a, b map[string]any
	if err := platform.GetJSON(ctx, s.Client, s.InventoryURL+"/debug/dbstats", &a); err != nil {
		return nil, err
	}
	if err := sleep(ctx, time.Second); err != nil {
		return nil, err
	}
	if err := platform.GetJSON(ctx, s.Client, s.InventoryURL+"/debug/dbstats", &b); err != nil {
		return nil, err
	}
	num := func(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }
	return map[string]any{"first": a, "second": b,
		"wait_count_per_s":  num(b, "wait_count") - num(a, "wait_count"),
		"wait_ms_per_s":     round3(num(b, "wait_duration_ms") - num(a, "wait_duration_ms")),
		"saturated":         num(b, "in_use") >= num(b, "max_open"),
		"in_use":            num(b, "in_use"),
		"max_open":          num(b, "max_open"),
		"avg_wait_per_wait": round3((num(b, "wait_duration_ms") - num(a, "wait_duration_ms")) / max(1, num(b, "wait_count")-num(a, "wait_count"))),
	}, nil
}

// @learn:end stage-dbstats

// @learn:stage-culprit
func (s *Server) stageCulprit(ctx context.Context, lc *learnCtx) (any, error) {
	var act struct {
		Sessions []map[string]any `json:"sessions"`
	}
	if err := platform.GetJSON(ctx, s.Client, s.InventoryURL+"/debug/pg-activity", &act); err != nil {
		return nil, err
	}
	var cfg map[string]any
	_ = platform.GetJSON(ctx, s.Client, s.InventoryURL+"/admin/config", &cfg)
	idle, oldest, query := 0, 0.0, ""
	for _, x := range act.Sessions {
		if x["state"] == "idle in transaction" {
			idle++
			if age, _ := x["xact_age_ms"].(float64); age > oldest {
				oldest, query = age, fmt.Sprint(x["query"])
			}
		}
	}
	if len(act.Sessions) > 6 {
		act.Sessions = act.Sessions[:6]
	}
	return map[string]any{"sessions": act.Sessions, "idle_in_transaction": idle, "oldest_xact_ms": round3(oldest), "query": query, "config": cfg}, nil
}

// @learn:end stage-culprit

// @learn:stage-fix
func (s *Server) stageFix(ctx context.Context, lc *learnCtx) (any, error) {
	var before, after map[string]any
	_ = platform.GetJSON(ctx, s.Client, s.InventoryURL+"/admin/config", &before)
	lc.before = s.Window.Range(lc.spikeAt, time.Now())
	lc.fixedAt = time.Now()
	if err := platform.PostJSON(ctx, s.Client, s.InventoryURL+"/admin/config", map[string]string{"mode": "fixed"}, &after); err != nil {
		return nil, err
	}
	_ = platform.GetJSON(ctx, s.Client, s.InventoryURL+"/debug/dbstats", &lc.beforeStats)
	return map[string]any{"before": before, "after": after, "fixed_at": lc.fixedAt.Unix()}, nil
}

// @learn:end stage-fix

// @learn:stage-verify
func (s *Server) stageVerify(ctx context.Context, lc *learnCtx) (any, error) {
	// The requests already queued under the legacy code drain first; then
	// measure a clean stretch under the fix with the fault still on.
	settle := 8 * time.Second
	if err := sleep(ctx, time.Until(lc.fixedAt.Add(settle))); err != nil {
		return nil, err
	}
	var st0, st1 map[string]any
	_ = platform.GetJSON(ctx, s.Client, s.InventoryURL+"/debug/dbstats", &st0)
	from := time.Now()
	if err := sleep(ctx, 10*time.Second); err != nil {
		return nil, err
	}
	_ = platform.GetJSON(ctx, s.Client, s.InventoryURL+"/debug/dbstats", &st1)
	after := s.Window.Range(from, time.Now())
	wc := func(m map[string]any) float64 { v, _ := m["wait_count"].(float64); return v }
	return map[string]any{"before": lc.before, "after": after, "after_window_s": 10, "fault_still_on": true,
		"pool_waits_during_after": wc(st1) - wc(st0)}, nil
}

// @learn:end stage-verify

// @learn:stage-history
func (s *Server) stageHistory(ctx context.Context, lc *learnCtx) (any, error) {
	var sum struct {
		Synthetic bool             `json:"synthetic"`
		Totals    map[string]any   `json:"totals"`
		Causes    []map[string]any `json:"causes"`
		Rules     []map[string]any `json:"rules"`
		Naive     map[string]any   `json:"naive"`
	}
	b, err := os.ReadFile(filepath.Join(s.Learn.AnalysisDir, "summary.json"))
	if err != nil {
		return nil, fmt.Errorf("analysis summary: %w", err)
	}
	if err := json.Unmarshal(b, &sum); err != nil {
		return nil, err
	}
	var pool, rule map[string]any
	for _, c := range sum.Causes {
		if c["cause"] == "db_pool_exhaustion" {
			pool = c
		}
	}
	for _, r := range sum.Rules {
		if r["rule"] == "InventoryDBPoolSaturated" {
			rule = r
		}
	}
	top := sum.Causes
	if len(top) > 4 {
		top = top[:4]
	}
	return map[string]any{"synthetic": sum.Synthetic, "cause": pool, "top": top, "rule": rule, "naive": sum.Naive, "totals": sum.Totals}, nil
}

// @learn:end stage-history

func (s *Server) stageRunbook(ctx context.Context, lc *learnCtx) (any, error) {
	steps := []map[string]string{
		{"step": "1", "title": "确认告警和影响面", "stage": "alert"},
		{"step": "2", "title": "看 p99 曲线，定尖峰时刻", "stage": "p99"},
		{"step": "3", "title": "打开一条慢 trace，看时间花在哪一段", "stage": "trace"},
		{"step": "4", "title": "看连接池 InUse 和 WaitCount", "stage": "dbstats"},
		{"step": "5", "title": "用 pg_stat_activity 找到占着连接的事务", "stage": "culprit"},
		{"step": "6", "title": "止血：超时 + 舱壁", "stage": "fix"},
		{"step": "7", "title": "根治：下游调用挪出事务", "stage": "fix"},
		{"step": "8", "title": "验证：p99 回到基线、WaitCount 不再涨", "stage": "verify"},
		{"step": "9", "title": "复盘：在告警历史里归类，调告警规则", "stage": "history"},
	}
	return map[string]any{"doc": "docs/runbook.md", "steps": steps}, nil
}

func (s *Server) analysisSummary(w http.ResponseWriter, r *http.Request) {
	b, err := os.ReadFile(filepath.Join(s.Learn.AnalysisDir, "summary.json"))
	if err != nil {
		platform.Error(w, http.StatusNotFound, "analysis summary not found; run scripts/analyze.sh")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(b)
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
