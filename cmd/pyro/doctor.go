package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func doctor() {
	fmt.Println("Pyro Doctor — checking system readiness")
	ok := true

	// 1. KVM
	fmt.Print("  KVM (/dev/kvm)          ")
	if _, err := os.Stat("/dev/kvm"); err == nil {
		pass()
	} else {
		fail("not found — Firecracker requires KVM")
		ok = false
	}

	// 2. Firecracker binary
	fmt.Print("  Firecracker binary      ")
	if path, err := exec.LookPath("firecracker"); err == nil {
		out, _ := exec.Command(path, "--version").Output()
		passf("%s", trimNL(out))
	} else {
		fail("not found — install from github.com/firecracker-microvm/firecracker")
		ok = false
	}

	// 3. Network bridge
	fmt.Print("  Network bridge (fcbr0)  ")
	if err := exec.Command("ip", "link", "show", "fcbr0").Run(); err == nil {
		pass()
	} else {
		warn("not found — run: pyro setup")
	}

	// 4. IP forwarding
	fmt.Print("  IP forwarding           ")
	if data, err := os.ReadFile("/proc/sys/net/ipv4/ip_forward"); err == nil && len(data) > 0 && data[0] == '1' {
		pass()
	} else {
		warn("disabled — run: echo 1 > /proc/sys/net/ipv4/ip_forward")
	}

	// 5. Images directory
	baseDir := envOr("PYRO_IMAGES", "/opt/pyro/images")
	fmt.Print("  Images directory        ")
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		fail(fmt.Sprintf("not found: %s", baseDir))
		ok = false
	} else {
		count := 0
		for _, e := range entries {
			if e.IsDir() {
				rootfs := filepath.Join(baseDir, e.Name(), "rootfs.ext4")
				if _, err := os.Stat(rootfs); err == nil {
					count++
				}
			}
		}
		if count > 0 {
			passf("%d images in %s", count, baseDir)
		} else {
			fail(fmt.Sprintf("no images found in %s — run: pyro build-image all", baseDir))
			ok = false
		}
	}

	// 6. Kernel
	fmt.Print("  Kernel (vmlinux)        ")
	kernelPath := filepath.Join(baseDir, "vmlinux")
	if info, err := os.Stat(kernelPath); err == nil {
		passf("%.0f MB", float64(info.Size())/1024/1024)
	} else {
		fail("not found — run: pyro build-kernel")
		ok = false
	}

	// 7. Server health
	fmt.Print("  Pyro server             ")
	serverURL := envOr("PYRO_BASE_URL", "http://localhost:8080")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(serverURL + "/api/health")
	if err != nil {
		warn(fmt.Sprintf("not reachable at %s", serverURL))
	} else {
		resp.Body.Close()
		if resp.StatusCode == 200 {
			passf("healthy at %s", serverURL)
		} else {
			warn(fmt.Sprintf("unhealthy (status %d)", resp.StatusCode))
		}
	}

	// 8. OCI tools
	for _, tool := range []string{"skopeo", "umoci"} {
		fmt.Printf("  %-24s", tool)
		if _, err := exec.LookPath(tool); err == nil {
			pass()
		} else {
			warn(fmt.Sprintf("not found — needed for: pyro build-image"))
		}
	}

	fmt.Println()
	if ok {
		fmt.Println("All critical checks passed.")
	} else {
		fmt.Println("Some checks failed — see above.")
		os.Exit(1)
	}
}

func pass()                    { fmt.Println("\033[32m✓\033[0m") }
func passf(f string, a ...any) { fmt.Printf("\033[32m✓\033[0m %s\n", fmt.Sprintf(f, a...)) }
func fail(msg string)          { fmt.Printf("\033[31m✗\033[0m %s\n", msg) }
func warn(msg string)          { fmt.Printf("\033[33m!\033[0m %s\n", msg) }

func trimNL(b []byte) string {
	return strings.TrimSuffix(string(b), "\n")
}
