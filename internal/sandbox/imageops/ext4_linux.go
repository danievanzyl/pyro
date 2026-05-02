//go:build linux

package imageops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// Ext4Builder owns the lifecycle of a sparse ext4 image: create → mkfs → mount → unmount.
type Ext4Builder struct{}

// NewExt4Builder constructs a builder.
func NewExt4Builder() *Ext4Builder { return &Ext4Builder{} }

// Mounted is a mounted ext4 image. Caller must Close to unmount and clean up.
type Mounted struct {
	ImagePath string
	MountDir  string
}

// Create creates a sparse ext4 image at imagePath sized to sizeMB megabytes,
// mkfs.ext4-formats it, mounts it under {imagePath}.mount, and returns the handle.
func (b *Ext4Builder) Create(ctx context.Context, imagePath string, sizeMB int) (*Mounted, error) {
	if sizeMB < 64 {
		sizeMB = 64
	}
	if err := os.MkdirAll(filepath.Dir(imagePath), 0o755); err != nil {
		return nil, fmt.Errorf("mkdir parent: %w", err)
	}
	if err := exec.CommandContext(ctx, "dd",
		"if=/dev/zero", "of="+imagePath,
		"bs=1M", "count=0", "seek="+itoa(sizeMB)).Run(); err != nil {
		return nil, fmt.Errorf("dd: %w", err)
	}
	if err := exec.CommandContext(ctx, "mkfs.ext4", "-F", imagePath).Run(); err != nil {
		return nil, fmt.Errorf("mkfs.ext4: %w", err)
	}
	mountDir := imagePath + ".mount"
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir mount: %w", err)
	}
	if err := exec.CommandContext(ctx, "mount", "-o", "loop", imagePath, mountDir).Run(); err != nil {
		_ = os.RemoveAll(mountDir)
		return nil, fmt.Errorf("mount: %w", err)
	}
	return &Mounted{ImagePath: imagePath, MountDir: mountDir}, nil
}

// Open mounts an existing ext4 image. Caller must Close to unmount.
func (b *Ext4Builder) Open(ctx context.Context, imagePath string) (*Mounted, error) {
	mountDir := imagePath + ".mount"
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir mount: %w", err)
	}
	if err := exec.CommandContext(ctx, "mount", "-o", "loop", imagePath, mountDir).Run(); err != nil {
		_ = os.RemoveAll(mountDir)
		return nil, fmt.Errorf("mount: %w", err)
	}
	return &Mounted{ImagePath: imagePath, MountDir: mountDir}, nil
}

// Close unmounts the image and removes the mount directory.
func (m *Mounted) Close() error {
	umount := exec.Command("umount", m.MountDir).Run()
	rm := os.RemoveAll(m.MountDir)
	if umount != nil {
		return umount
	}
	return rm
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}
