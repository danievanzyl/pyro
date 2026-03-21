// vsock_linux.go — Linux-only vsock dialer using AF_VSOCK.

//go:build linux

package sandbox

import (
	"net"

	"github.com/mdlayher/vsock"
)

func dialVsockPlatform(cid uint32, port uint32) (net.Conn, error) {
	return vsock.Dial(cid, port, nil)
}
