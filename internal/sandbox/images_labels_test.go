package sandbox

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeImage writes a minimal on-disk image (rootfs.ext4 + vmlinux) and
// optionally an image-meta.json sidecar. Returns the image dir.
func fakeImage(t *testing.T, root, name string, meta *imageMeta) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rootfs.ext4"), []byte("rootfs"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vmlinux"), []byte("kernel"), 0o644); err != nil {
		t.Fatal(err)
	}
	if meta != nil {
		b, err := json.Marshal(meta)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, imageMetaName), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// TestGet_LabelsFromSidecar — Get() surfaces labels persisted in the
// image-meta.json sidecar.
func TestGet_LabelsFromSidecar(t *testing.T) {
	root := t.TempDir()
	fakeImage(t, root, "py312", &imageMeta{
		Digest: "sha256:abc",
		Source: "python:3.12",
		Labels: map[string]string{
			"org.opencontainers.image.source": "https://github.com/python/cpython",
			"org.opencontainers.image.version": "3.12.0",
		},
	})

	im, err := NewImageManager(ImageConfig{ImagesDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	info, err := im.Get("py312")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	want := map[string]string{
		"org.opencontainers.image.source":  "https://github.com/python/cpython",
		"org.opencontainers.image.version": "3.12.0",
	}
	if !reflect.DeepEqual(info.Labels, want) {
		t.Errorf("labels: got=%v want=%v", info.Labels, want)
	}
}

// TestGet_NoLabels_OmittedJSON — image with no labels marshals without
// the labels field. Honors omitzero.
func TestGet_NoLabels_OmittedJSON(t *testing.T) {
	root := t.TempDir()
	// No sidecar — pre-existing image scenario.
	fakeImage(t, root, "old", nil)

	im, err := NewImageManager(ImageConfig{ImagesDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	info, err := im.Get("old")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Labels != nil {
		t.Errorf("labels should be nil for legacy image, got %v", info.Labels)
	}

	b, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	if _, present := raw["labels"]; present {
		t.Errorf("labels field must be omitted from JSON when empty; got %s", b)
	}
}

// TestGet_SidecarMissingLabelsField — sidecar written before this slice
// (no Labels field) must round-trip cleanly without error.
func TestGet_SidecarMissingLabelsField(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "legacy")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rootfs.ext4"), []byte("r"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vmlinux"), []byte("k"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-slice sidecar shape: just digest + source, no labels key.
	pre := []byte(`{"digest":"sha256:old","source":"alpine:3.18"}`)
	if err := os.WriteFile(filepath.Join(dir, imageMetaName), pre, 0o644); err != nil {
		t.Fatal(err)
	}

	im, err := NewImageManager(ImageConfig{ImagesDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	info, err := im.Get("legacy")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.Digest != "sha256:old" || info.Source != "alpine:3.18" {
		t.Errorf("digest/source roundtrip broken: %+v", info)
	}
	if info.Labels != nil {
		t.Errorf("legacy sidecar should give nil labels, got %v", info.Labels)
	}
}
