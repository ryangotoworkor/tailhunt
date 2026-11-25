// Package platform holds the small pieces every tailhunt service shares:
// reading config from the environment, writing JSON responses, calling a
// sibling service, and running an HTTP server that shuts down cleanly.
package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// Env returns the environment variable or def when it is unset or empty.
func Env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// EnvInt is Env for integers; a value that does not parse is a startup error.
func EnvInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Error("bad integer in environment", "key", key, "value", v)
		os.Exit(2)
	}
	return n
}

// JSON writes v with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// Error writes {"error": msg}.
func Error(w http.ResponseWriter, status int, msg string) {
	JSON(w, status, map[string]string{"error": msg})
}

// StatusError is returned by GetJSON / PostJSON when the sibling answered
// with a non-2xx status, so callers can pass the status through.
type StatusError struct {
	Status int
	Body   string
}

func (e *StatusError) Error() string { return fmt.Sprintf("upstream %d: %s", e.Status, e.Body) }

// GetJSON calls url and decodes a JSON body into out.
func GetJSON(ctx context.Context, c *http.Client, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	return do(c, req, out)
}

// PostJSON sends body as JSON and decodes the reply into out (out may be nil).
func PostJSON(ctx context.Context, c *http.Client, url string, body, out any) error {
	pr, pw := io.Pipe()
	go func() { pw.CloseWithError(json.NewEncoder(pw).Encode(body)) }()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return do(c, req, out)
}

func do(c *http.Client, req *http.Request, out any) error {
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return &StatusError{Status: resp.StatusCode, Body: string(b)}
	}
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Run serves h on addr until SIGINT/SIGTERM, then drains for up to 5 s.
// onStop runs after the listener has closed (flush telemetry, close pools).
func Run(name, addr string, h http.Handler, onStop ...func(context.Context)) {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		slog.Info("listening", "svc", name, "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server failed", "svc", name, "err", err)
			os.Exit(1)
		}
	}()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	for _, f := range onStop {
		f(ctx)
	}
	slog.Info("stopped", "svc", name)
}

// Health is the handler every service mounts on /healthz.
func Health(w http.ResponseWriter, _ *http.Request) {
	JSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
