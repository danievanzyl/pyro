package api

import (
	"archive/tar"
	"bytes"
	cryptorand "crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/danievanzyl/pyro/internal/sandbox"
	"github.com/danievanzyl/pyro/internal/sandbox/imagestate"
	"github.com/go-chi/chi/v5"
	ggcrname "github.com/google/go-containerregistry/pkg/name"
	ggcrregistry "github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/google/go-containerregistry/pkg/v1/types"
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
// drive the ledger directly. Size cap is disabled (-1) — tests against
// unreachable registries would otherwise fail the sync LayerSizes
// resolve before reaching Begin. The size-cap path has its own
// fixture-served test.
func newImageRouterWithManager(t *testing.T) (http.Handler, *sandbox.ImageManager) {
	t.Helper()
	dir := t.TempDir()
	im, err := sandbox.NewImageManager(sandbox.ImageConfig{
		ImagesDir:      dir,
		MaxImageSizeMB: -1,
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
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

// pushTestImage pushes a small amd64 image with a single layer of the
// given size in MiB onto an httptest registry. Layer payload is
// incompressible random bytes so the manifest layer size matches MiB
// (a bytes.Repeat payload would compress to kilobytes and bypass the
// size cap). Returns the ref string.
func pushTestImage(t *testing.T, host, repo, tag string, layerMiB int) string {
	t.Helper()
	payload := make([]byte, layerMiB<<20)
	if _, err := cryptorand.Read(payload); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{
		Name:     "marker",
		Mode:     0o644,
		Size:     int64(len(payload)),
		Typeflag: tar.TypeReg,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	}, tarball.WithMediaType(types.DockerLayer))
	if err != nil {
		t.Fatal(err)
	}
	img, err := mutate.AppendLayers(empty.Image, layer)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	cfg.OS = "linux"
	cfg.Architecture = "amd64"
	img, err = mutate.ConfigFile(img, cfg)
	if err != nil {
		t.Fatal(err)
	}

	ref := host + "/" + repo + ":" + tag
	parsed, err := ggcrname.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	if err := remote.Write(parsed, img); err != nil {
		t.Fatal(err)
	}
	return ref
}

// TestCreateImage_SizeCapReturns413 verifies the disk-cap path: a
// fixture-served image with a layer larger than the configured cap is
// rejected synchronously with 413 + a body carrying limit_mb and
// estimated_mb. No background goroutine is started.
func TestCreateImage_SizeCapReturns413(t *testing.T) {
	srv := httptest.NewServer(ggcrregistry.New())
	defer srv.Close()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ref := pushTestImage(t, u.Host, "test/big", "v1", 3) // ~3 MiB layer

	dir := t.TempDir()
	im, err := sandbox.NewImageManager(sandbox.ImageConfig{
		ImagesDir:      dir,
		MaxImageSizeMB: 1, // tiny cap forces 413
	}, slog.New(slog.NewTextHandler(os.Stderr, nil)))
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	SetupImageRoutes(r, im)

	w := postImage(t, r, map[string]string{
		"name":   "big",
		"source": ref,
	})
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d want 413; body=%s", w.Code, w.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v; body=%s", err, w.Body.String())
	}
	if body["error"] != "image too large" {
		t.Errorf("error = %v want \"image too large\"", body["error"])
	}
	// JSON numbers decode as float64.
	limit, ok := body["limit_mb"].(float64)
	if !ok || int(limit) != 1 {
		t.Errorf("limit_mb = %v (%T) want 1", body["limit_mb"], body["limit_mb"])
	}
	est, ok := body["estimated_mb"].(float64)
	if !ok || est <= 0 {
		t.Errorf("estimated_mb = %v want > 0", body["estimated_mb"])
	}

	// No on-disk dir was created.
	if _, err := os.Stat(dir + "/big"); !os.IsNotExist(err) {
		t.Errorf("image dir should not exist after 413 (err=%v)", err)
	}
	// No ledger entry.
	if op := im.Ledger().Get("big"); op != nil {
		t.Errorf("ledger should be empty after 413, got %+v", op)
	}
}

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
