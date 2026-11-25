package obs

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Line is one request in the request log (JSON Lines). analysis/ reads
// these files with SQL; the gateway's line and inventory's line for the
// same request share trace_id, which is how the analysis joins end-to-end
// latency to time spent waiting for a DB connection.
// @learn:reqlog-line
type Line struct {
	TS       time.Time `json:"ts"`
	Svc      string    `json:"svc"`
	Route    string    `json:"route"`
	Status   int       `json:"status"`
	DurMs    float64   `json:"dur_ms"`
	DBWaitMs float64   `json:"db_wait_ms"`
	TraceID  string    `json:"trace_id"`
}

// @learn:end reqlog-line

// RequestLog appends Lines to LOG_DIR/<svc>.jsonl (or discards them when
// LOG_DIR is unset). It rotates once the file passes 64 MB.
type RequestLog struct {
	mu   sync.Mutex
	path string
	f    *os.File
	w    *bufio.Writer
	n    int64
	last time.Time
}

const maxLog = 64 << 20

// NewRequestLog opens the log for svc.
func NewRequestLog(svc string) *RequestLog {
	dir := os.Getenv("LOG_DIR")
	l := &RequestLog{}
	if dir == "" {
		return l
	}
	l.path = filepath.Join(dir, svc+".jsonl")
	if err := l.open(); err != nil {
		slog.Warn("request log disabled", "err", err)
		l.path = ""
		return l
	}
	go func() { // an idle service still gets its last lines on disk
		for range time.Tick(time.Second) {
			l.Flush()
		}
	}()
	return l
}

func (l *RequestLog) open() error {
	if err := os.MkdirAll(filepath.Dir(l.path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	l.f, l.w, l.n = f, bufio.NewWriterSize(f, 64<<10), st.Size()
	return nil
}

// Write appends one line. Buffered; flushed at least every 500 ms.
func (l *RequestLog) Write(line Line) {
	if l.path == "" {
		return
	}
	b, _ := json.Marshal(line)
	l.mu.Lock()
	defer l.mu.Unlock()
	n, _ := l.w.Write(append(b, '\n'))
	l.n += int64(n)
	if time.Since(l.last) > 500*time.Millisecond {
		_ = l.w.Flush()
		l.last = time.Now()
	}
	if l.n > maxLog {
		_ = l.w.Flush()
		_ = l.f.Close()
		_ = os.Rename(l.path, l.path+".1")
		if err := l.open(); err != nil {
			l.path = ""
		}
	}
}

// Flush writes buffered lines to the file.
func (l *RequestLog) Flush() {
	if l.path == "" {
		return
	}
	l.mu.Lock()
	_ = l.w.Flush()
	l.mu.Unlock()
}
