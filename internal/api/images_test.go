package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
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
