// Package guard has the two small concurrency guards inventory uses: a
// bulkhead (a counting semaphore with a deadline) and a circuit breaker.
package guard

import (
	"context"
	"errors"
	"sync"
	"time"
)

// ErrFull is returned when a bulkhead slot did not free up in time.
var ErrFull = errors.New("bulkhead full")

// Bulkhead lets at most N callers in at once. It is how the slow code path
// (holds) is kept from taking every database connection: with N below the
// pool size, the fast path (availability) always has connections left.
// @learn:bulkhead
type Bulkhead struct{ slots chan struct{} }

// NewBulkhead makes a bulkhead with n slots.
func NewBulkhead(n int) *Bulkhead { return &Bulkhead{slots: make(chan struct{}, n)} }

// Acquire waits up to wait for a slot. The returned func gives it back.
func (b *Bulkhead) Acquire(ctx context.Context, wait time.Duration) (func(), error) {
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case b.slots <- struct{}{}:
		return func() { <-b.slots }, nil
	case <-t.C:
		return nil, ErrFull
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// @learn:end bulkhead

// InUse is how many slots are taken.
func (b *Bulkhead) InUse() int { return len(b.slots) }

// Cap is the number of slots.
func (b *Bulkhead) Cap() int { return cap(b.slots) }

// ErrOpen is returned while the breaker is open.
var ErrOpen = errors.New("circuit open")

// Breaker opens after Threshold consecutive failures, rejects calls for
// Cooldown, then lets one probe through (half-open): success closes it,
// failure opens it again.
type Breaker struct {
	Threshold int
	Cooldown  time.Duration

	mu       sync.Mutex
	fails    int
	openedAt time.Time
	probing  bool
	now      func() time.Time
}

// State is closed, open or half-open.
func (b *Breaker) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.fails < b.Threshold:
		return "closed"
	case b.clock().Sub(b.openedAt) < b.Cooldown:
		return "open"
	default:
		return "half-open"
	}
}

func (b *Breaker) clock() time.Time {
	if b.now != nil {
		return b.now()
	}
	return time.Now()
}

// Do runs f unless the breaker is open.
func (b *Breaker) Do(f func() error) error {
	b.mu.Lock()
	if b.fails >= b.Threshold {
		if b.clock().Sub(b.openedAt) < b.Cooldown || b.probing {
			b.mu.Unlock()
			return ErrOpen
		}
		b.probing = true
	}
	b.mu.Unlock()
	err := f()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
	if err == nil {
		b.fails = 0
		return nil
	}
	b.fails++
	if b.fails >= b.Threshold {
		b.openedAt = b.clock()
	}
	return err
}
