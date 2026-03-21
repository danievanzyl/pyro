// vsock_other.go — Stub for non-Linux builds (macOS dev).

//go:build !linux

package sandbox

import (
	"fmt"
	"net"
)

func dialVsockPlatform(cid uint32, port uint32) (net.Conn, error) {
	return nil, fmt.Errorf("vsock not supported on this platform (requires Linux with KVM)")
}
