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
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/danievanzyl/pyro/internal/sandbox/imageconfig"
	"github.com/danievanzyl/pyro/internal/sandbox/imageops"
	"github.com/danievanzyl/pyro/internal/sandbox/imagestate"
	"github.com/danievanzyl/pyro/internal/sandbox/registry"
)

// imageMetaName is the on-disk metadata sidecar persisted next to
// rootfs.ext4. Stores the resolved manifest digest + source so GET can
// surface them after a successful pull (the ledger entry is dropped on
// Complete; the sidecar is the source of truth for ready images).
const imageMetaName = "image-meta.json"

// ImageConfig configures image management.
type ImageConfig struct {
	// ImagesDir is the base directory for all images.
	ImagesDir string

	// AgentBinaryPath is the path to the compiled pyro-agent binary.
	// It gets injected into new rootfs images at /usr/bin/pyro-agent.
	AgentBinaryPath string
}

// ImageManager handles base image lifecycle.
type ImageManager struct {
	cfg    ImageConfig
	log    *slog.Logger
	ledger *imagestate.Ledger
}

// ImageInfo describes a stored base image.
//
// `omitzero` JSON tags keep the response payload terse for in-flight or
// disk-only views (e.g. a pulling entry has no rootfs path; a ready disk
// image without a meta sidecar has no digest).
type ImageInfo struct {
	Name       string            `json:"name"`
	Status     imagestate.Status `json:"status,omitzero"`
	Source     string            `json:"source,omitzero"`
	Digest     string            `json:"digest,omitzero"`
	Error      string            `json:"error,omitzero"`
	RootfsPath string            `json:"rootfs_path,omitzero"`
	KernelPath string            `json:"kernel_path,omitzero"`
	Size       int64             `json:"size,omitzero"` // rootfs size in bytes
	CreatedAt  time.Time         `json:"created_at,omitzero"`
}

// imageMeta is the disk sidecar for a ready image.
type imageMeta struct {
	Digest string `json:"digest,omitzero"`
	Source string `json:"source,omitzero"`
}

// NewImageManager creates an image manager.
func NewImageManager(cfg ImageConfig, log *slog.Logger) (*ImageManager, error) {
	if err := os.MkdirAll(cfg.ImagesDir, 0750); err != nil {
		return nil, fmt.Errorf("create images dir: %w", err)
	}
	return &ImageManager{
		cfg:    cfg,
		log:    log,
		ledger: imagestate.New(imagestate.RealClock(), imagestate.DefaultFailedTTL),
	}, nil
}

// Ledger exposes the in-memory pull ledger so the API layer can surface
// in-flight and recently-failed pull state.
func (im *ImageManager) Ledger() *imagestate.Ledger { return im.ledger }

// KernelInfo describes an available guest kernel.
type KernelInfo struct {
	Version string `json:"version"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
}

// ListKernels returns all vmlinux-* kernels in the images dir, sorted by version descending.
func (im *ImageManager) ListKernels() ([]*KernelInfo, error) {
	entries, err := os.ReadDir(im.cfg.ImagesDir)
	if err != nil {
		return nil, fmt.Errorf("read images dir: %w", err)
	}

	var kernels []*KernelInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		version, ok := strings.CutPrefix(name, "vmlinux-")
		if !ok {
			continue
		}
		path := filepath.Join(im.cfg.ImagesDir, name)
		info, err := entry.Info()
		if err != nil {
			continue
		}
		kernels = append(kernels, &KernelInfo{
			Version: version,
			Path:    path,
			Size:    info.Size(),
		})
	}

	// Sort descending by version string (higher versions first).
	slices.SortFunc(kernels, func(a, b *KernelInfo) int {
		return cmp.Compare(b.Version, a.Version)
	})

	return kernels, nil
}

// ResolveKernel returns the path for a kernel version, or the latest if version is empty.
func (im *ImageManager) ResolveKernel(version string) (string, error) {
	kernels, err := im.ListKernels()
	if err != nil {
		return "", err
	}
	if len(kernels) == 0 {
		return "", fmt.Errorf("no kernels found in %s", im.cfg.ImagesDir)
	}
	if version == "" {
		return kernels[0].Path, nil // latest (sorted descending)
	}
	for _, k := range kernels {
		if k.Version == version {
			return k.Path, nil
		}
	}
	return "", fmt.Errorf("kernel version %q not found", version)
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

	// Fall back to shared kernel in images root if no per-image kernel.
	if _, err := os.Stat(kernel); err != nil {
		shared := filepath.Join(im.cfg.ImagesDir, "vmlinux")
		if _, err := os.Stat(shared); err != nil {
			return nil, fmt.Errorf("kernel not found for image %q: %w", name, err)
		}
		kernel = shared
	}

	info := &ImageInfo{
		Name:       name,
		Status:     imagestate.StatusReady,
		RootfsPath: rootfs,
		KernelPath: kernel,
		Size:       rootfsInfo.Size(),
		CreatedAt:  rootfsInfo.ModTime(),
	}
	// Sidecar is best-effort — pre-existing images registered before the
	// async pull path won't have one and that's fine.
	if meta, err := readImageMeta(filepath.Join(dir, imageMetaName)); err == nil && meta != nil {
		info.Digest = meta.Digest
		info.Source = meta.Source
	}
	return info, nil
}

// Status returns the live view of an image: ledger entry if one exists
// (in-flight or recently-failed), otherwise the disk record. Returns nil
// if the image is unknown to both the ledger and the disk.
func (im *ImageManager) Status(name string) *ImageInfo {
	if op := im.ledger.Get(name); op != nil {
		return &ImageInfo{
			Name:   op.Name,
			Status: op.Status,
			Source: op.Source,
			Digest: op.Digest,
			Error:  op.Error,
		}
	}
	info, err := im.Get(name)
	if err != nil {
		return nil
	}
	return info
}

func readImageMeta(path string) (*imageMeta, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var m imageMeta
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func writeImageMeta(path string, m imageMeta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// CreateFromDockerfile builds a rootfs from a Dockerfile.
// It uses docker/buildah to build the image, then exports the filesystem
// to an ext4 image with the pyro-agent binary injected.
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
	dockerTag := fmt.Sprintf("pyro/%s:latest", name)
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

	// Inject pyro-agent binary into rootfs.
	if im.cfg.AgentBinaryPath != "" {
		if err := im.injectAgent(ctx, rootfs); err != nil {
			os.RemoveAll(dir)
			return nil, fmt.Errorf("inject agent: %w", err)
		}
	}

	// Persist the image's runtime defaults (Env/WorkingDir/User) so the agent
	// can apply them per exec — parity with the registry-pull path.
	imgCfg, err := dockerInspectConfig(ctx, dockerTag)
	if err != nil {
		im.log.Warn("could not extract image config via docker inspect", "err", err)
	} else if err := im.writeImageConfigToRootfs(ctx, rootfs, imgCfg); err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("write image config: %w", err)
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

// CreateFromRegistry registers an image pull from a remote OCI registry.
// Asynchronous: returns immediately with status=pulling. The pull and
// extraction proceed in a background goroutine that drives the ledger
// through pulling → extracting → ready (or failed). Callers poll
// ImageManager.Status(name) or the ledger to observe progress.
//
// On failure the partial image directory is removed and the ledger
// records the error. The failed entry stays queryable for ~1h.
//
// puller may be nil; in that case a default Puller is created.
func (im *ImageManager) CreateFromRegistry(ctx context.Context, name, source string, puller *registry.Puller) (*ImageInfo, error) {
	if puller == nil {
		puller = registry.New()
	}

	op, attached := im.ledger.Begin(name, source)
	if attached {
		// Slice 05 wires concurrent attach end-to-end; for now both callers
		// see the same in-flight state.
		return &ImageInfo{
			Name:   op.Name,
			Status: op.Status,
			Source: op.Source,
			Digest: op.Digest,
		}, nil
	}

	// Detach from the request context — the request returns immediately
	// but the pull must outlive it.
	go im.runRegistryPull(context.Background(), name, source, puller)

	return &ImageInfo{
		Name:   op.Name,
		Status: op.Status,
		Source: op.Source,
	}, nil
}

func (im *ImageManager) runRegistryPull(ctx context.Context, name, source string, puller *registry.Puller) {
	dir := filepath.Join(im.cfg.ImagesDir, name)
	rootfs := filepath.Join(dir, "rootfs.ext4")

	fail := func(err error) {
		im.log.Warn("image pull failed", "name", name, "source", source, "err", err)
		os.RemoveAll(dir)
		_ = im.ledger.Fail(name, err)
	}

	manifest, err := puller.Resolve(ctx, source)
	if err != nil {
		fail(fmt.Errorf("resolve %s: %w", source, err))
		return
	}
	_ = im.ledger.SetDigest(name, manifest.Digest)

	if err := os.MkdirAll(dir, 0o750); err != nil {
		fail(fmt.Errorf("create image dir: %w", err))
		return
	}

	im.log.Info("pulling image from registry",
		"name", name, "source", source,
		"digest", manifest.Digest, "layers", len(manifest.Layers))

	// Estimate size: sum of layer sizes × 1.3 for ext4 overhead, with 64 MiB floor.
	var totalBytes int64
	for _, l := range manifest.Layers {
		totalBytes += l.Size
	}
	sizeMB := int(totalBytes/(1<<20)*13/10) + 64

	if err := im.ledger.Update(name, imagestate.StatusExtracting); err != nil {
		fail(fmt.Errorf("ledger transition: %w", err))
		return
	}

	builder := imageops.NewExt4Builder()
	mount, err := builder.Create(ctx, rootfs, sizeMB)
	if err != nil {
		fail(fmt.Errorf("create ext4: %w", err))
		return
	}

	extractor := imageops.NewLayerExtractor()
	for _, layer := range manifest.Layers {
		rc, err := manifest.LayerReader(layer.Digest)
		if err != nil {
			mount.Close()
			fail(fmt.Errorf("open layer %s: %w", layer.Digest, err))
			return
		}
		if err := extractor.Extract(mount.MountDir, rc); err != nil {
			rc.Close()
			mount.Close()
			fail(fmt.Errorf("extract layer %s: %w", layer.Digest, err))
			return
		}
		rc.Close()
	}

	// Inject agent while still mounted.
	if im.cfg.AgentBinaryPath != "" {
		injector := imageops.NewAgentInjector(im.cfg.AgentBinaryPath)
		if err := injector.Inject(mount.MountDir); err != nil {
			mount.Close()
			fail(fmt.Errorf("inject agent: %w", err))
			return
		}
	}

	// Persist Env/WorkDir/User defaults so the agent can apply them per exec.
	imgCfg := registry.ExtractConfig(manifest)
	if err := imageops.WriteImageConfig(mount.MountDir, imgCfg); err != nil {
		mount.Close()
		fail(fmt.Errorf("write image config: %w", err))
		return
	}

	if err := mount.Close(); err != nil {
		fail(fmt.Errorf("unmount: %w", err))
		return
	}

	// Copy default kernel for parity with CreateFromDockerfile.
	defaultKernel := filepath.Join(im.cfg.ImagesDir, "default", "vmlinux")
	kernel := filepath.Join(dir, "vmlinux")
	if err := copyFile(defaultKernel, kernel); err != nil {
		im.log.Warn("could not copy default kernel", "err", err)
	}

	// Persist source + digest sidecar so GET surfaces them once the
	// ledger entry drops.
	if err := writeImageMeta(filepath.Join(dir, imageMetaName), imageMeta{
		Digest: manifest.Digest,
		Source: source,
	}); err != nil {
		fail(fmt.Errorf("write image meta: %w", err))
		return
	}

	if err := im.ledger.Complete(name); err != nil {
		// Couldn't transition the ledger but the on-disk image is sound;
		// keep it and surface the inconsistency in the log.
		im.log.Warn("ledger complete failed", "name", name, "err", err)
	}
	im.log.Info("image registered from registry",
		"name", name, "digest", manifest.Digest, "rootfs", rootfs)
}

// dockerInspectConfig pulls Env/WorkingDir/User out of `docker inspect` for a
// built image tag. Returns a zero-value config if the field is absent.
func dockerInspectConfig(ctx context.Context, target string) (imageconfig.ImageConfig, error) {
	out, err := exec.CommandContext(ctx, "docker", "inspect", "--format={{json .Config}}", target).Output()
	if err != nil {
		return imageconfig.ImageConfig{}, fmt.Errorf("docker inspect: %w", err)
	}
	var raw struct {
		Env        []string `json:"Env"`
		WorkingDir string   `json:"WorkingDir"`
		User       string   `json:"User"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return imageconfig.ImageConfig{}, fmt.Errorf("parse docker inspect output: %w", err)
	}
	return imageconfig.ImageConfig{
		Env:     raw.Env,
		WorkDir: raw.WorkingDir,
		User:    raw.User,
	}, nil
}

// writeImageConfigToRootfs mounts ext4Path and writes the image config file.
// Linux-only at runtime (uses mount/umount); compiles everywhere.
func (im *ImageManager) writeImageConfigToRootfs(ctx context.Context, ext4Path string, cfg imageconfig.ImageConfig) error {
	mountDir := ext4Path + ".cfg-mount"
	if err := os.MkdirAll(mountDir, 0o755); err != nil {
		return fmt.Errorf("mkdir mount: %w", err)
	}
	defer os.RemoveAll(mountDir)

	mountCmd := exec.CommandContext(ctx, "mount", "-o", "loop", ext4Path, mountDir)
	if err := mountCmd.Run(); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer exec.CommandContext(ctx, "umount", mountDir).Run()

	return imageops.WriteImageConfig(mountDir, cfg)
}

// injectAgent copies the pyro-agent binary into the rootfs at /usr/bin/pyro-agent.
func (im *ImageManager) injectAgent(ctx context.Context, ext4Path string) error {
	mountDir := ext4Path + ".mount"
	os.MkdirAll(mountDir, 0755)
	defer os.RemoveAll(mountDir)

	mountCmd := exec.CommandContext(ctx, "mount", "-o", "loop", ext4Path, mountDir)
	if err := mountCmd.Run(); err != nil {
		return fmt.Errorf("mount: %w", err)
	}
	defer exec.CommandContext(ctx, "umount", mountDir).Run()

	agentDst := filepath.Join(mountDir, "usr", "bin", "pyro-agent")
	os.MkdirAll(filepath.Dir(agentDst), 0755)

	if err := copyFile(im.cfg.AgentBinaryPath, agentDst); err != nil {
		return fmt.Errorf("copy agent: %w", err)
	}
	return os.Chmod(agentDst, 0755)
}
