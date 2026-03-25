//go:build linux

package main

import (
	"encoding/binary"
	"net"
	"os"
	"strings"
	"syscall"
	"unsafe"
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

	setLinkUp("lo")

	// Configure eth0 from kernel boot params (pyro.ip= pyro.gw=).
	ipStr, gw := bootParam("pyro.ip"), bootParam("pyro.gw")
	if ipStr != "" {
		addAddr("eth0", ipStr, 24)
		setLinkUp("eth0")
		if gw != "" {
			addDefaultRoute(gw)
		}
		os.MkdirAll("/etc", 0755)
		_ = os.WriteFile("/etc/resolv.conf", []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644)
	}

	// Mount scratch disk (/dev/vdb) if present, set up overlayfs.
	if bootParam("pyro.scratch") == "1" {
		setupScratch()
	}
}

// setupScratch mounts /dev/vdb at /scratch and layers overlayfs on /usr, /var, /etc
// so that apt/pip/npm installs write to scratch while rootfs stays pristine.
//
//	/dev/vdb (scratch)
//	  ├── .overlay/{usr,var,etc}/{upper,work}  — overlayfs upper dirs
//	  ├── tmp/                                  — bind-mounted to /tmp
//	  └── home/                                 — bind-mounted to /root
//
//	overlayfs mounts:
//	  /usr = overlay(lower=/usr, upper=/scratch/.overlay/usr/upper)
//	  /var = overlay(lower=/var, upper=/scratch/.overlay/var/upper)
//	  /etc = overlay(lower=/etc, upper=/scratch/.overlay/etc/upper)
func setupScratch() {
	os.MkdirAll("/scratch", 0755)

	// Mount /dev/vdb at /scratch.
	if err := syscall.Mount("/dev/vdb", "/scratch", "ext4", 0, ""); err != nil {
		return // no scratch disk attached
	}

	// Create overlay directories.
	for _, dir := range []string{"usr", "var", "etc"} {
		os.MkdirAll("/scratch/.overlay/"+dir+"/upper", 0755)
		os.MkdirAll("/scratch/.overlay/"+dir+"/work", 0755)
	}
	os.MkdirAll("/scratch/tmp", 0777)
	os.Chmod("/scratch/tmp", 0777|os.ModeSticky) // 1777 sticky bit
	os.MkdirAll("/scratch/home", 0750)

	// Mount overlayfs on /usr, /var, /etc.
	for _, dir := range []string{"usr", "var", "etc"} {
		opts := "lowerdir=/" + dir + ",upperdir=/scratch/.overlay/" + dir + "/upper,workdir=/scratch/.overlay/" + dir + "/work"
		syscall.Mount("overlay", "/"+dir, "overlay", 0, opts)
	}

	// Bind /tmp and /root to scratch.
	syscall.Mount("/scratch/tmp", "/tmp", "", syscall.MS_BIND, "")
	syscall.Mount("/scratch/home", "/root", "", syscall.MS_BIND, "")

	// Re-create resolv.conf in overlayed /etc (overlay may hide it).
	_ = os.WriteFile("/etc/resolv.conf", []byte("nameserver 1.1.1.1\nnameserver 8.8.8.8\n"), 0644)
}

func bootParam(key string) string {
	data, err := os.ReadFile("/proc/cmdline")
	if err != nil {
		return ""
	}
	for _, param := range strings.Split(string(data), " ") {
		if val, ok := strings.CutPrefix(param, key+"="); ok {
			return val
		}
	}
	return ""
}

// --- Netlink helpers (no external dependencies) ---

// setLinkUp brings a network interface up using ioctl SIOCSIFFLAGS.
func setLinkUp(name string) {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, 0)
	if err != nil {
		return
	}
	defer syscall.Close(fd)

	var ifr [40]byte // struct ifreq
	copy(ifr[:syscall.IFNAMSIZ], name)

	// Get current flags.
	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCGIFFLAGS, uintptr(unsafe.Pointer(&ifr[0])))

	// Set IFF_UP.
	flags := binary.LittleEndian.Uint16(ifr[16:18])
	flags |= syscall.IFF_UP | syscall.IFF_RUNNING
	binary.LittleEndian.PutUint16(ifr[16:18], flags)

	syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&ifr[0])))
}

// addAddr adds an IPv4 address to an interface using netlink RTM_NEWADDR.
func addAddr(name, ipStr string, prefixLen int) {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return
	}
	ip4 := ip.To4()
	if ip4 == nil {
		return
	}

	ifindex := ifIndex(name)
	if ifindex == 0 {
		return
	}

	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE)
	if err != nil {
		return
	}
	defer syscall.Close(fd)

	// RTM_NEWADDR message.
	msg := make([]byte, 0, 128)

	// nlmsghdr (16 bytes)
	msg = appendU32(msg, 0) // length (fill later)
	msg = appendU16(msg, syscall.RTM_NEWADDR)
	msg = appendU16(msg, syscall.NLM_F_REQUEST|syscall.NLM_F_ACK|syscall.NLM_F_CREATE|syscall.NLM_F_EXCL)
	msg = appendU32(msg, 1) // seq
	msg = appendU32(msg, 0) // pid

	// ifaddrmsg (8 bytes)
	msg = append(msg, syscall.AF_INET)     // family
	msg = append(msg, byte(prefixLen))     // prefixlen
	msg = append(msg, 0)                   // flags
	msg = append(msg, 0)                   // scope (RT_SCOPE_UNIVERSE)
	msg = appendU32(msg, uint32(ifindex))  // index

	// IFA_LOCAL attr
	msg = appendAttr(msg, 2 /* IFA_LOCAL */, ip4)
	// IFA_ADDRESS attr
	msg = appendAttr(msg, 1 /* IFA_ADDRESS */, ip4)

	// Fill length.
	binary.LittleEndian.PutUint32(msg[0:4], uint32(len(msg)))

	syscall.Write(fd, msg)
	// Read ACK (ignore errors — best effort).
	ack := make([]byte, 128)
	syscall.Read(fd, ack)
}

// addDefaultRoute adds a default route via gateway using netlink RTM_NEWROUTE.
func addDefaultRoute(gwStr string) {
	gw := net.ParseIP(gwStr)
	if gw == nil {
		return
	}
	gw4 := gw.To4()
	if gw4 == nil {
		return
	}

	fd, err := syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_ROUTE)
	if err != nil {
		return
	}
	defer syscall.Close(fd)

	msg := make([]byte, 0, 128)

	// nlmsghdr
	msg = appendU32(msg, 0) // length
	msg = appendU16(msg, syscall.RTM_NEWROUTE)
	msg = appendU16(msg, syscall.NLM_F_REQUEST|syscall.NLM_F_ACK|syscall.NLM_F_CREATE|syscall.NLM_F_EXCL)
	msg = appendU32(msg, 2) // seq
	msg = appendU32(msg, 0) // pid

	// rtmsg (12 bytes)
	msg = append(msg, syscall.AF_INET) // family
	msg = append(msg, 0)               // dst_len (0 = default route)
	msg = append(msg, 0)               // src_len
	msg = append(msg, 0)               // tos
	msg = append(msg, syscall.RT_TABLE_MAIN)
	msg = append(msg, syscall.RTPROT_BOOT)
	msg = append(msg, syscall.RT_SCOPE_UNIVERSE)
	msg = append(msg, syscall.RTN_UNICAST)
	msg = appendU32(msg, 0) // flags

	// RTA_GATEWAY attr
	msg = appendAttr(msg, syscall.RTA_GATEWAY, gw4)

	binary.LittleEndian.PutUint32(msg[0:4], uint32(len(msg)))

	syscall.Write(fd, msg)
	ack := make([]byte, 128)
	syscall.Read(fd, ack)
}

// ifIndex returns the index of a network interface.
func ifIndex(name string) int {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0
	}
	return iface.Index
}

func appendU16(b []byte, v uint16) []byte {
	return append(b, byte(v), byte(v>>8))
}

func appendU32(b []byte, v uint32) []byte {
	return append(b, byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
}

// appendAttr appends a netlink attribute (NLA header + data, padded to 4-byte alignment).
func appendAttr(b []byte, typ uint16, data []byte) []byte {
	attrLen := 4 + len(data) // nla_len (2) + nla_type (2) + data
	b = appendU16(b, uint16(attrLen))
	b = appendU16(b, typ)
	b = append(b, data...)
	// Pad to 4-byte alignment.
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}
