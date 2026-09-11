# The runner image starts its own services; pyro-agent only provides the mounts

The runner guest image carries `dockerd`, and starts it from a wrapper script
baked into the image at `/usr/local/bin/pyro-runner-boot`, which is what the
runner controller passes as `workload.command`. pyro-agent gains no knowledge of
Docker; it gains only the three mounts any service manager would have provided —
`cgroup2` at `/sys/fs/cgroup`, and tmpfs at `/dev/shm` and `/run`.

## Context

No init system runs in a pyro sandbox. The kernel boot arg is
`init=/usr/bin/pyro-agent`, pyro-agent detects PID 1, does minimal setup
(`cmd/agent/init_linux.go:14-45`) and then blocks in a vsock accept loop. A
systemd unit baked into an image never fires, and the image's `CMD`/`ENTRYPOINT`
is never executed. #15 settled the job primitive on top of that: one
`workload.command`, started by pyro-agent as its child, one job per sandbox.

Every self-hosted runner this host will serve builds container images —
`ferrix`, `mcp1` and `yagh-runner` all use `docker/build-push-action`, two of
them with `docker/setup-buildx-action` on its default `docker-container` driver,
which drives buildkit through the Docker daemon. No workflow in the account uses
`container:` or `services:`. So the requirement is Docker *builds*, not container
jobs, and it is not optional.

The guest kernel turned out not to be the obstacle. The Firecracker CI v1.15
kernel config has every option Docker requires compiled in as `=y` rather than
`=m` — namespaces, all cgroup controllers, `OVERLAY_FS`, `VETH`, `BRIDGE`,
`BRIDGE_NETFILTER`, `NF_NAT`, `IP_NF_TARGET_MASQUERADE`, `SECCOMP`,
`POSIX_MQUEUE`. Built-in matters here specifically because there is no `modprobe`
and nothing to run it.

## Decision

Two things need to happen before `run.sh` runs, and they belong on opposite sides
of the line.

**Starting `dockerd` belongs to the image.** #15's primitive runs exactly one
command, and there is no init to start a service, so something must. A wrapper
script inside the image keeps the runner controller ignorant of Docker: it hands
over the JIT config and nothing else, and a future image that needs a different
service changes only itself. The wrapper starts `dockerd`, waits for the daemon
with a bounded timeout, then `exec`s `run.sh --jitconfig "$1"`.

**Providing the mounts belongs to pyro-agent.** `containerd` and `runc` cannot
start without the cgroup2 hierarchy, and no wrapper script should be mounting
kernel filesystems in a VM where it is not PID 1. These are the mounts a service
manager provides on any Linux system; pyro-agent is the only thing in the guest
positioned to provide them. They are generic — nothing about them is
Docker-specific or runner-specific.

The wrapper waits for the daemon rather than backgrounding it and racing. It
costs roughly a second against a cold boot already measured in seconds, and buys
a deterministic start instead of an intermittent "Cannot connect to the Docker
daemon" surfacing in a user's job log.

## Considered options

**A daemonless image builder** — buildkit with buildx's `remote` driver, or
buildah, or kaniko — was rejected. It removes the daemon but requires editing
workflows in three repositories to avoid it, and leaves every bare `docker`
command in a job step still broken.

**Adding an init system to the image** was rejected. It contradicts the boot
contract, which is settled and which `CONTEXT.md` records: the image's `CMD` never
runs, and pyro-agent is PID 1. An init would have to replace pyro-agent, which
would take the vsock protocol with it.

**Two images, `runner` and `runner-dind`,** was rejected. Everything that will run
here builds images, so a slim variant would serve only pyro's own CI, in exchange
for a second Dockerfile to hold above GitHub's moving runner-version floor. The
saving is ~420 MB of a sparse file on a reflinking filesystem, which is nothing.

**Teaching pyro-agent to start Docker** was rejected for the reason the boundary
exists: `internal/sandbox` and the guest agent stay GitHub-unaware, exactly as
ADR 0003 keeps `internal/sandbox` from importing `internal/runner`.

## Consequences

`/var/lib/docker` must be bind-mounted onto the scratch disk when one is
attached. `setupScratch` overlays `/var`, and Docker's `overlay2` storage driver
refuses an overlayfs backing store — it falls back to `vfs`, silently, which
copies every layer in full. The wrapper does the bind, because it is the part
that knows about Docker.

pyro-agent now mounts cgroup2 and two tmpfs filesystems in every sandbox, not
just runner sandboxes. That is intentional and harmless: they are what a Linux
userspace expects, and their absence is the surprising state, not their presence.

The JIT config now travels one hop further than #15 specified — the controller
passes it to `pyro-runner-boot`, which passes it to `run.sh`. The transport is
unchanged: argv over the vsock `workload_start` message.
