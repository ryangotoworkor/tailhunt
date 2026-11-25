package inventory

import (
	"encoding/json"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ryangotoworkor/tailhunt/internal/platform"
)

// Config switches each layer of the fix on or off at runtime, so the same
// binary can show the incident and the fix side by side.
// @learn:fix-config
type Config struct {
	AcquireTimeout bool `json:"acquire_timeout"`  // availability waits at most 80 ms for a connection, then 503
	Bulkhead       bool `json:"bulkhead"`         // holds use at most 2 of the 6 connections
	QuoteOutsideTx bool `json:"quote_outside_tx"` // quote first, then a short transaction
	QuoteGuard     bool `json:"quote_guard"`      // the quote call has a timeout and a circuit breaker
}

// Legacy is how the service first shipped; Fixed is every layer on.
var (
	Legacy = Config{}
	Fixed  = Config{AcquireTimeout: true, Bulkhead: true, QuoteOutsideTx: true, QuoteGuard: true}
)

// @learn:end fix-config

// Tunables of the fixed path.
const (
	availAcquireTimeout = 80 * time.Millisecond
	holdAcquireTimeout  = 300 * time.Millisecond
	holdSlots           = 2
	queryTimeout        = 2 * time.Second
	quoteTimeout        = 800 * time.Millisecond
)

// Sabotage forces layers off whatever the config says. It exists so the
// tests can prove they would notice a layer going missing:
// TAILHUNT_SABOTAGE=timeout,bulkhead,txscope,quoteguard (or "all").
// @learn:sabotage
func sabotaged() []string {
	v := os.Getenv("TAILHUNT_SABOTAGE")
	if v == "" {
		return nil
	}
	if v == "all" {
		return []string{"timeout", "bulkhead", "txscope", "quoteguard"}
	}
	return strings.Split(v, ",")
}

func effective(c Config) Config {
	off := sabotaged()
	if slices.Contains(off, "timeout") {
		c.AcquireTimeout = false
	}
	if slices.Contains(off, "bulkhead") {
		c.Bulkhead = false
	}
	if slices.Contains(off, "txscope") {
		c.QuoteOutsideTx = false
	}
	if slices.Contains(off, "quoteguard") {
		c.QuoteGuard = false
	}
	return c
}

// @learn:end sabotage

type configBox struct{ p atomic.Pointer[Config] }

func (b *configBox) get() Config {
	if c := b.p.Load(); c != nil {
		return effective(*c)
	}
	return effective(Fixed)
}

// SetConfig replaces the running config.
func (s *Server) SetConfig(c Config) { s.cfg.p.Store(&c) }

// Config returns the effective config (after sabotage).
func (s *Server) Config() Config { return s.cfg.get() }

func modeOf(c Config) string {
	switch c {
	case Legacy:
		return "legacy"
	case Fixed:
		return "fixed"
	}
	return "custom"
}

func (s *Server) configHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var in struct {
			Mode string `json:"mode"`
			*Config
		}
		if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
			platform.Error(w, http.StatusBadRequest, "bad json")
			return
		}
		switch {
		case in.Mode == "legacy":
			s.SetConfig(Legacy)
		case in.Mode == "fixed":
			s.SetConfig(Fixed)
		case in.Config != nil:
			s.SetConfig(*in.Config)
		default:
			platform.Error(w, http.StatusBadRequest, `send {"mode":"legacy|fixed"} or the four flags`)
			return
		}
	}
	c := s.Config()
	platform.JSON(w, http.StatusOK, map[string]any{"mode": modeOf(c), "config": c, "sabotaged": sabotaged(),
		"hold_slots": holdSlots, "breaker": s.breaker.State()})
}
