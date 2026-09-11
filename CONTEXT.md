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

**Cold boot**:
How every sandbox starts: a fresh microVM boot, on demand, with no pre-warmed
state carried over from any earlier sandbox. It is the only way a sandbox starts.
See `docs/adr/0005-pyro-cold-boots-every-sandbox.md`.
_Avoid_: Warm pool, snapshot pool, snapshot restore, resume (pyro takes no
snapshots and restores none; the vocabulary describes a capability it does not
have)

**Host readiness**:
Whether a host can actually boot a sandbox — `/dev/kvm`, the `firecracker`
binary, the bridge with IP forwarding, a guest kernel, at least one base image,
and root. Distinct from `/api/health`, which reports only that the server process
is answering.
_Avoid_: Health, preflight (reserve `preflight` for the startup check itself)

**Blocking check**:
A host-readiness check whose failure means a sandbox cannot be created. The
complement is a *degraded* check — the host still boots sandboxes without it.
Only blocking checks gate readiness.
_Avoid_: Critical, fatal (no host-readiness failure stops the server from running —
see `docs/adr/0002-host-readiness-is-reported-never-fatal.md`)

**pyro-agent**:
The in-VM binary that runs as PID 1 and serves the vsock protocol. A sandbox's
base image never runs its own `CMD` or `ENTRYPOINT`.
_Avoid_: Init, guest agent

**Runner controller**:
The part of pyro that watches a GitHub repository or organisation for queued
Actions jobs and creates one sandbox per job. It belongs to pyro and ships with
it, but a pyro with no GitHub configuration does not have one. See
`docs/adr/0003-the-runner-controller-lives-in-pyro-behind-a-one-way-boundary.md`.
_Avoid_: Runner (unqualified), listener, provider, GARM provider

**Runner**:
The GitHub Actions runner agent running inside a sandbox, which claims a single
job and exits. It is GitHub's software, not pyro's, and pyro never uses the word
for anything else.
_Avoid_: Agent (that is `pyro-agent`), worker, executor

**Boot wrapper**:
A script carried inside a base image that a sandbox's workload command points at.
It starts whatever services that image's workload needs, then hands control to
the workload. pyro never supplies one — an image that needs services carries its
own. See `docs/adr/0004-the-runner-image-starts-its-own-services.md`.
_Avoid_: Entrypoint, init script, startup script (all imply something pyro or the
image runtime invokes on its own)
