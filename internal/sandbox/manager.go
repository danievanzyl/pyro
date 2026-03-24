// Package sandbox manages Firecracker microVM lifecycles.
//
// Architecture:
//
//	Manager
//	  ├── CreateSandbox()  → spawn jailer + firecracker, wait for vsock agent
//	  ├── ExecInSandbox()  → connect vsock, send ExecRequest, return ExecResponse
//	  ├── DestroySandbox() → SIGKILL process, cleanup state dir + tap + DB
//	  ├── Reconcile()      → startup recovery of orphaned VMs
//	  └── cidAllocator     → atomic counter for unique vsock CIDs
package sandbox

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/KarpelesLab/reflink"
	"github.com/danievanzyl/pyro/internal/protocol"
	"github.com/danievanzyl/pyro/internal/store"
	"github.com/google/uuid"
)

// Config holds sandbox manager configuration.
type Config struct {
	// StateDir is the base directory for VM state (sockets, metadata).
	// Each VM gets a subdirectory: {StateDir}/{sandbox-id}/
	StateDir string

	// FirecrackerBin is the path to the firecracker binary.
	FirecrackerBin string

	// JailerBin is the path to the jailer binary.
	JailerBin string

	// KernelPath is the path to the guest kernel image.
	KernelPath string

	// DefaultRootfs is the path to the default rootfs ext4 image.
	DefaultRootfs string

	// ImagesDir is the directory containing image subdirectories.
	// Each image has {ImagesDir}/{name}/rootfs.ext4.
	ImagesDir string

	// BridgeName is the network bridge for VM tap devices.
	BridgeName string

	// VsockAgentPort is the port the in-VM agent listens on inside vsock.
	VsockAgentPort uint32

	// ExecTimeout is the default timeout for command execution.
	ExecTimeout time.Duration

	// MaxSandboxes is the maximum number of concurrent sandboxes.
	MaxSandboxes int

	// MaxVCPU is the max vCPUs per sandbox (0 = no limit).
	MaxVCPU int

	// MaxMemMiB is the max memory per sandbox in MiB (0 = no limit).
	MaxMemMiB int

	// DefaultVCPU is the default vCPU count when not specified.
	DefaultVCPU int

	// DefaultMemMiB is the default memory in MiB when not specified.
	DefaultMemMiB int

	// Metrics for recording phase timings (optional).
	Metrics interface {
		RecordCreatePhase(ctx context.Context, image, phase string, duration time.Duration)
	}
}

// Manager handles Firecracker VM lifecycle operations.
type Manager struct {
	cfg   Config
	store *store.Store
	log   *slog.Logger
	pool  *Pool

	nextCID atomic.Uint32

	mu       sync.RWMutex
	active   map[string]*vmHandle // sandbox ID → handle
	stopping bool
}

// SetPool attaches a snapshot pool to the manager.
func (m *Manager) SetPool(p *Pool) {
	m.pool = p
}

// vmHandle holds runtime state for a running VM.
type vmHandle struct {
	sandbox *store.Sandbox
	cmd     *exec.Cmd
	cancel  context.CancelFunc
}

// New creates a sandbox Manager.
func New(cfg Config, st *store.Store, log *slog.Logger) (*Manager, error) {
	if err := os.MkdirAll(cfg.StateDir, 0750); err != nil {
		return nil, fmt.Errorf("create state dir: %w", err)
	}

	m := &Manager{
		cfg:    cfg,
		store:  st,
		log:    log,
		active: make(map[string]*vmHandle),
	}
	// Start CID allocation at 3 (0-2 are reserved in vsock).
	m.nextCID.Store(3)

	return m, nil
}

// allocCID returns a unique vsock context ID.
func (m *Manager) allocCID() uint32 {
	return m.nextCID.Add(1)
}

// ActiveCount returns the number of running sandboxes.
func (m *Manager) ActiveCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.active)
}

// ErrAtCapacity is returned when the sandbox manager can't create more VMs.
var ErrAtCapacity = errors.New("at capacity")

// VMResources holds configurable VM resources.
type VMResources struct {
	VCPU       int    // 0 = use default
	MemMiB     int    // 0 = use default
	KernelPath string // empty = use server default
}

// CreateSandbox provisions a new Firecracker microVM.
func (m *Manager) CreateSandbox(ctx context.Context, apiKeyID, image string, ttl time.Duration, res VMResources) (*store.Sandbox, error) {
	createStart := time.Now()
	if m.ActiveCount() >= m.cfg.MaxSandboxes {
		return nil, fmt.Errorf("%w: %d/%d sandboxes running", ErrAtCapacity, m.ActiveCount(), m.cfg.MaxSandboxes)
	}

	id := uuid.New().String()
	cid := m.allocCID()
	now := time.Now().UTC()
	stateDir := filepath.Join(m.cfg.StateDir, id)

	if err := os.MkdirAll(stateDir, 0750); err != nil {
		return nil, fmt.Errorf("create sandbox state dir: %w", err)
	}

	socketPath := filepath.Join(stateDir, "firecracker.sock")
	tapDevice := fmt.Sprintf("tap-%s", id[:8])

	sb := &store.Sandbox{
		ID:         id,
		APIKeyID:   apiKeyID,
		State:      store.StateCreating,
		Image:      image,
		VsockCID:   cid,
		SocketPath: socketPath,
		TapDevice:  tapDevice,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
		StateDir:   stateDir,
	}

	if err := m.store.CreateSandbox(ctx, sb); err != nil {
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("store sandbox: %w", err)
	}

	m.log.Info("create: db done", "id", id[:8], "elapsed", time.Since(createStart))

	// Create tap device and attach to bridge.
	if err := m.setupNetworking(tapDevice); err != nil {
		m.store.UpdateSandboxState(ctx, id, store.StateDestroyed)
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("setup networking: %w", err)
	}

	m.log.Info("create: network done", "id", id[:8], "elapsed", time.Since(createStart))

	// Resolve rootfs: try image-specific path first, fall back to default.
	sourceRootfs := m.cfg.DefaultRootfs
	if m.cfg.ImagesDir != "" && image != "" {
		imgRootfs := filepath.Join(m.cfg.ImagesDir, image, "rootfs.ext4")
		// Guard against path traversal: resolved path must stay under ImagesDir.
		absImagesDir, _ := filepath.Abs(m.cfg.ImagesDir)
		absImgRootfs, _ := filepath.Abs(imgRootfs)
		if !strings.HasPrefix(absImgRootfs, absImagesDir+string(filepath.Separator)) {
			m.store.UpdateSandboxState(ctx, id, store.StateDestroyed)
			os.RemoveAll(stateDir)
			return nil, fmt.Errorf("invalid image path")
		}
		if _, err := os.Stat(imgRootfs); err == nil {
			sourceRootfs = imgRootfs
		}
	}

	// Copy rootfs for this sandbox (each VM needs its own writable copy).
	rootfsStart := time.Now()
	rootfsPath := filepath.Join(stateDir, "rootfs.ext4")
	if err := copyFile(sourceRootfs, rootfsPath); err != nil {
		m.cleanupNetworking(tapDevice)
		m.store.UpdateSandboxState(ctx, id, store.StateDestroyed)
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("copy rootfs: %w", err)
	}
	rootfsDur := time.Since(rootfsStart)
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.RecordCreatePhase(ctx, image, "rootfs_copy", rootfsDur)
	}

	m.log.Info("create: rootfs copied", "id", id[:8], "elapsed", time.Since(createStart))

	// Resolve VM resources: request override → config defaults.
	vcpu := cmp.Or(res.VCPU, m.cfg.DefaultVCPU, 1)
	memMiB := cmp.Or(res.MemMiB, m.cfg.DefaultMemMiB, 256)
	// Enforce limits.
	if m.cfg.MaxVCPU > 0 && vcpu > m.cfg.MaxVCPU {
		vcpu = m.cfg.MaxVCPU
	}
	if m.cfg.MaxMemMiB > 0 && memMiB > m.cfg.MaxMemMiB {
		memMiB = m.cfg.MaxMemMiB
	}

	// Spawn Firecracker.
	spawnStart := time.Now()
	vmCtx, vmCancel := context.WithCancel(context.Background())
	kernelPath := res.KernelPath
	if kernelPath == "" {
		kernelPath = m.cfg.KernelPath
	}
	cmd, err := m.spawnFirecracker(vmCtx, sb, rootfsPath, kernelPath, vcpu, memMiB)
	if err != nil {
		vmCancel()
		m.cleanupNetworking(tapDevice)
		m.store.UpdateSandboxState(ctx, id, store.StateDestroyed)
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("spawn firecracker: %w", err)
	}
	spawnDur := time.Since(spawnStart)
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.RecordCreatePhase(ctx, image, "spawn", spawnDur)
	}

	sb.PID = cmd.Process.Pid
	if err := m.store.UpdateSandboxPID(ctx, id, sb.PID, socketPath); err != nil {
		vmCancel()
		m.killProcess(cmd)
		m.cleanupNetworking(tapDevice)
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("update sandbox pid: %w", err)
	}

	m.log.Info("create: firecracker spawned", "id", id[:8], "pid", cmd.Process.Pid, "elapsed", time.Since(createStart))

	// Wait for vsock agent to become ready.
	agentStart := time.Now()
	if err := m.waitForAgent(sb, 15*time.Second); err != nil {
		vmCancel()
		m.killProcess(cmd)
		m.cleanupNetworking(tapDevice)
		m.store.UpdateSandboxState(ctx, id, store.StateDestroyed)
		os.RemoveAll(stateDir)
		return nil, fmt.Errorf("agent not ready: %w", err)
	}

	agentDur := time.Since(agentStart)
	if m.cfg.Metrics != nil {
		m.cfg.Metrics.RecordCreatePhase(ctx, image, "agent_wait", agentDur)
	}

	sb.State = store.StateRunning
	handle := &vmHandle{sandbox: sb, cmd: cmd, cancel: vmCancel}

	m.mu.Lock()
	m.active[id] = handle
	m.mu.Unlock()

	m.log.Info("sandbox created",
		"id", id, "pid", sb.PID, "cid", cid,
		"ttl", ttl, "expires_at", sb.ExpiresAt)

	return sb, nil
}

// ExecInSandbox runs a command inside a sandbox via vsock.
func (m *Manager) ExecInSandbox(ctx context.Context, id string, req *protocol.ExecRequest) (*protocol.ExecResponse, error) {
	m.mu.RLock()
	handle, ok := m.active[id]
	m.mu.RUnlock()

	if !ok {
		sb, err := m.store.GetSandbox(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("get sandbox: %w", err)
		}
		if sb == nil {
			return nil, fmt.Errorf("sandbox %s not found", id)
		}
		if sb.State == store.StateDestroyed || sb.IsExpired() {
			return nil, fmt.Errorf("sandbox %s is expired or destroyed", id)
		}
		return nil, fmt.Errorf("sandbox %s not in active set", id)
	}

	if handle.sandbox.IsExpired() {
		return nil, fmt.Errorf("sandbox %s has expired", id)
	}

	timeout := m.cfg.ExecTimeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	execCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	conn, err := m.dialVsock(handle.sandbox.VsockCID, m.cfg.VsockAgentPort)
	if err != nil {
		return nil, fmt.Errorf("connect vsock: %w", err)
	}
	defer conn.Close()

	// Set deadline from context.
	if deadline, ok := execCtx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	env := &protocol.Envelope{
		Type:    protocol.TypeExecRequest,
		Payload: req,
	}
	if err := protocol.WriteMessage(conn, env); err != nil {
		return nil, fmt.Errorf("send exec request: %w", err)
	}

	resp, err := protocol.ReadMessage(conn)
	if err != nil {
		return nil, fmt.Errorf("read exec response: %w", err)
	}

	if resp.Type == protocol.TypeError {
		errResp, _ := protocol.DecodePayload[protocol.ErrorResponse](resp)
		if errResp != nil {
			return nil, fmt.Errorf("agent error: %s", errResp.Message)
		}
		return nil, fmt.Errorf("agent returned error")
	}

	execResp, err := protocol.DecodePayload[protocol.ExecResponse](resp)
	if err != nil {
		return nil, fmt.Errorf("decode exec response: %w", err)
	}

	return execResp, nil
}

// DestroySandbox kills a VM and cleans up all resources.
func (m *Manager) DestroySandbox(ctx context.Context, id string) error {
	m.mu.Lock()
	handle, ok := m.active[id]
	if ok {
		delete(m.active, id)
	}
	m.mu.Unlock()

	if handle != nil {
		handle.cancel()
		m.killProcess(handle.cmd)
		m.cleanupNetworking(handle.sandbox.TapDevice)
		os.RemoveAll(handle.sandbox.StateDir)
	} else {
		// Not in active set — try to clean up from DB state.
		sb, err := m.store.GetSandbox(ctx, id)
		if err != nil {
			return fmt.Errorf("get sandbox: %w", err)
		}
		if sb == nil {
			return fmt.Errorf("sandbox %s not found", id)
		}
		if sb.PID > 0 {
			syscall.Kill(sb.PID, syscall.SIGKILL)
		}
		m.cleanupNetworking(sb.TapDevice)
		os.RemoveAll(sb.StateDir)
	}

	if err := m.store.UpdateSandboxState(ctx, id, store.StateDestroyed); err != nil {
		m.log.Error("update sandbox state", "id", id, "err", err)
	}

	m.log.Info("sandbox destroyed", "id", id)
	return nil
}

// Reconcile recovers orphaned VMs after a server restart.
func (m *Manager) Reconcile(ctx context.Context) error {
	sandboxes, err := m.store.GetAllActiveSandboxes(ctx)
	if err != nil {
		return fmt.Errorf("get active sandboxes: %w", err)
	}

	m.log.Info("reconciling sandboxes", "count", len(sandboxes))

	for _, sb := range sandboxes {
		if sb.IsExpired() {
			m.log.Info("killing expired orphan", "id", sb.ID, "pid", sb.PID)
			if sb.PID > 0 {
				syscall.Kill(sb.PID, syscall.SIGKILL)
			}
			m.cleanupNetworking(sb.TapDevice)
			os.RemoveAll(sb.StateDir)
			m.store.UpdateSandboxState(ctx, sb.ID, store.StateDestroyed)
			continue
		}

		// Check if process is still alive.
		if sb.PID > 0 && processAlive(sb.PID) {
			m.log.Info("reclaiming orphan", "id", sb.ID, "pid", sb.PID)
			// Re-add to active set. We don't have the exec.Cmd but
			// we can still kill by PID and track the sandbox.
			handle := &vmHandle{
				sandbox: sb,
				cancel:  func() {}, // no-op cancel for reclaimed VMs
			}
			m.mu.Lock()
			m.active[sb.ID] = handle
			m.mu.Unlock()

			// Update CID counter to avoid collisions.
			if sb.VsockCID >= m.nextCID.Load() {
				m.nextCID.Store(sb.VsockCID + 1)
			}
		} else {
			m.log.Info("cleaning dead orphan", "id", sb.ID, "pid", sb.PID)
			m.cleanupNetworking(sb.TapDevice)
			os.RemoveAll(sb.StateDir)
			m.store.UpdateSandboxState(ctx, sb.ID, store.StateDestroyed)
		}
	}

	return nil
}

// Shutdown gracefully stops all sandboxes.
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	m.stopping = true
	handles := make([]*vmHandle, 0, len(m.active))
	for _, h := range m.active {
		handles = append(handles, h)
	}
	m.mu.Unlock()

	m.log.Info("shutting down", "active_sandboxes", len(handles))
	for _, h := range handles {
		m.DestroySandbox(ctx, h.sandbox.ID)
	}
}

// spawnFirecracker starts a Firecracker process.
func (m *Manager) spawnFirecracker(ctx context.Context, sb *store.Sandbox, rootfsPath, kernelPath string, vcpu, memMiB int) (*exec.Cmd, error) {
	// Build the Firecracker config JSON and write to state dir.
	configPath := filepath.Join(sb.StateDir, "vm-config.json")
	config := fmt.Sprintf(`{
  "boot-source": {
    "kernel_image_path": %q,
    "boot_args": "console=ttyS0 reboot=k panic=1 pci=off root=/dev/vda rw init=/usr/bin/pyro-agent pyro.ip=172.16.0.%d pyro.gw=172.16.0.1"
  },
  "drives": [{
    "drive_id": "rootfs",
    "path_on_host": %q,
    "is_root_device": true,
    "is_read_only": false
  }],
  "machine-config": {
    "vcpu_count": %d,
    "mem_size_mib": %d
  },
  "network-interfaces": [{
    "iface_id": "eth0",
    "guest_mac": "06:00:AC:10:00:%02x",
    "host_dev_name": %q
  }],
  "vsock": {
    "guest_cid": %d,
    "uds_path": %q
  }
}`, kernelPath, sb.VsockCID, rootfsPath, vcpu, memMiB, sb.VsockCID&0xFF, sb.TapDevice, sb.VsockCID,
		filepath.Join(sb.StateDir, "vsock.sock"))

	if err := os.WriteFile(configPath, []byte(config), 0640); err != nil {
		return nil, fmt.Errorf("write vm config: %w", err)
	}

	// Spawn firecracker directly (jailer integration is Phase 2 hardening).
	args := []string{
		"--api-sock", sb.SocketPath,
		"--config-file", configPath,
	}

	cmd := exec.CommandContext(ctx, m.cfg.FirecrackerBin, args...)
	cmd.Dir = sb.StateDir

	// Log stdout/stderr to files.
	stdout, err := os.Create(filepath.Join(sb.StateDir, "stdout.log"))
	if err != nil {
		return nil, fmt.Errorf("create stdout log: %w", err)
	}
	stderr, err := os.Create(filepath.Join(sb.StateDir, "stderr.log"))
	if err != nil {
		stdout.Close()
		return nil, fmt.Errorf("create stderr log: %w", err)
	}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		stdout.Close()
		stderr.Close()
		return nil, fmt.Errorf("start firecracker: %w", err)
	}

	// Detach log file handles — the process owns them now.
	go func() {
		cmd.Wait()
		stdout.Close()
		stderr.Close()
	}()

	return cmd, nil
}

// WaitForAgentAt polls the vsock agent at a given UDS path until it responds.
func (m *Manager) WaitForAgentAt(stateDir string, timeout time.Duration) error {
	udsPath := filepath.Join(stateDir, "vsock.sock")
	return m.waitForAgentUDS(udsPath, timeout)
}

// waitForAgent polls the vsock agent until it responds to a ping.
func (m *Manager) waitForAgent(sb *store.Sandbox, timeout time.Duration) error {
	return m.waitForAgentUDS(filepath.Join(sb.StateDir, "vsock.sock"), timeout)
}

func (m *Manager) waitForAgentUDS(udsPath string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := dialVsockUDS(udsPath, m.cfg.VsockAgentPort)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		conn.SetDeadline(time.Now().Add(2 * time.Second))
		env := &protocol.Envelope{
			Type:    protocol.TypePingRequest,
			Payload: &protocol.PingRequest{},
		}
		if err := protocol.WriteMessage(conn, env); err != nil {
			conn.Close()
			time.Sleep(200 * time.Millisecond)
			continue
		}

		resp, err := protocol.ReadMessage(conn)
		conn.Close()
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		if resp.Type == protocol.TypePingResponse {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("agent did not respond within %s", timeout)
}

// dialVsock connects to the in-VM agent via Firecracker's vsock UDS.
// Firecracker exposes vsock via a Unix domain socket on the host.
// To connect to a guest port: connect to UDS, send "CONNECT <port>\n",
// receive "OK <cid>\n", then the connection is forwarded to the guest.
func (m *Manager) dialVsock(cid uint32, port uint32) (net.Conn, error) {
	// Find the vsock UDS path for this CID.
	m.mu.RLock()
	var udsPath string
	for _, h := range m.active {
		if h.sandbox.VsockCID == cid {
			udsPath = filepath.Join(h.sandbox.StateDir, "vsock.sock")
			break
		}
	}
	m.mu.RUnlock()

	if udsPath == "" {
		return nil, fmt.Errorf("no vsock UDS found for CID %d", cid)
	}

	return dialVsockUDS(udsPath, port)
}

// dialVsockByPath connects to a vsock UDS directly by path.
func dialVsockUDS(udsPath string, port uint32) (net.Conn, error) {
	conn, err := net.Dial("unix", udsPath)
	if err != nil {
		return nil, fmt.Errorf("connect to vsock UDS %s: %w", udsPath, err)
	}

	// Send CONNECT command.
	connectCmd := fmt.Sprintf("CONNECT %d\n", port)
	if _, err := conn.Write([]byte(connectCmd)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send CONNECT: %w", err)
	}

	// Read response (expect "OK <cid>\n").
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read CONNECT response: %w", err)
	}

	resp := string(buf[:n])
	if len(resp) < 2 || resp[:2] != "OK" {
		conn.Close()
		return nil, fmt.Errorf("vsock CONNECT failed: %s", resp)
	}

	return conn, nil
}

// setupNetworking creates a tap device, attaches it to the bridge, and isolates it.
func (m *Manager) setupNetworking(tapDevice string) error {
	// Create tap device.
	if err := runCmd("ip", "tuntap", "add", tapDevice, "mode", "tap"); err != nil {
		return fmt.Errorf("create tap %s: %w", tapDevice, err)
	}

	// Attach to bridge.
	if err := runCmd("ip", "link", "set", tapDevice, "master", m.cfg.BridgeName); err != nil {
		runCmd("ip", "tuntap", "del", tapDevice, "mode", "tap") // cleanup on failure
		return fmt.Errorf("attach %s to %s: %w", tapDevice, m.cfg.BridgeName, err)
	}

	// Bring up.
	if err := runCmd("ip", "link", "set", tapDevice, "up"); err != nil {
		return fmt.Errorf("bring up %s: %w", tapDevice, err)
	}

	// Isolate: bridge port isolation prevents VMs from talking to each other.
	// They can only reach the bridge gateway (host), not other TAP ports.
	runCmd("bridge", "link", "set", "dev", tapDevice, "isolated", "on")

	return nil
}

// cleanupNetworking removes a tap device.
func (m *Manager) cleanupNetworking(tapDevice string) {
	if tapDevice == "" {
		return
	}
	runCmd("ip", "tuntap", "del", tapDevice, "mode", "tap")
}

// killProcess sends SIGKILL and reaps a Firecracker process with a timeout.
func (m *Manager) killProcess(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	cmd.Process.Kill()

	// Reap with timeout — don't block the HTTP handler if the process is slow to die.
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		m.log.Warn("process reap timed out, continuing cleanup", "pid", cmd.Process.Pid)
	}
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil
}

func runCmd(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %v: %s: %w", name, args, out, err)
	}
	return nil
}

// copyFile copies src to dst using the fastest available method:
// 1. reflink (FICLONE ioctl) — O(1) on CoW filesystems (btrfs, xfs reflink=1)
// 2. copy_file_range — kernel-space copy via page cache
// 3. io.Copy — userspace fallback via sendfile(2)
func copyFile(src, dst string) error {
	if err := reflink.Auto(src, dst); err == nil {
		return os.Chmod(dst, 0640)
	}
	// Fallback to streaming kernel-space copy.
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0640)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
