package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

var imagesDir = envOr("PYRO_IMAGES", "/opt/pyro/images")

// Built-in image definitions: name → OCI reference + post-install packages
type imageSpec struct {
	ociRef   string   // e.g. "docker://ubuntu:24.04"
	sizeMB   int      // rootfs ext4 size
	postPkgs []string // apt-get install after pull
	postCmds [][]string // arbitrary chroot commands after packages
}

var builtinImages = map[string]imageSpec{
	"ubuntu": {
		ociRef: "docker://ubuntu:24.04",
		sizeMB: 1024,
		postPkgs: []string{
			"python3", "python3-pip", "git", "curl", "wget", "jq",
			"build-essential", "ca-certificates", "openssh-client",
			"net-tools", "iputils-ping", "dnsutils", "vim-tiny",
			"less", "file", "unzip", "zip", "htop", "strace", "procps",
		},
	},
	"python": {
		ociRef: "docker://python:3.12-slim",
		sizeMB: 1024,
		postPkgs: []string{
			"git", "curl", "wget", "ca-certificates", "build-essential", "procps",
		},
		postCmds: [][]string{
			{"pip", "install", "--no-cache-dir", "numpy", "requests", "httpx", "pydantic", "rich"},
		},
	},
	"node": {
		ociRef: "docker://node:22-slim",
		sizeMB: 1024,
		postPkgs: []string{
			"git", "curl", "wget", "ca-certificates", "build-essential", "procps",
		},
		postCmds: [][]string{
			{"corepack", "enable"},
		},
	},
}

func buildImage() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, `usage: pyro build-image <name|all> [--from <oci-ref>] [--size <MB>]

Built-in images: minimal, ubuntu, python, node, all
Custom:          pyro build-image myimg --from docker://alpine:3.19`)
		os.Exit(1)
	}
	name := os.Args[2]

	requireRoot()
	requireTool("skopeo")
	requireTool("umoci")

	// Ensure kernel exists
	kernelPath := filepath.Join(imagesDir, "vmlinux")
	if _, err := os.Stat(kernelPath); os.IsNotExist(err) {
		fmt.Fprintln(os.Stderr, "error: kernel not built yet. Run: pyro build-kernel")
		os.Exit(1)
	}

	// Parse --from flag for custom images
	ociRef := ""
	sizeMB := 2048
	for i := 3; i < len(os.Args); i++ {
		switch os.Args[i] {
		case "--from":
			if i+1 < len(os.Args) {
				ociRef = os.Args[i+1]
				i++
			}
		case "--size":
			if i+1 < len(os.Args) {
				fmt.Sscanf(os.Args[i+1], "%d", &sizeMB)
				i++
			}
		}
	}

	if name == "all" {
		// Build minimal first (no skopeo needed), then OCI images
		fmt.Println("\n==> Building image: minimal")
		buildMinimal()
		for _, n := range []string{"ubuntu", "python", "node"} {
			fmt.Printf("\n==> Building image: %s\n", n)
			spec := builtinImages[n]
			buildFromOCI(n, spec.ociRef, spec.sizeMB, spec.postPkgs, spec.postCmds)
		}
		return
	}

	if name == "minimal" {
		buildMinimal()
		return
	}

	// Built-in image
	if spec, ok := builtinImages[name]; ok && ociRef == "" {
		buildFromOCI(name, spec.ociRef, spec.sizeMB, spec.postPkgs, spec.postCmds)
		return
	}

	// Custom image via --from
	if ociRef != "" {
		if !strings.HasPrefix(ociRef, "docker://") {
			ociRef = "docker://" + ociRef
		}
		buildFromOCI(name, ociRef, sizeMB, nil, nil)
		return
	}

	fmt.Fprintf(os.Stderr, "unknown image: %s (use --from <oci-ref> for custom)\n", name)
	os.Exit(1)
}

// buildFromOCI pulls an OCI image via skopeo, extracts with umoci, packs into ext4
func buildFromOCI(name, ociRef string, sizeMB int, postPkgs []string, postCmds [][]string) {
	imgDir := filepath.Join(imagesDir, name)
	rootfs := filepath.Join(imgDir, "rootfs.ext4")
	os.MkdirAll(imgDir, 0755)

	tmpDir, _ := os.MkdirTemp("", "fcbuild-*")
	defer os.RemoveAll(tmpDir)

	ociDir := filepath.Join(tmpDir, "oci")
	bundleDir := filepath.Join(tmpDir, "bundle")

	// 1. Pull image via skopeo
	fmt.Printf("==> Pulling %s\n", ociRef)
	run("skopeo", "copy", ociRef, "oci:"+ociDir+":latest")

	// 2. Unpack OCI image via umoci
	fmt.Println("==> Unpacking OCI layers")
	run("umoci", "unpack", "--image", ociDir+":latest", bundleDir)

	bundleRootfs := filepath.Join(bundleDir, "rootfs")

	// 3. Post-install: packages + commands (if rootfs has apt)
	if len(postPkgs) > 0 || len(postCmds) > 0 {
		// Bind-mount /dev, /proc, /sys for working chroot
		mountChroot(bundleRootfs)

		// DNS for chroot
		writeFile(filepath.Join(bundleRootfs, "etc/resolv.conf"), "nameserver 8.8.8.8\nnameserver 8.8.4.4\n")

		if len(postPkgs) > 0 && exists(filepath.Join(bundleRootfs, "usr/bin/apt-get")) {
			fmt.Println("==> Installing additional packages")
			chrootRun(bundleRootfs, "apt-get", "update", "-qq")
			args := append([]string{"install", "-y", "-qq", "--no-install-recommends"}, postPkgs...)
			chrootRun(bundleRootfs, "apt-get", args...)

			// Cleanup apt cache
			chrootRun(bundleRootfs, "apt-get", "clean")
			os.RemoveAll(filepath.Join(bundleRootfs, "var/lib/apt/lists"))
		}

		// Run post-install commands
		for _, cmd := range postCmds {
			fmt.Printf("==> Running: %s\n", strings.Join(cmd, " "))
			chrootRun(bundleRootfs, cmd[0], cmd[1:]...)
		}

		// MUST unmount before size calculation and copy
		umountChroot(bundleRootfs)
	}

	// 4. Inject pyro-agent
	installAgent(bundleRootfs)

	// 5. Firecracker-specific /etc files
	writeFile(filepath.Join(bundleRootfs, "etc/hostname"), "firecracker")
	writeFile(filepath.Join(bundleRootfs, "etc/hosts"), "127.0.0.1 localhost firecracker\n")
	writeFile(filepath.Join(bundleRootfs, "etc/resolv.conf"), "nameserver 8.8.8.8\nnameserver 8.8.4.4\n")

	// 6. Calculate actual rootfs size after all modifications
	actualSizeMB := dirSizeMB(bundleRootfs)
	// Add 30% headroom for runtime writes, min 64MB
	neededMB := int(float64(actualSizeMB)*1.3) + 64
	if neededMB > sizeMB {
		sizeMB = neededMB
	}
	fmt.Printf("==> Creating %dMB ext4 rootfs (content: %dMB)\n", sizeMB, actualSizeMB)
	createRootfs(rootfs, sizeMB)

	mnt := mustMount(rootfs)

	// 7. Copy bundle rootfs into ext4
	fmt.Println("==> Copying filesystem")
	run("cp", "-a", bundleRootfs+"/.", mnt+"/")

	// Unmount + report
	cleanup(mnt, rootfs)

	// Write image metadata
	meta := map[string]any{
		"name":   name,
		"source": ociRef,
		"size":   sizeMB,
	}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(filepath.Join(imgDir, "image.json"), metaJSON, 0644)

	fmt.Printf("==> %s image complete\n", name)
}

func buildKernel() {
	requireRoot()

	version := envOr("KERNEL_VERSION", "6.1.166")
	major := strings.Split(version, ".")[0]
	buildDir := envOr("KERNEL_BUILD_DIR", "/tmp/kernel-build")
	output := filepath.Join(imagesDir, "vmlinux")
	configURL := "https://raw.githubusercontent.com/firecracker-microvm/firecracker/main/resources/guest_configs/microvm-kernel-ci-x86_64-6.1.config"
	kernelURL := fmt.Sprintf("https://cdn.kernel.org/pub/linux/kernel/v%s.x/linux-%s.tar.xz", major, version)

	nproc := "1"
	if out, err := exec.Command("nproc").Output(); err == nil {
		nproc = strings.TrimSpace(string(out))
	}

	fmt.Printf("==> Building kernel %s (%s cores)\n", version, nproc)

	// Install build deps
	run("apt-get", "update", "-qq")
	run("apt-get", "install", "-y", "-qq",
		"build-essential", "bc", "bison", "flex", "libssl-dev",
		"libelf-dev", "libncurses-dev", "dwarves", "wget", "xz-utils")

	os.MkdirAll(imagesDir, 0755)
	os.MkdirAll(buildDir, 0755)

	// Download kernel source
	kernelTar := filepath.Join(buildDir, fmt.Sprintf("linux-%s.tar.xz", version))
	kernelSrc := filepath.Join(buildDir, fmt.Sprintf("linux-%s", version))
	if !exists(kernelSrc) {
		if !exists(kernelTar) {
			fmt.Printf("==> Downloading linux-%s\n", version)
			run("wget", "-q", "--show-progress", "-O", kernelTar, kernelURL)
		}
		fmt.Println("==> Extracting")
		run("tar", "-xf", kernelTar, "-C", buildDir)
	}

	// Download Firecracker config
	configFile := filepath.Join(buildDir, "microvm.config")
	if !exists(configFile) {
		fmt.Println("==> Downloading Firecracker microvm config")
		run("wget", "-q", "-O", configFile, configURL)
	}

	// Skip if vmlinux newer than config
	if exists(output) && isNewer(output, configFile) {
		fmt.Println("==> vmlinux already up to date")
		return
	}

	// Build
	cp(configFile, filepath.Join(kernelSrc, ".config"))
	runIn(kernelSrc, "make", "olddefconfig")
	fmt.Println("==> Compiling vmlinux")
	runIn(kernelSrc, "make", "-j"+nproc, "vmlinux")

	// Install
	cp(filepath.Join(kernelSrc, "vmlinux"), output)
	os.Chmod(output, 0644)
	fmt.Println("==> Kernel build complete")
}

// --- Minimal image (no container runtime needed) ---

func buildMinimal() {
	imgDir := filepath.Join(imagesDir, "minimal")
	rootfs := filepath.Join(imgDir, "rootfs.ext4")
	os.MkdirAll(imgDir, 0755)

	fmt.Println("==> Building minimal image (busybox + pyro-agent)")

	createRootfs(rootfs, 50)
	mnt := mustMount(rootfs)
	defer cleanup(mnt, rootfs)

	for _, d := range []string{"bin", "sbin", "usr/bin", "usr/sbin", "dev", "proc", "sys", "etc", "tmp", "root", "var/log"} {
		os.MkdirAll(filepath.Join(mnt, d), 0755)
	}

	bbPath := findBusybox()
	cp(bbPath, filepath.Join(mnt, "bin/busybox"))
	os.Chmod(filepath.Join(mnt, "bin/busybox"), 0755)

	links := []string{
		"cat", "cp", "date", "echo", "env", "grep", "head", "hostname",
		"id", "ip", "ls", "mkdir", "mount", "mv", "pwd", "rm", "sh",
		"sleep", "tail", "umount", "uname", "wc", "whoami", "vi", "wget",
		"tar", "gzip", "gunzip", "sed", "awk", "find", "xargs", "sort",
		"uniq", "cut", "tr", "tee", "dd", "df", "du", "free", "top",
		"kill", "ps", "chmod", "chown", "ln", "touch", "test", "true",
		"false", "yes", "nohup", "ping", "nc", "ifconfig", "route",
	}
	for _, l := range links {
		os.Symlink("/bin/busybox", filepath.Join(mnt, "bin", l))
	}

	installAgent(mnt)

	writeFile(filepath.Join(mnt, "etc/hostname"), "firecracker")
	writeFile(filepath.Join(mnt, "etc/hosts"), "127.0.0.1 localhost firecracker\n")
	writeFile(filepath.Join(mnt, "etc/resolv.conf"), "nameserver 8.8.8.8\nnameserver 8.8.4.4\n")
	writeFile(filepath.Join(mnt, "etc/passwd"), "root:x:0:0:root:/root:/bin/sh\n")
	writeFile(filepath.Join(mnt, "etc/group"), "root:x:0:\n")

	meta := map[string]any{"name": "minimal", "source": "busybox-static", "size": 50}
	metaJSON, _ := json.MarshalIndent(meta, "", "  ")
	os.WriteFile(filepath.Join(imgDir, "image.json"), metaJSON, 0644)

	fmt.Println("==> Minimal image complete")
}

// --- Helpers ---

func requireRoot() {
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "error: must run as root (need mount, skopeo, etc)")
		os.Exit(1)
	}
}

func requireTool(name string) {
	if _, err := exec.LookPath(name); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s not found. Install: apt-get install %s\n", name, name)
		os.Exit(1)
	}
}

func run(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
}

func runIn(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
}

func mountChroot(root string) {
	os.MkdirAll(filepath.Join(root, "dev"), 0755)
	os.MkdirAll(filepath.Join(root, "proc"), 0755)
	os.MkdirAll(filepath.Join(root, "sys"), 0755)
	exec.Command("mount", "--bind", "/dev", filepath.Join(root, "dev")).Run()
	exec.Command("mount", "--bind", "/proc", filepath.Join(root, "proc")).Run()
	exec.Command("mount", "--bind", "/sys", filepath.Join(root, "sys")).Run()
}

func umountChroot(root string) {
	exec.Command("umount", "-l", filepath.Join(root, "sys")).Run()
	exec.Command("umount", "-l", filepath.Join(root, "proc")).Run()
	exec.Command("umount", "-l", filepath.Join(root, "dev")).Run()
}

func chrootRun(root, name string, args ...string) {
	chrootArgs := append([]string{root, name}, args...)
	cmd := exec.Command("chroot", chrootArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(),
		"DEBIAN_FRONTEND=noninteractive",
		"LANG=C.UTF-8",
		"LC_ALL=C.UTF-8",
		"TZ=UTC",
	)
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: chroot %s %s: %v\n", name, strings.Join(args, " "), err)
		os.Exit(1)
	}
}

func createRootfs(path string, sizeMB int) {
	os.Remove(path)
	run("dd", "if=/dev/zero", "of="+path, "bs=1M", "count=0",
		"seek="+strconv.Itoa(sizeMB))
	run("mkfs.ext4", "-q", "-F", path)
}

func mustMount(rootfs string) string {
	mnt, err := os.MkdirTemp("", "fcimg-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: mktemp: %v\n", err)
		os.Exit(1)
	}
	run("mount", "-o", "loop", rootfs, mnt)
	return mnt
}

func cleanup(mnt, rootfs string) {
	exec.Command("umount", mnt).Run()
	os.Remove(mnt)
	if info, err := os.Stat(rootfs); err == nil {
		fmt.Printf("    rootfs: %s (%.0f MB)\n", rootfs, float64(info.Size())/1024/1024)
	}
}

func installAgent(mnt string) {
	candidates := []string{
		filepath.Join(filepath.Dir(os.Args[0]), "pyro-agent"),
		"/opt/pyro/bin/pyro-agent",
		"bin/pyro-agent",
	}
	for _, c := range candidates {
		if exists(c) {
			dst := filepath.Join(mnt, "usr/bin/pyro-agent")
			os.MkdirAll(filepath.Dir(dst), 0755)
			cp(c, dst)
			os.Chmod(dst, 0755)
			fmt.Printf("    pyro-agent installed from %s\n", c)
			return
		}
	}
	fmt.Fprintln(os.Stderr, "warning: pyro-agent binary not found, skipping")
}

func findBusybox() string {
	for _, c := range []string{"/bin/busybox-static", "/usr/bin/busybox-static", "/bin/busybox"} {
		if exists(c) {
			return c
		}
	}
	run("apt-get", "update", "-qq")
	run("apt-get", "install", "-y", "-qq", "busybox-static")
	if exists("/bin/busybox-static") {
		return "/bin/busybox-static"
	}
	fmt.Fprintln(os.Stderr, "error: busybox-static not found")
	os.Exit(1)
	return ""
}

func dirSizeMB(path string) int {
	out, err := exec.Command("du", "-sm", path).Output()
	if err != nil {
		return 512 // default fallback
	}
	parts := strings.Fields(string(out))
	if len(parts) > 0 {
		n, _ := strconv.Atoi(parts[0])
		return n
	}
	return 512
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func isNewer(a, b string) bool {
	ai, _ := os.Stat(a)
	bi, _ := os.Stat(b)
	if ai == nil || bi == nil {
		return false
	}
	return ai.ModTime().After(bi.ModTime())
}

func cp(src, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: read %s: %v\n", src, err)
		os.Exit(1)
	}
	if err := os.WriteFile(dst, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: write %s: %v\n", dst, err)
		os.Exit(1)
	}
}

func writeFile(path, content string) {
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, []byte(content), 0644)
}
