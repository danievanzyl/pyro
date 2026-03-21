//go:build linux

package main

import (
	"net"

	"github.com/mdlayher/vsock"
)

func listenVsockPlatform(port uint32) (net.Listener, error) {
	return vsock.Listen(port, nil)
}
