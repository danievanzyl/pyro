// Package sandbox — network.go implements per-sandbox network isolation.
//
// Default policy: DENY ALL. Each sandbox gets a tap device on a bridge,
// but iptables rules block all traffic by default.
//
// Available policies:
//   - "none"     — no network access (default)
//   - "outbound" — outbound internet via NAT, no inbound, no inter-sandbox
//   - "full"     — full network access (dangerous, for trusted workloads)
//
// Implementation:
//
//	Host iptables chain: FCLK-{sandbox-id}
//	  ├── ACCEPT established,related (stateful return traffic)
//	  ├── DROP inter-sandbox (block tap-to-tap)
//	  └── policy-specific rules
//
//	NAT (outbound policy):
//	  POSTROUTING -s {vm-ip}/32 -o {wan-iface} -j MASQUERADE
package sandbox

import (
	"fmt"
	"os/exec"
)

// NetworkPolicy defines what network access a sandbox has.
type NetworkPolicy string

const (
	NetworkNone     NetworkPolicy = "none"
	NetworkOutbound NetworkPolicy = "outbound"
	NetworkFull     NetworkPolicy = "full"
)

// NetworkConfig holds network isolation settings.
type NetworkConfig struct {
	// WANInterface is the host's outbound network interface (e.g., "eth0").
	WANInterface string

	// SubnetCIDR is the subnet for sandbox VMs (e.g., "172.16.0.0/24").
	SubnetCIDR string
}

// ApplyNetworkPolicy configures iptables rules for a sandbox.
func ApplyNetworkPolicy(tapDevice, vmIP string, policy NetworkPolicy, cfg NetworkConfig) error {
	chain := "FCLK-" + tapDevice

	// Create per-sandbox chain.
	iptables("-N", chain)

	// Allow established/related (stateful return traffic).
	if err := iptables("-A", chain,
		"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED",
		"-j", "ACCEPT"); err != nil {
		return fmt.Errorf("allow established: %w", err)
	}

	// Block inter-sandbox traffic (drop packets to other VMs on the bridge).
	if cfg.SubnetCIDR != "" {
		if err := iptables("-A", chain,
			"-d", cfg.SubnetCIDR,
			"-j", "DROP"); err != nil {
			return fmt.Errorf("block inter-sandbox: %w", err)
		}
	}

	switch policy {
	case NetworkNone:
		// Drop everything else.
		if err := iptables("-A", chain, "-j", "DROP"); err != nil {
			return fmt.Errorf("drop all: %w", err)
		}

	case NetworkOutbound:
		// Allow outbound to internet (not to bridge subnet).
		if err := iptables("-A", chain, "-j", "ACCEPT"); err != nil {
			return fmt.Errorf("allow outbound: %w", err)
		}
		// NAT for outbound traffic.
		if cfg.WANInterface != "" && vmIP != "" {
			if err := iptables("-t", "nat", "-A", "POSTROUTING",
				"-s", vmIP+"/32",
				"-o", cfg.WANInterface,
				"-j", "MASQUERADE"); err != nil {
				return fmt.Errorf("nat masquerade: %w", err)
			}
		}

	case NetworkFull:
		// Allow everything.
		// Remove the inter-sandbox drop rule for full policy.
		iptables("-D", chain, "-d", cfg.SubnetCIDR, "-j", "DROP")
		if err := iptables("-A", chain, "-j", "ACCEPT"); err != nil {
			return fmt.Errorf("allow all: %w", err)
		}
	}

	// Wire the per-sandbox chain into FORWARD.
	if err := iptables("-A", "FORWARD",
		"-i", tapDevice,
		"-j", chain); err != nil {
		return fmt.Errorf("forward to chain: %w", err)
	}

	// Also apply to traffic coming TO the sandbox.
	if err := iptables("-A", "FORWARD",
		"-o", tapDevice,
		"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED",
		"-j", "ACCEPT"); err != nil {
		return fmt.Errorf("forward return traffic: %w", err)
	}

	return nil
}

// RemoveNetworkPolicy cleans up iptables rules for a sandbox.
func RemoveNetworkPolicy(tapDevice, vmIP string, cfg NetworkConfig) {
	chain := "FCLK-" + tapDevice

	// Remove from FORWARD chain.
	iptables("-D", "FORWARD", "-i", tapDevice, "-j", chain)
	iptables("-D", "FORWARD", "-o", tapDevice,
		"-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT")

	// Remove NAT rule.
	if cfg.WANInterface != "" && vmIP != "" {
		iptables("-t", "nat", "-D", "POSTROUTING",
			"-s", vmIP+"/32", "-o", cfg.WANInterface, "-j", "MASQUERADE")
	}

	// Flush and delete the chain.
	iptables("-F", chain)
	iptables("-X", chain)
}

func iptables(args ...string) error {
	cmd := exec.Command("iptables", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables %v: %s: %w", args, out, err)
	}
	return nil
}
