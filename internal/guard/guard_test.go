package guard

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestBulkheadAdmitsNThenTimesOut(t *testing.T) {
	b := NewBulkhead(2)
	r1, _ := b.Acquire(context.Background(), time.Millisecond)
	r2, _ := b.Acquire(context.Background(), time.Millisecond)
	start := time.Now()
	if _, err := b.Acquire(context.Background(), 30*time.Millisecond); !errors.Is(err, ErrFull) {
		t.Fatalf("third acquire: %v", err)
	}
	if time.Since(start) < 30*time.Millisecond {
		t.Fatal("did not wait")
	}
	r1()
	r3, err := b.Acquire(context.Background(), time.Millisecond)
	if err != nil || b.InUse() != 2 {
		t.Fatalf("after release: %v in use %d", err, b.InUse())
	}
	r2()
	r3()
	if b.InUse() != 0 {
		t.Fatal("slots leaked")
	}
}

func TestBreakerOpensHalfOpensAndCloses(t *testing.T) {
	now := time.Unix(0, 0)
	b := &Breaker{Threshold: 3, Cooldown: time.Second, now: func() time.Time { return now }}
	boom := errors.New("boom")
	for i := 0; i < 3; i++ {
		_ = b.Do(func() error { return boom })
	}
	if b.State() != "open" || b.Do(func() error { t.Fatal("ran while open"); return nil }) != ErrOpen {
		t.Fatal("not open")
	}
	now = now.Add(1100 * time.Millisecond)
	if b.State() != "half-open" {
		t.Fatal(b.State())
	}
	if err := b.Do(func() error { return boom }); err != boom || b.State() != "open" {
		t.Fatalf("failed probe should reopen: %v %s", err, b.State())
	}
	now = now.Add(1100 * time.Millisecond)
	if err := b.Do(func() error { return nil }); err != nil || b.State() != "closed" {
		t.Fatalf("good probe should close: %v %s", err, b.State())
	}
}
