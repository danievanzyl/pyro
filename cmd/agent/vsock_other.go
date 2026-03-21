//go:build !linux

package main

import (
	"fmt"
	"net"
)

func listenVsockPlatform(port uint32) (net.Listener, error) {
	return nil, fmt.Errorf("vsock not supported on this platform (agent must run inside a Linux VM)")
}
