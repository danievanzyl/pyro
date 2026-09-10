# Pyro

A Firecracker microVM sandbox platform for AI agents. Pyro runs on a single Linux
host with native `/dev/kvm`, boots one microVM per sandbox, and exposes them over
an HTTP API.

## Language

**Sandbox**:
One Firecracker microVM with a lifetime, owned by the API key that created it.
_Avoid_: VM, instance, container

**Base image**:
A named, rootfs-only ext4 filesystem a sandbox boots from. A base image never
carries a kernel.
_Avoid_: Image (unqualified, when a Docker image is meant), template

**Guest kernel**:
The single `vmlinux` a host boots every sandbox with, named by the server's
`--kernel` flag. It belongs to the host, not to a base image, and is not
selectable per sandbox. See `docs/adr/0001-guest-kernel-is-a-host-resource.md`.
_Avoid_: Kernel version, kernel image (both imply a selectable set)

**Host readiness**:
Whether a host can actually boot a sandbox — `/dev/kvm`, the `firecracker`
binary, the bridge with IP forwarding, a guest kernel, at least one base image,
and root. Distinct from `/api/health`, which reports only that the server process
is answering.
_Avoid_: Health, preflight (reserve `preflight` for the startup check itself)

**pyro-agent**:
The in-VM binary that runs as PID 1 and serves the vsock protocol. A sandbox's
base image never runs its own `CMD` or `ENTRYPOINT`.
_Avoid_: Init, guest agent
