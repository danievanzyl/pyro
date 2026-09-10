package main

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/danievanzyl/pyro/internal/sandbox"
)

// TestWriteImageSidecar_ReadableByImageManager pins the CLI/server contract:
// the sidecar build-image writes must be named and shaped so that
// ImageManager.Get can read it back with a non-blank Source and Digest.
func TestWriteImageSidecar_ReadableByImageManager(t *testing.T) {
	imagesDir := t.TempDir()
	imgDir := filepath.Join(imagesDir, "myimg")
	if err := os.MkdirAll(imgDir, 0755); err != nil {
		t.Fatal(err)
	}
	rootfs := filepath.Join(imgDir, "rootfs.ext4")
	if err := os.WriteFile(rootfs, []byte("fake-rootfs-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	writeImageSidecar(imgDir, "myimg", "docker://ubuntu:24.04", 512, rootfs)

	sidecar := filepath.Join(imgDir, "image-meta.json")
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("expected sidecar at %s: %v", sidecar, err)
	}

	im, err := sandbox.NewImageManager(sandbox.ImageConfig{ImagesDir: imagesDir}, slog.Default())
	if err != nil {
		t.Fatal(err)
	}
	info, err := im.Get("myimg")
	if err != nil {
		t.Fatal(err)
	}
	if info.Source == "" {
		t.Error("expected non-blank Source")
	}
	if info.Digest == "" {
		t.Error("expected non-blank Digest")
	}
}
