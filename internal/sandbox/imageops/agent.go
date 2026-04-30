package imageops

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// AgentInjector copies the pyro-agent binary into a mounted rootfs.
// The mount is provided by the caller (Linux loop mount, or any path on macOS
// for testing). Pure file copy — works on any OS.
type AgentInjector struct {
	BinaryPath string
}

// NewAgentInjector constructs an injector.
func NewAgentInjector(binaryPath string) *AgentInjector {
	return &AgentInjector{BinaryPath: binaryPath}
}

// Inject copies the agent binary to {mountDir}/usr/bin/pyro-agent with mode 0755.
func (a *AgentInjector) Inject(mountDir string) error {
	if a.BinaryPath == "" {
		return fmt.Errorf("agent binary path not configured")
	}
	dst := filepath.Join(mountDir, "usr", "bin", "pyro-agent")
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	src, err := os.Open(a.BinaryPath)
	if err != nil {
		return fmt.Errorf("open agent: %w", err)
	}
	defer src.Close()

	_ = os.Remove(dst)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("create dst: %w", err)
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	return os.Chmod(dst, 0o755)
}
