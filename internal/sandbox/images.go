// Package sandbox — images.go manages base images for sandboxes.
//
// Images are stored as rootfs.ext4 + vmlinux kernel pairs.
// Each image has a name, and sandboxes reference images by name.
//
// Directory layout:
//
//	{ImagesDir}/
//	  ├── default/
//	  │   ├── rootfs.ext4
//	  │   └── vmlinux
//	  ├── python312/
//	  │   ├── rootfs.ext4
//	  │   └── vmlinux
//	  └── node22/
//	      ├── rootfs.ext4
//	      └── vmlinux
package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// ImageConfig configures image management.
type ImageConfig struct {
	// ImagesDir is the base directory for all images.
	ImagesDir string

	// AgentBinaryPath is the path to the compiled fc-agent binary.
	// It gets injected into new rootfs images at /usr/bin/fc-agent.
	AgentBinaryPath string
}

// ImageManager handles base image lifecycle.
type ImageManager struct {
	cfg ImageConfig
	log *slog.Logger
}

// ImageInfo describes a stored base image.
type ImageInfo struct {
	Name      string    `json:"name"`
	RootfsPath string   `json:"rootfs_path"`
	KernelPath string   `json:"kernel_path"`
	Size      int64     `json:"size"` // rootfs size in bytes
	CreatedAt time.Time `json:"created_at"`
}

// NewImageManager creates an image manager.
func NewImageManager(cfg ImageConfig, log *slog.Logger) (*ImageManager, error) {
	if err := os.MkdirAll(cfg.ImagesDir, 0750); err != nil {
		return nil, fmt.Errorf("create images dir: %w", err)
	}
	return &ImageManager{cfg: cfg, log: log}, nil
}

// List returns all available images.
func (im *ImageManager) List() ([]*ImageInfo, error) {
	entries, err := os.ReadDir(im.cfg.ImagesDir)
	if err != nil {
		return nil, fmt.Errorf("read images dir: %w", err)
	}

	var images []*ImageInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := im.Get(entry.Name())
		if err != nil {
			continue // skip malformed image dirs
		}
		images = append(images, info)
	}
	return images, nil
}

// Get returns info for a specific image.
func (im *ImageManager) Get(name string) (*ImageInfo, error) {
	dir := filepath.Join(im.cfg.ImagesDir, name)
	rootfs := filepath.Join(dir, "rootfs.ext4")
	kernel := filepath.Join(dir, "vmlinux")

	rootfsInfo, err := os.Stat(rootfs)
	if err != nil {
		return nil, fmt.Errorf("rootfs not found for image %q: %w", name, err)
	}

	if _, err := os.Stat(kernel); err != nil {
		return nil, fmt.Errorf("kernel not found for image %q: %w", name, err)
	}

	return &ImageInfo{
		Name:       name,
		RootfsPath: rootfs,
		KernelPath: kernel,
		Size:       rootfsInfo.Size(),
		CreatedAt:  rootfsInfo.ModTime(),
	}, nil
}

// CreateFromDockerfile builds a rootfs from a Dockerfile.
// It uses docker/buildah to build the image, then exports the filesystem
// to an ext4 image with the fc-agent binary injected.
func (im *ImageManager) CreateFromDockerfile(ctx context.Context, name, dockerfilePath string) (*ImageInfo, error) {
	dir := filepath.Join(im.cfg.ImagesDir, name)
	if err := os.MkdirAll(dir, 0750); err != nil {
		return nil, fmt.Errorf("create image dir: %w", err)
	}

	rootfs := filepath.Join(dir, "rootfs.ext4")
	dockerCtx := filepath.Dir(dockerfilePath)

	im.log.Info("building image from Dockerfile",
		"name", name,
		"dockerfile", dockerfilePath)

	// Build Docker image.
	dockerTag := fmt.Sprintf("firecrackerlacker/%s:latest", name)
	buildCmd := exec.CommandContext(ctx, "docker", "build",
		"-t", dockerTag,
		"-f", dockerfilePath,
		dockerCtx)
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("docker build: %w", err)
	}

	// Create a container to export filesystem.
	containerName := fmt.Sprintf("fclk-export-%s-%d", name, time.Now().UnixNano())
	createCmd := exec.CommandContext(ctx, "docker", "create", "--name", containerName, dockerTag)
	if err := createCmd.Run(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("docker create: %w", err)
	}
	defer exec.Command("docker", "rm", containerName).Run()

	// Export filesystem to tar.
	tarPath := filepath.Join(dir, "rootfs.tar")
	exportCmd := exec.CommandContext(ctx, "docker", "export", "-o", tarPath, containerName)
	if err := exportCmd.Run(); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("docker export: %w", err)
	}
	defer os.Remove(tarPath)

	// Create ext4 image from tar.
	if err := im.tarToExt4(ctx, tarPath, rootfs); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("tar to ext4: %w", err)
	}

	// Inject fc-agent binary into rootfs.
	if im.cfg.AgentBinaryPath != "" {
		if err := im.injectAgent(ctx, rootfs); err != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("inject agent: %w", err)
		}
	}

	// Copy the default kernel (images share the same kernel for now).
	defaultKernel := filepath.Join(im.cfg.ImagesDir, "default", "vmlinux")
	kernel := filepath.Join(dir, "vmlinux")
	if err := copyFile(defaultKernel, kernel); err != nil {
		// Non-fatal — user can provide kernel separately.
		im.log.Warn("could not copy default kernel", "err", err)
	}

	im.log.Info("image created", "name", name, "rootfs", rootfs)
	return im.Get(name)
}

// tarToExt4 converts a tar archive to an ext4 filesystem image.
func (im *ImageManager) tarToExt4(ctx context.Context, tarPath, ext4Path string) error {
	// Create a 2GB sparse ext4 image.
	ddCmd := exec.CommandContext(ctx, "dd",
		"if=/dev/zero", "of="+ext4Path,
		"bs=1M", "count=0", "seek=2048")
	if err := ddCmd.Run(); err != nil {
		return fmt.Errorf("create sparse image: %w", err)
	}

	mkfsCmd := exec.CommandContext(ctx, "mkfs.ext4", "-F", ext4Path)
	if err := mkfsCmd.Run(); err != nil {
		return fmt.Errorf("mkfs.ext4: %w", err)
	}

	// Mount, extract tar, unmount.
	mountDir := ext4Path + ".mount"
	os.MkdirAll(mountDir, 0755)
	defer os.RemoveAll(mountDir)

	mountCmd := exec.CommandContext(ctx, "mount", "-o", "loop", ext4Path, mountDir)
	if err := mountCmd.Run(); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer exec.CommandContext(ctx, "umount", mountDir).Run()

	tarCmd := exec.CommandContext(ctx, "tar", "xf", tarPath, "-C", mountDir)
	if err := tarCmd.Run(); err != nil {
		return fmt.Errorf("extract tar: %w", err)
	}

	return nil
}

// injectAgent copies the fc-agent binary into the rootfs at /usr/bin/fc-agent.
func (im *ImageManager) injectAgent(ctx context.Context, ext4Path string) error {
	mountDir := ext4Path + ".mount"
	os.MkdirAll(mountDir, 0755)
	defer os.RemoveAll(mountDir)

	mountCmd := exec.CommandContext(ctx, "mount", "-o", "loop", ext4Path, mountDir)
	if err := mountCmd.Run(); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer exec.CommandContext(ctx, "umount", mountDir).Run()

	agentDst := filepath.Join(mountDir, "usr", "bin", "fc-agent")
	os.MkdirAll(filepath.Dir(agentDst), 0755)

	if err := copyFile(im.cfg.AgentBinaryPath, agentDst); err != nil {
		return fmt.Errorf("copy agent: %w", err)
	}
	return os.Chmod(agentDst, 0755)
}
