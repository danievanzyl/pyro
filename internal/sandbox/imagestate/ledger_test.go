package imagestate

import (
	"errors"
	"sync"
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
	if err := l.Complete("py", 0); err != nil {
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
	_ = l.Complete("py", 0) // ledger entry now gone

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
	if err := l.Complete("py", 0); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("pulling→ready via Complete should be rejected; got %v", err)
	}
}

// captureEmitter records every event for assertion. Safe for concurrent
// publishers but tests here run single-threaded so the channel-free
// slice is fine.
type captureEmitter struct {
	mu     sync.Mutex
	events []capturedEvent
}

type capturedEvent struct {
	Type    string
	Payload map[string]any
}

func (c *captureEmitter) Publish(eventType string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m, _ := data.(map[string]any)
	c.events = append(c.events, capturedEvent{Type: eventType, Payload: m})
}

func (c *captureEmitter) types() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.events))
	for i, e := range c.events {
		out[i] = e.Type
	}
	return out
}

func TestLedger_EmitsHappyPathEvents(t *testing.T) {
	emitter := &captureEmitter{}
	l := New(nil, 0)
	l.SetEmitter(emitter)

	l.Begin("py", "python:3.12")
	if err := l.SetDigest("py", "sha256:abc"); err != nil {
		t.Fatalf("set digest: %v", err)
	}
	if err := l.Update("py", StatusExtracting); err != nil {
		t.Fatalf("→extracting: %v", err)
	}
	if err := l.Complete("py", 12345); err != nil {
		t.Fatalf("complete: %v", err)
	}

	want := []string{EventPulling, EventExtracting, EventReady}
	got := emitter.types()
	if len(got) != len(want) {
		t.Fatalf("event count = %d (%v) want %d (%v)", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %s want %s", i, got[i], want[i])
		}
	}

	// Pulling payload carries name + source.
	if p := emitter.events[0].Payload; p["name"] != "py" || p["source"] != "python:3.12" {
		t.Errorf("pulling payload = %+v", p)
	}
	// Extracting payload carries name only.
	if p := emitter.events[1].Payload; p["name"] != "py" {
		t.Errorf("extracting payload = %+v", p)
	}
	// Ready payload carries name + digest + size.
	if p := emitter.events[2].Payload; p["name"] != "py" || p["digest"] != "sha256:abc" || p["size"] != int64(12345) {
		t.Errorf("ready payload = %+v", p)
	}
}

func TestLedger_EmitsFailedEvent(t *testing.T) {
	emitter := &captureEmitter{}
	l := New(nil, time.Hour)
	l.SetEmitter(emitter)

	l.Begin("py", "python:3.12")
	if err := l.Fail("py", errors.New("registry unreachable")); err != nil {
		t.Fatalf("fail: %v", err)
	}

	got := emitter.types()
	want := []string{EventPulling, EventFailed}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("events = %v want %v", got, want)
	}
	p := emitter.events[1].Payload
	if p["name"] != "py" || p["error"] != "registry unreachable" {
		t.Errorf("failed payload = %+v", p)
	}
}

// TestLedger_ConcurrentBegin_SameName fires N concurrent Begins for one
// name and asserts exactly one fresh op (attached=false) plus N-1
// attached. Race-detector clean (mutex serializes Begin).
func TestLedger_ConcurrentBegin_SameName(t *testing.T) {
	const N = 32
	l := New(nil, time.Hour)

	var freshCount, attachedCount int64
	var mu sync.Mutex
	var wg sync.WaitGroup
	start := make(chan struct{})

	for range N {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // align goroutines on a barrier
			_, attached := l.Begin("py", "python:3.12")
			mu.Lock()
			if attached {
				attachedCount++
			} else {
				freshCount++
			}
			mu.Unlock()
		}()
	}
	close(start)
	wg.Wait()

	if freshCount != 1 {
		t.Fatalf("fresh begin count = %d want 1", freshCount)
	}
	if attachedCount != N-1 {
		t.Fatalf("attached count = %d want %d", attachedCount, N-1)
	}
}

// TestLedger_ConcurrentBegin_DifferentNames: distinct names never
// collide; every caller should see attached=false.
func TestLedger_ConcurrentBegin_DifferentNames(t *testing.T) {
	const N = 16
	l := New(nil, time.Hour)

	var attachedCount int64
	var mu sync.Mutex
	var wg sync.WaitGroup

	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := "img-" + itoa(i)
			_, attached := l.Begin(name, "src:"+itoa(i))
			if attached {
				mu.Lock()
				attachedCount++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	if attachedCount != 0 {
		t.Fatalf("distinct names should never attach; got %d attached", attachedCount)
	}
}

// TestLedger_AttachedSeesUpdates verifies an attached caller (who got
// only a snapshot at Begin time) can poll Get() to observe later
// transitions driven by the original puller goroutine.
func TestLedger_AttachedSeesUpdates(t *testing.T) {
	l := New(nil, time.Hour)
	first, _ := l.Begin("py", "python:3.12")
	second, attached := l.Begin("py", "python:3.12")
	if !attached {
		t.Fatalf("second begin should be attached")
	}
	// Both snapshots reflect the initial pulling state.
	if first.Status != StatusPulling || second.Status != StatusPulling {
		t.Fatalf("both snapshots should be pulling; got %s / %s", first.Status, second.Status)
	}
	// Driver advances the entry…
	if err := l.Update("py", StatusExtracting); err != nil {
		t.Fatalf("update: %v", err)
	}
	// …and the attached caller sees it via Get().
	if got := l.Get("py"); got == nil || got.Status != StatusExtracting {
		t.Fatalf("attached poll should see extracting; got %+v", got)
	}
}

// itoa avoids the strconv import for a single-digit integer-to-string.
func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('a'+i-10)) // up to 36; tests use <32
}

func TestLedger_AttachedBeginDoesNotEmit(t *testing.T) {
	emitter := &captureEmitter{}
	l := New(nil, time.Hour)
	l.SetEmitter(emitter)

	l.Begin("py", "python:3.12") // 1 emit
	l.Begin("py", "python:3.12") // attach — no extra emit

	if got := emitter.types(); len(got) != 1 || got[0] != EventPulling {
		t.Fatalf("attached begin should not re-emit pulling; got %v", got)
	}
}
