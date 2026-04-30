package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/danievanzyl/pyro/internal/sandbox/imagestate"
	"github.com/go-chi/chi/v5"
)

func newImageRouter(t *testing.T) http.Handler {
	t.Helper()
	dir := t.TempDir()
	im, err := sandbox.NewImageManager(sandbox.ImageConfig{ImagesDir: dir}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	SetupImageRoutes(r, im)
	return r
}

func postImage(t *testing.T, r http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/images", bytes.NewReader(buf))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCreateImage_RejectsBothSourceAndDockerfile(t *testing.T) {
	r := newImageRouter(t)
	w := postImage(t, r, map[string]string{
		"name":       "x",
		"source":     "python:3.12",
		"dockerfile": "/tmp/Dockerfile",
	})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCreateImage_RejectsNeitherSourceNorDockerfile(t *testing.T) {
	r := newImageRouter(t)
	w := postImage(t, r, map[string]string{"name": "x"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCreateImage_RejectsMissingName(t *testing.T) {
	r := newImageRouter(t)
	w := postImage(t, r, map[string]string{"source": "python:3.12"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d want 400; body=%s", w.Code, w.Body.String())
	}
}

// newImageRouterWithManager exposes the manager so tests can inspect or
// drive the ledger directly.
func newImageRouterWithManager(t *testing.T) (http.Handler, *sandbox.ImageManager) {
	t.Helper()
	dir := t.TempDir()
	im, err := sandbox.NewImageManager(sandbox.ImageConfig{ImagesDir: dir}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	SetupImageRoutes(r, im)
	return r, im
}

// TestCreateImage_AsyncReturns202 verifies POST /images with a source
// returns immediately (well under the request timeout) with 202 + a
// pulling-state body. The background goroutine's failure against an
// unreachable registry is incidental — we only assert on the sync path.
func TestCreateImage_AsyncReturns202(t *testing.T) {
	r, _ := newImageRouterWithManager(t)
	start := time.Now()
	// 127.0.0.1:1 is an unused port — the goroutine will fail but the
	// synchronous handler still returns 202 immediately.
	w := postImage(t, r, map[string]string{
		"name":   "py312",
		"source": "127.0.0.1:1/no:such",
	})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("handler took %v — async path not actually returning fast", elapsed)
	}
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d want 202; body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if body["name"] != "py312" {
		t.Errorf("name = %v want py312", body["name"])
	}
	if body["status"] != "pulling" {
		t.Errorf("status = %v want pulling", body["status"])
	}
	if body["source"] != "127.0.0.1:1/no:such" {
		t.Errorf("source = %v", body["source"])
	}
}

// TestGetImage_SurfacesLedgerState directly seeds the ledger with a
// failed entry and verifies GET /images/{name} surfaces it (the ledger
// is consulted before disk).
func TestGetImage_SurfacesLedgerState(t *testing.T) {
	r, im := newImageRouterWithManager(t)
	op, _ := im.Ledger().Begin("brokenpy", "python:3.12")
	if op.Status != imagestate.StatusPulling {
		t.Fatalf("seed pulling state; got %v", op.Status)
	}
	if err := im.Ledger().Fail("brokenpy", errSentinel("registry unreachable")); err != nil {
		t.Fatalf("fail: %v", err)
	}

	req := httptest.NewRequest("GET", "/images/brokenpy", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d want 200; body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "failed" {
		t.Errorf("status = %v want failed", body["status"])
	}
	if body["error"] != "registry unreachable" {
		t.Errorf("error = %v", body["error"])
	}
}

// errSentinel is a tiny error helper for tests so we don't depend on
// `errors.New` import noise.
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// TestCreateImage_ConcurrentSameSource_SingleFlight fires N concurrent
// POSTs against the same name+source. All must return 202; only one
// underlying registry pull may start (verified by counting
// image.pulling SSE events on the EventBus — Begin only emits on a
// fresh entry, attached callers do not re-emit).
func TestCreateImage_ConcurrentSameSource_SingleFlight(t *testing.T) {
	r, im := newImageRouterWithManager(t)
	bus := NewEventBus()
	im.SetEmitter(bus)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	const N = 8
	var wg sync.WaitGroup
	codes := make([]int, N)
	bodies := make([]map[string]any, N)
	startBarrier := make(chan struct{})

	for i := range N {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-startBarrier
			w := postImage(t, r, map[string]string{
				"name":   "concur",
				"source": "127.0.0.1:1/no:such",
			})
			codes[i] = w.Code
			_ = json.Unmarshal(w.Body.Bytes(), &bodies[i])
		}(i)
	}
	close(startBarrier)
	wg.Wait()

	for i, c := range codes {
		if c != http.StatusAccepted {
			t.Errorf("call[%d] code=%d body=%v want 202", i, c, bodies[i])
		}
	}

	// Drain events for a short window. Single-flight ⇒ exactly one
	// image.pulling. (The background goroutine will eventually emit
	// image.failed too, which is fine.)
	pulling := 0
	deadline := time.After(300 * time.Millisecond)
drain:
	for {
		select {
		case ev := <-ch:
			if ev.Type == imagestate.EventPulling {
				pulling++
			}
		case <-deadline:
			break drain
		}
	}
	if pulling != 1 {
		t.Errorf("image.pulling events = %d want 1 (single-flight violated)", pulling)
	}
}

// TestCreateImage_SourceMismatchReturns409 verifies that a second POST
// for the same name with a *different* source while the first is still
// in flight returns 409. We seed the ledger directly (no goroutine) so
// the in-flight state is deterministic.
func TestCreateImage_SourceMismatchReturns409(t *testing.T) {
	r, im := newImageRouterWithManager(t)
	// Seed an in-flight pull for "py312" with source "python:3.12".
	im.Ledger().Begin("py312", "python:3.12")

	w := postImage(t, r, map[string]string{
		"name":   "py312",
		"source": "python:3.13",
	})
	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d want 409; body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	errStr, _ := body["error"].(string)
	if errStr == "" {
		t.Fatalf("expected non-empty error message; body=%s", w.Body.String())
	}
}

// TestCreateImage_SameSourceReturns202 confirms that re-posting the
// same source against an in-flight pull does not trip the 409 path —
// it attaches and returns 202.
func TestCreateImage_SameSourceReturns202(t *testing.T) {
	r, im := newImageRouterWithManager(t)
	im.Ledger().Begin("py312", "python:3.12")

	w := postImage(t, r, map[string]string{
		"name":   "py312",
		"source": "python:3.12",
	})
	if w.Code != http.StatusAccepted {
		t.Fatalf("status = %d want 202; body=%s", w.Code, w.Body.String())
	}
}

// TestImageLifecycle_EmitsSSEEventsViaBus drives the ledger through the
// full happy-path transition sequence after wiring the api.EventBus as
// the emitter. Verifies subscribers see image.pulling →
// image.extracting → image.ready in order with the right payloads.
// Ledger emission is exercised directly so the test runs anywhere
// (the registry-pull goroutine isn't needed to validate the SSE wire).
func TestImageLifecycle_EmitsSSEEventsViaBus(t *testing.T) {
	dir := t.TempDir()
	im, err := sandbox.NewImageManager(sandbox.ImageConfig{ImagesDir: dir}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}

	bus := NewEventBus()
	im.SetEmitter(bus)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	// Drive the ledger transitions like the orchestrator does.
	im.Ledger().Begin("py312", "python:3.12")
	if err := im.Ledger().SetDigest("py312", "sha256:dead"); err != nil {
		t.Fatalf("set digest: %v", err)
	}
	if err := im.Ledger().Update("py312", imagestate.StatusExtracting); err != nil {
		t.Fatalf("→extracting: %v", err)
	}
	if err := im.Ledger().Complete("py312", 4096); err != nil {
		t.Fatalf("complete: %v", err)
	}

	want := []string{imagestate.EventPulling, imagestate.EventExtracting, imagestate.EventReady}
	got := make([]string, 0, len(want))
	deadline := time.After(2 * time.Second)
	for len(got) < len(want) {
		select {
		case ev := <-ch:
			if ev.Type == "connected" {
				continue
			}
			got = append(got, ev.Type)
		case <-deadline:
			t.Fatalf("only received %d events: %v want %v", len(got), got, want)
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %s want %s", i, got[i], want[i])
		}
	}
}

// TestImageLifecycle_EmitsFailedEvent covers the error path: when
// runRegistryPull (or the ledger) calls Fail, subscribers receive
// image.pulling followed by image.failed with the error.
func TestImageLifecycle_EmitsFailedEvent(t *testing.T) {
	dir := t.TempDir()
	im, err := sandbox.NewImageManager(sandbox.ImageConfig{ImagesDir: dir}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}

	bus := NewEventBus()
	im.SetEmitter(bus)
	ch := bus.Subscribe()
	defer bus.Unsubscribe(ch)

	im.Ledger().Begin("brokenpy", "python:3.12")
	if err := im.Ledger().Fail("brokenpy", errSentinel("registry unreachable")); err != nil {
		t.Fatalf("fail: %v", err)
	}

	want := []string{imagestate.EventPulling, imagestate.EventFailed}
	var failPayload map[string]any
	got := make([]string, 0, len(want))
	deadline := time.After(2 * time.Second)
	for len(got) < len(want) {
		select {
		case ev := <-ch:
			if ev.Type == "connected" {
				continue
			}
			got = append(got, ev.Type)
			if ev.Type == imagestate.EventFailed {
				failPayload, _ = ev.Data.(map[string]any)
			}
		case <-deadline:
			t.Fatalf("only received %d events: %v want %v", len(got), got, want)
		}
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event[%d] = %s want %s", i, got[i], want[i])
		}
	}
	if failPayload["name"] != "brokenpy" {
		t.Errorf("failed payload name = %v want brokenpy", failPayload["name"])
	}
	if failPayload["error"] != "registry unreachable" {
		t.Errorf("failed payload error = %v", failPayload["error"])
	}
}
