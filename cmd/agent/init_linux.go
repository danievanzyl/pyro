//go:build linux

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func initAsInit() {
	syscall.Mount("proc", "/proc", "proc", 0, "")
	syscall.Mount("sysfs", "/sys", "sysfs", 0, "")
	syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, "")
	os.MkdirAll("/dev/pts", 0755)
	syscall.Mount("devpts", "/dev/pts", "devpts", 0, "")
	syscall.Sethostname([]byte("firecrackerlacker"))

	// Set PATH so commands work without full paths.
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	os.Setenv("HOME", "/root")
	os.Setenv("TERM", "linux")

	exec.Command("/bin/busybox", "ip", "link", "set", "lo", "up").Run()
}
