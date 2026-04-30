package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/danievanzyl/pyro/internal/sandbox"
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
