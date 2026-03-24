package store

import "time"

// SandboxState represents the lifecycle state of a sandbox.
type SandboxState string

const (
	StateCreating  SandboxState = "creating"
	StateRunning   SandboxState = "running"
	StateStopping  SandboxState = "stopping"
	StateDestroyed SandboxState = "destroyed"
)

// Sandbox represents a Firecracker microVM sandbox.
type Sandbox struct {
	ID         string       `json:"id"`
	APIKeyID   string       `json:"api_key_id"`
	State      SandboxState `json:"state"`
	Image      string       `json:"image"`
	PID        int          `json:"pid"`
	SocketPath string       `json:"socket_path"`
	VsockCID   uint32       `json:"vsock_cid"`
	TapDevice  string       `json:"tap_device"`
	IP         string       `json:"ip"`
	CreatedAt  time.Time    `json:"created_at"`
	ExpiresAt  time.Time    `json:"expires_at"`
	StateDir   string       `json:"state_dir"`
}

// APIKey represents an authentication key for API access.
type APIKey struct {
	ID        string    `json:"id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
}

// IsExpired returns true if the sandbox TTL has passed.
func (s *Sandbox) IsExpired() bool {
	return time.Now().After(s.ExpiresAt)
}

// RemainingTTL returns the time remaining before expiry.
func (s *Sandbox) RemainingTTL() time.Duration {
	remaining := time.Until(s.ExpiresAt)
	if remaining < 0 {
		return 0
	}
	return remaining
}
