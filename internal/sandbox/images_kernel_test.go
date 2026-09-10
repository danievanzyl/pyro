package sandbox

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

// TestGet_RootfsOnly_NoPerImageKernel_Succeeds asserts base images are
// rootfs-only: Get() must succeed on an image dir with a rootfs and no
// kernel anywhere (no per-image vmlinux, no shared vmlinux), because the
// guest kernel is a host resource named by --kernel, not an image concern.
func TestGet_RootfsOnly_NoPerImageKernel_Succeeds(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "myimage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "rootfs.ext4"), []byte("rootfs"), 0o644); err != nil {
		t.Fatal(err)
	}

	im, err := NewImageManager(ImageConfig{ImagesDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	info, err := im.Get("myimage")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if info.RootfsPath == "" {
		t.Error("expected rootfs path to be set")
	}
}

// TestGet_NoRootfs_StillErrors asserts the one thing Get() still requires
// after the kernel fallback is deleted: a rootfs.
func TestGet_NoRootfs_StillErrors(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	im, err := NewImageManager(ImageConfig{ImagesDir: root}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	if _, err := im.Get("empty"); err == nil {
		t.Fatal("expected error for missing rootfs")
	}
}
