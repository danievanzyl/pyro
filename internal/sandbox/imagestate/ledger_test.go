package imagestate

import (
	"errors"
	"testing"
	"time"
)

// fakeClock is a manually-advanced clock for TTL tests.
type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) advance(d time.Duration) { c.now = c.now.Add(d) }

func TestLedger_HappyPath(t *testing.T) {
	l := New(nil, 0)
	op, attached := l.Begin("py", "python:3.12")
	if attached {
		t.Fatalf("first begin should not be attached")
	}
	if op.Status != StatusPulling || op.Source != "python:3.12" {
		t.Fatalf("unexpected initial op: %+v", op)
	}

	if err := l.SetDigest("py", "sha256:abc"); err != nil {
		t.Fatalf("set digest: %v", err)
	}
	if err := l.Update("py", StatusExtracting); err != nil {
		t.Fatalf("→extracting: %v", err)
	}
	got := l.Get("py")
	if got == nil || got.Status != StatusExtracting || got.Digest != "sha256:abc" {
		t.Fatalf("after update: %+v", got)
	}
	if err := l.Complete("py"); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if l.Get("py") != nil {
		t.Fatalf("complete should drop entry; ledger still has it")
	}
}

func TestLedger_FailRecorded(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	l := New(clk, time.Hour)
	l.Begin("py", "python:3.12")
	if err := l.Fail("py", errors.New("registry unreachable")); err != nil {
		t.Fatalf("fail: %v", err)
	}
	got := l.Get("py")
	if got == nil || got.Status != StatusFailed {
		t.Fatalf("expected failed entry; got %+v", got)
	}
	if got.Error != "registry unreachable" {
		t.Fatalf("error not recorded: %q", got.Error)
	}
}

func TestLedger_FailedTTLExpiry(t *testing.T) {
	clk := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	l := New(clk, time.Hour)
	l.Begin("py", "python:3.12")
	_ = l.Fail("py", errors.New("boom"))

	// Just under the TTL — still queryable.
	clk.advance(59 * time.Minute)
	if l.Get("py") == nil {
		t.Fatalf("entry should still be visible inside TTL")
	}

	// Past the TTL — gone.
	clk.advance(2 * time.Minute)
	if l.Get("py") != nil {
		t.Fatalf("entry should be GC'd after TTL")
	}
}

func TestLedger_FreshBeginReplacesFailed(t *testing.T) {
	l := New(nil, time.Hour)
	l.Begin("py", "python:3.11")
	_ = l.Fail("py", errors.New("nope"))

	op, attached := l.Begin("py", "python:3.12")
	if attached {
		t.Fatalf("re-begin after failure should not be attached")
	}
	if op.Status != StatusPulling || op.Source != "python:3.12" {
		t.Fatalf("expected fresh pulling entry; got %+v", op)
	}
	if op.Error != "" {
		t.Fatalf("fresh entry should have no carried-over error")
	}
}

func TestLedger_SecondBeginAttaches(t *testing.T) {
	l := New(nil, time.Hour)
	l.Begin("py", "python:3.12")
	op, attached := l.Begin("py", "python:3.12")
	if !attached {
		t.Fatalf("second begin should attach")
	}
	if op.Status != StatusPulling {
		t.Fatalf("attached op should reflect current status: %+v", op)
	}
}

func TestLedger_RejectsInvalidTransition(t *testing.T) {
	l := New(nil, time.Hour)
	l.Begin("py", "python:3.12")
	if err := l.Update("py", StatusReady); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("pulling→ready should be rejected; got %v", err)
	}

	_ = l.Update("py", StatusExtracting)
	_ = l.Complete("py") // ledger entry now gone

	// ready→pulling: re-Begin produces a *new* pulling entry, which is the
	// intended escape hatch — not a transition on the old terminal state.
	// What we actually guard against is Update / Fail on terminal states,
	// but those entries are already evicted, so callers receive ErrUnknown.
	if err := l.Update("py", StatusPulling); !errors.Is(err, ErrUnknown) {
		t.Fatalf("update on absent entry should be ErrUnknown; got %v", err)
	}
}

func TestLedger_CompleteRequiresExtracting(t *testing.T) {
	l := New(nil, time.Hour)
	l.Begin("py", "python:3.12")
	// Trying to jump pulling→ready directly is invalid.
	if err := l.Complete("py"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("pulling→ready via Complete should be rejected; got %v", err)
	}
}
