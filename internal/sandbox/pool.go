// Package sandbox — pool.go implements pre-warmed snapshot pools.
//
// The pool maintains N ready-to-use VM snapshots per image. When a sandbox
// is requested, it restores from snapshot (~50ms) instead of cold-booting
// (~1-3s). The pool auto-replenishes in the background.
//
// Pool lifecycle:
//
//	┌───────────┐   boot+snapshot   ┌──────────────┐   restore   ┌─────────┐
//	│ WARMING   │──────────────────▶│ POOL (ready)  │────────────▶│ CLAIMED │
//	│ (cold boot│                   │ N snapshots   │             │ (in use)│
//	│  + snap)  │                   │ per image     │             │         │
//	└───────────┘                   └──────────────┘             └─────────┘
//	      ▲                               │ count < target
//	      └───────────────────────────────┘ (auto-replenish)
package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// PoolConfig configures the snapshot pool.
type PoolConfig struct {
	// TargetSize is the number of warm snapshots to maintain per image.
	TargetSize int

	// SnapshotDir is where snapshot files are stored.
	SnapshotDir string

	// ReplenishInterval is how often to check and refill the pool.
	ReplenishInterval time.Duration

	// Images is the list of images to maintain warm pools for.
	// If empty, discovers from ImagesDir.
	Images []string
}

// snapshot represents a pre-warmed VM snapshot ready for fast restore.
type snapshot struct {
	ID         string
	Image      string
	MemFile    string // path to memory snapshot
	SnapFile   string // path to VM state snapshot
	VsockCID   uint32
	CreatedAt  time.Time
}

// Pool manages pre-warmed Firecracker snapshots for fast sandbox creation.
type Pool struct {
	cfg     PoolConfig
	manager *Manager
	log     *slog.Logger

	mu    sync.Mutex
	ready map[string][]*snapshot // image name → available snapshots
}

// NewPool creates a snapshot pool.
func NewPool(cfg PoolConfig, manager *Manager, log *slog.Logger) (*Pool, error) {
	if err := os.MkdirAll(cfg.SnapshotDir, 0750); err != nil {
		return nil, fmt.Errorf("create snapshot dir: %w", err)
	}

	return &Pool{
		cfg:     cfg,
		manager: manager,
		log:     log,
		ready:   make(map[string][]*snapshot),
	}, nil
}

// Claim takes a ready snapshot from the pool for the given image.
// Returns nil if none available (caller should fall back to cold boot).
func (p *Pool) Claim(image string) *snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	snaps := p.ready[image]
	if len(snaps) == 0 {
		return nil
	}

	// Pop the oldest snapshot (FIFO).
	s := snaps[0]
	p.ready[image] = snaps[1:]

	p.log.Info("snapshot claimed from pool",
		"image", image,
		"snap_id", s.ID,
		"remaining", len(p.ready[image]))

	return s
}

// Available returns the number of ready snapshots for an image.
func (p *Pool) Available(image string) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ready[image])
}

// Stats returns pool statistics.
func (p *Pool) Stats() map[string]int {
	p.mu.Lock()
	defer p.mu.Unlock()
	stats := make(map[string]int)
	for img, snaps := range p.ready {
		stats[img] = len(snaps)
	}
	return stats
}

// Run starts the pool replenishment loop. Blocks until ctx is cancelled.
func (p *Pool) Run(ctx context.Context) {
	images := p.cfg.Images
	if len(images) == 0 {
		// Discover images from ImagesDir.
		images = p.discoverImages()
	}

	// Clean up any orphaned snapshot dirs from previous failed runs.
	p.cleanupOrphans()

	p.log.Info("snapshot pool started",
		"target_size", p.cfg.TargetSize,
		"images", images,
		"interval", p.cfg.ReplenishInterval)

	// Initial fill.
	for _, img := range images {
		p.replenish(ctx, img)
	}

	ticker := time.NewTicker(p.cfg.ReplenishInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.log.Info("snapshot pool stopped")
			return
		case <-ticker.C:
			for _, img := range images {
				p.replenish(ctx, img)
			}
		}
	}
}

// cleanupOrphans removes snapshot dirs that aren't tracked in the pool.
func (p *Pool) cleanupOrphans() {
	entries, err := os.ReadDir(p.cfg.SnapshotDir)
	if err != nil {
		return
	}

	p.mu.Lock()
	tracked := make(map[string]bool)
	for _, snaps := range p.ready {
		for _, s := range snaps {
			tracked[s.ID] = true
		}
	}
	p.mu.Unlock()

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !tracked[e.Name()] {
			path := filepath.Join(p.cfg.SnapshotDir, e.Name())
			p.log.Info("cleaning orphaned snapshot", "id", e.Name())
			os.RemoveAll(path)
		}
	}
}

// discoverImages finds available image directories.
func (p *Pool) discoverImages() []string {
	dir := p.manager.cfg.ImagesDir
	if dir == "" {
		return []string{"default"}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []string{"default"}
	}
	var images []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Check rootfs exists.
		rootfs := filepath.Join(dir, e.Name(), "rootfs.ext4")
		if _, err := os.Stat(rootfs); err == nil {
			images = append(images, e.Name())
		}
	}
	if len(images) == 0 {
		return []string{"default"}
	}
	return images
}

// replenish creates snapshots until the pool reaches target size for an image.
func (p *Pool) replenish(ctx context.Context, image string) {
	p.mu.Lock()
	current := len(p.ready[image])
	needed := p.cfg.TargetSize - current
	p.mu.Unlock()

	if needed <= 0 {
		return
	}

	p.log.Info("replenishing pool", "image", image, "current", current, "needed", needed)

	for range needed {
		select {
		case <-ctx.Done():
			return
		default:
		}

		snap, err := p.createSnapshot(ctx, image)
		if err != nil {
			p.log.Error("create snapshot failed", "image", image, "err", err)
			return // back off on failure
		}

		p.mu.Lock()
		p.ready[image] = append(p.ready[image], snap)
		p.mu.Unlock()

		p.log.Info("snapshot added to pool",
			"image", image,
			"snap_id", snap.ID,
			"pool_size", p.Available(image))
	}
}

// createSnapshot boots a VM, waits for the agent, then takes a snapshot.
func (p *Pool) createSnapshot(ctx context.Context, image string) (*snapshot, error) {
	// Pre-check disk space — need at least 2GB for rootfs copy + snapshot files.
	if avail, err := availableDiskSpace(p.cfg.SnapshotDir); err == nil && avail < 2*1024*1024*1024 {
		return nil, fmt.Errorf("insufficient disk space: %d MB available, need 2048 MB", avail/1024/1024)
	}

	id := fmt.Sprintf("snap-%d", time.Now().UnixNano())
	snapDir := filepath.Join(p.cfg.SnapshotDir, id)
	if err := os.MkdirAll(snapDir, 0750); err != nil {
		return nil, fmt.Errorf("create snap dir: %w", err)
	}

	cid := p.manager.allocCID()

	// Boot a temporary VM for snapshotting.
	stateDir := filepath.Join(snapDir, "vm")
	if err := os.MkdirAll(stateDir, 0750); err != nil {
		return nil, fmt.Errorf("create vm dir: %w", err)
	}

	socketPath := filepath.Join(stateDir, "firecracker.sock")

	// Resolve image-specific rootfs.
	sourceRootfs := p.manager.cfg.DefaultRootfs
	if p.manager.cfg.ImagesDir != "" && image != "" {
		imgRootfs := filepath.Join(p.manager.cfg.ImagesDir, image, "rootfs.ext4")
		if _, err := os.Stat(imgRootfs); err == nil {
			sourceRootfs = imgRootfs
		}
	}

	// Copy rootfs.
	rootfsPath := filepath.Join(stateDir, "rootfs.ext4")
	if err := copyFile(sourceRootfs, rootfsPath); err != nil {
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("copy rootfs: %w", err)
	}

	// Create temp sandbox for boot.
	tempSB := &tempVM{
		socketPath: socketPath,
		vsockCID:   cid,
		stateDir:   stateDir,
		rootfs:     rootfsPath,
		tapDevice:  fmt.Sprintf("taps%d", cid),
	}

	// Setup networking for the temp VM.
	if err := p.manager.setupNetworking(tempSB.tapDevice); err != nil {
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("setup networking: %w", err)
	}

	// Write config and spawn.
	configPath := filepath.Join(stateDir, "vm-config.json")
	config := fmt.Sprintf(`{
  "boot-source": {
    "kernel_image_path": %q,
    "boot_args": "console=ttyS0 reboot=k panic=1 pci=off init=/usr/bin/pyro-agent"
  },
  "drives": [{
    "drive_id": "rootfs",
    "path_on_host": %q,
    "is_root_device": true,
    "is_read_only": false
  }],
  "machine-config": {
    "vcpu_count": 1,
    "mem_size_mib": 256
  },
  "network-interfaces": [{
    "iface_id": "eth0",
    "guest_mac": "06:00:AC:10:00:02",
    "host_dev_name": %q
  }],
  "vsock": {
    "guest_cid": %d,
    "uds_path": %q
  }
}`, p.manager.cfg.KernelPath, rootfsPath, tempSB.tapDevice, cid,
		filepath.Join(stateDir, "vsock.sock"))

	if err := os.WriteFile(configPath, []byte(config), 0640); err != nil {
		p.manager.cleanupNetworking(tempSB.tapDevice)
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("write config: %w", err)
	}

	// Start Firecracker.
	cmd := newFirecrackerCmd(ctx, p.manager.cfg.FirecrackerBin, socketPath, configPath, stateDir)
	if err := cmd.Start(); err != nil {
		p.manager.cleanupNetworking(tempSB.tapDevice)
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	// Wait for agent using proper ping protocol.
	if err := p.manager.WaitForAgentAt(stateDir, 15*time.Second); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		p.manager.cleanupNetworking(tempSB.tapDevice)
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("agent timeout: %w", err)
	}

	// Pause the VM before snapshotting.
	if err := firecrackerAPICall(socketPath, "PATCH", "/vm", `{"state":"Paused"}`); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		p.manager.cleanupNetworking(tempSB.tapDevice)
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("pause vm: %w", err)
	}

	// Take snapshot.
	memFile := filepath.Join(snapDir, "mem")
	snapFile := filepath.Join(snapDir, "vmstate")
	snapPayload := fmt.Sprintf(`{"snapshot_type":"Full","snapshot_path":%q,"mem_file_path":%q}`,
		snapFile, memFile)

	if err := firecrackerAPICall(socketPath, "PUT", "/snapshot/create", snapPayload); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		p.manager.cleanupNetworking(tempSB.tapDevice)
		os.RemoveAll(snapDir)
		return nil, fmt.Errorf("create snapshot: %w", err)
	}

	// Kill the temp VM — we only need the snapshot files.
	cmd.Process.Kill()
	cmd.Wait() // reap zombie
	p.manager.cleanupNetworking(tempSB.tapDevice)
	// Keep snapDir — it has the snapshot files we need.
	// Remove the temp VM socket and logs.
	os.Remove(socketPath)

	return &snapshot{
		ID:        id,
		Image:     image,
		MemFile:   memFile,
		SnapFile:  snapFile,
		VsockCID:  cid,
		CreatedAt: time.Now(),
	}, nil
}

// availableDiskSpace returns free bytes on the filesystem containing path.
func availableDiskSpace(path string) (uint64, error) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	return stat.Bavail * uint64(stat.Bsize), nil
}

type tempVM struct {
	socketPath string
	vsockCID   uint32
	stateDir   string
	rootfs     string
	tapDevice  string
}
