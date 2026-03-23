//go:build linux

package main

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
)

func initAsInit() {
	syscall.Mount("proc", "/proc", "proc", 0, "")
	syscall.Mount("sysfs", "/sys", "sysfs", 0, "")
	syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, "")
	os.MkdirAll("/dev/pts", 0755)
	syscall.Mount("devpts", "/dev/pts", "devpts", 0, "")
	syscall.Sethostname([]byte("pyro"))

	// Set PATH so commands work without full paths.
	os.Setenv("PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	os.Setenv("HOME", "/root")
	os.Setenv("TERM", "linux")

	exec.Command("ip", "link", "set", "lo", "up").Run()

	// Configure eth0 from kernel boot params (pyro.ip= pyro.gw=).
	ip, gw := bootParam("pyro.ip"), bootParam("pyro.gw")
	if ip != "" {
		exec.Command("ip", "addr", "add", ip+"/24", "dev", "eth0").Run()
		exec.Command("ip", "link", "set", "eth0", "up").Run()
		if gw != "" {
			exec.Command("ip", "route", "add", "default", "via", gw).Run()
		}
		// DNS
		os.MkdirAll("/etc", 0755)
		os.WriteFile("/etc/resolv.conf", []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644)
	}
}

func bootParam(key string) string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	for _, param := range strings.Split(string(data), " ") {
		if strings.HasPrefix(param, key+"=") {
			return strings.TrimPrefix(param, key+"=")
		}
	}
	return ""
}
