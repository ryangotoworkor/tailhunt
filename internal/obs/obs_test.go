package obs

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWrapLogsOneLinePerRequestWithRouteStatusAndDBWait(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOG_DIR", dir)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /things/{id}", func(w http.ResponseWriter, r *http.Request) {
		Info(r.Context()).AddDBWait(7 * time.Millisecond)
		w.WriteHeader(http.StatusTeapot)
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {})
	var seen []string
	h := Wrap("unit", mux, func(route string, status int, _ time.Duration, _ string) { seen = append(seen, route) })
	for _, p := range []string{"/things/1", "/things/2", "/healthz", "/nope"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
	}
	time.Sleep(1200 * time.Millisecond) // the background flush runs every second
	f, err := os.Open(filepath.Join(dir, "unit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var lines []Line
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var l Line
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatalf("not JSON: %s", sc.Text())
		}
		lines = append(lines, l)
	}
	// two matched requests + one unmatched; /healthz is not logged
	if len(lines) != 3 || len(seen) != 3 {
		t.Fatalf("%d lines, %d observed: %+v", len(lines), len(seen), lines)
	}
	if l := lines[0]; l.Route != "GET /things/{id}" || l.Status != 418 || l.DBWaitMs != 7 || l.Svc != "unit" {
		t.Fatalf("line %+v", l)
	}
	if lines[2].Route != "unmatched" {
		t.Fatalf("unmatched route logged as %q", lines[2].Route)
	}
}
