#!/bin/bash
# Setup network bridge for Firecracker VMs.
# Run once on the KVM host.
set -e

BRIDGE=fcbr0
SUBNET=172.16.0.1/24
WAN_IFACE=$(ip route | grep default | awk '{print $5}' | head -1)

ip link show $BRIDGE >/dev/null 2>&1 && echo "bridge $BRIDGE exists" || {
    ip link add $BRIDGE type bridge
    ip addr add $SUBNET dev $BRIDGE
    ip link set $BRIDGE up
    echo "bridge $BRIDGE created"
}

echo 1 > /proc/sys/net/ipv4/ip_forward

iptables -t nat -C POSTROUTING -s 172.16.0.0/24 -o $WAN_IFACE -j MASQUERADE 2>/dev/null || {
    iptables -t nat -A POSTROUTING -s 172.16.0.0/24 -o $WAN_IFACE -j MASQUERADE
    echo "NAT rule added via $WAN_IFACE"
}

echo "done"
