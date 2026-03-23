package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"github.com/danievanzyl/pyro/internal/store"
	"github.com/google/uuid"
)

func setup() {
	requireRoot()
	if runtime.GOARCH != "amd64" {
		fmt.Fprintf(os.Stderr, "error: only x86_64 supported (got %s)\n", runtime.GOARCH)
		os.Exit(1)
	}

	baseDir := envOr("PYRO_BASE", "/opt/pyro")
	fmt.Println("==> pyro setup")
	fmt.Printf("    base: %s\n", baseDir)

	// 1. Create directory structure
	fmt.Println("\n==> Creating directories")
	for _, d := range []string{"bin", "images", "state", "db"} {
		os.MkdirAll(filepath.Join(baseDir, d), 0755)
	}
	fmt.Println("    done")

	// 2. Check/install Firecracker
	fmt.Println("\n==> Checking Firecracker")
	if path, err := exec.LookPath("firecracker"); err == nil {
		out, _ := exec.Command(path, "--version").Output()
		fmt.Printf("    found: %s", out)
	} else {
		fmt.Println("    not found — installing from GitHub releases")
		installFirecracker(baseDir)
	}

	// 3. Network bridge
	fmt.Println("\n==> Setting up network bridge")
	setupBridge()

	// 4. Build kernel
	fmt.Println("\n==> Building kernel")
	buildKernel()

	// 4.5. Check skopeo/umoci
	fmt.Println("\n==> Checking OCI tools")
	for _, tool := range []string{"skopeo", "umoci"} {
		if _, err := exec.LookPath(tool); err != nil {
			fmt.Printf("    installing %s\n", tool)
			run("apt-get", "install", "-y", "-qq", tool)
		} else {
			fmt.Printf("    %s: ok\n", tool)
		}
	}

	// 5. Build all images
	fmt.Println("\n==> Building all images")
	buildMinimal()
	for _, name := range []string{"ubuntu", "python", "node"} {
		fmt.Printf("\n--- %s ---\n", name)
		spec := builtinImages[name]
		buildFromOCI(name, spec.ociRef, spec.sizeMB, spec.postPkgs, spec.postCmds)
	}

	// 6. Migrate "default" image → symlink to minimal
	defaultDir := filepath.Join(imagesDir, "default")
	minimalDir := filepath.Join(imagesDir, "minimal")
	if _, err := os.Lstat(defaultDir); err == nil {
		// default exists — check if it's already a symlink
		if target, err := os.Readlink(defaultDir); err != nil || target != minimalDir {
			// backup existing default
			os.Rename(defaultDir, defaultDir+".bak")
			os.Symlink(minimalDir, defaultDir)
			fmt.Println("\n==> default → minimal (old default backed up)")
		}
	} else {
		os.Symlink(minimalDir, defaultDir)
		fmt.Println("\n==> default → minimal")
	}

	// 7. Install systemd service
	fmt.Println("\n==> Installing systemd service")
	installService(baseDir)

	// 8. Create first API key
	fmt.Println("\n==> Creating API key")
	db := filepath.Join(baseDir, "db/pyro.db")
	st, err := store.New(db)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: open db: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	key := generateKey()
	ak := &store.APIKey{
		ID:        uuid.New().String(),
		Key:       key,
		Name:      "default",
		CreatedAt: time.Now().UTC(),
	}
	if err := st.CreateAPIKey(context.Background(), ak); err != nil {
		fmt.Fprintf(os.Stderr, "error: create key: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n==> Setup complete!\n\n")
	fmt.Printf("API Key: %s\n", ak.Key)
	fmt.Printf("Store this securely — it cannot be retrieved later.\n\n")
	fmt.Printf("Start the server:\n")
	fmt.Printf("  systemctl start pyro\n\n")
	fmt.Printf("Or manually:\n")
	fmt.Printf("  %s/bin/pyro-server \\\n", baseDir)
	fmt.Printf("    --images-dir %s/images \\\n", baseDir)
	fmt.Printf("    --state-dir %s/state \\\n", baseDir)
	fmt.Printf("    --db %s/db/pyro.db\n", baseDir)
}

func installFirecracker(baseDir string) {
	// Download latest release
	version := "v1.15.0"
	url := fmt.Sprintf("https://github.com/firecracker-microvm/firecracker/releases/download/%s/firecracker-%s-x86_64.tgz", version, version)
	tarPath := "/tmp/firecracker.tgz"

	run("wget", "-q", "--show-progress", "-O", tarPath, url)
	run("tar", "-xzf", tarPath, "-C", "/tmp")
	run("mv", fmt.Sprintf("/tmp/release-%s-x86_64/firecracker-%s-x86_64", version, version), "/usr/local/bin/firecracker")
	os.Chmod("/usr/local/bin/firecracker", 0755)
	os.Remove(tarPath)
	fmt.Println("    installed firecracker", version)
}

func setupBridge() {
	// Check if bridge exists
	if err := exec.Command("ip", "link", "show", "fcbr0").Run(); err == nil {
		fmt.Println("    bridge fcbr0 already exists")
		return
	}

	run("ip", "link", "add", "fcbr0", "type", "bridge")
	run("ip", "addr", "add", "172.16.0.1/24", "dev", "fcbr0")
	run("ip", "link", "set", "fcbr0", "up")

	// Enable IP forwarding
	os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1"), 0644)

	// NAT
	wanIface := detectWanIface()
	if wanIface != "" {
		exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
			"-s", "172.16.0.0/24", "-o", wanIface, "-j", "MASQUERADE").Run()
		fmt.Printf("    NAT via %s\n", wanIface)
	}
	fmt.Println("    bridge fcbr0 created")
}

func detectWanIface() string {
	out, err := exec.Command("bash", "-c", "ip route | grep default | awk '{print $5}' | head -1").Output()
	if err != nil {
		return ""
	}
	return string(out[:len(out)-1]) // trim newline
}

func installService(baseDir string) {
	service := fmt.Sprintf(`[Unit]
Description=Firecrackerlacker API Server
After=network.target

[Service]
Type=simple
ExecStart=%s/bin/pyro-server \
  --images-dir %s/images \
  --state-dir %s/state \
  --db %s/db/pyro.db \
  --prometheus \
  --max-per-key 10 \
  --rate-limit 30
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`, baseDir, baseDir, baseDir, baseDir)

	os.WriteFile("/etc/systemd/system/pyro.service", []byte(service), 0644)
	run("systemctl", "daemon-reload")
	run("systemctl", "enable", "pyro")
	fmt.Println("    systemd service installed + enabled")
}
