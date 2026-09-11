# Host readiness is reported, never fatal

`pyro-server` runs a host-readiness preflight at startup, logs every failed check
with its remedy, and then starts and serves regardless of the result. Nothing in
the server exits on an unready host. The same checks run again before each
`POST /api/sandboxes` and are exposed at `GET /api/ready`.

## Context

Four distinct host-readiness failures — missing bridge, missing `firecracker`
binary, missing rootfs, and not running as root — all collapsed into
`500 {"error":"failed to create sandbox"}` (`internal/api/router.go`), with the
real cause only in the server log. There was no preflight anywhere in
`cmd/server/main.go`; readiness was checked only by `pyro doctor`, which the
server never runs and which goes stale because `ip link` and `ip_forward` are
both non-persistent. A missing guest kernel was the worst of the four: Firecracker
is `Start()`ed but not waited on, so a kernel that will not boot surfaced only as
the 15-second `waitForAgent` timeout.

"Not ready" is the normal first state of a pyro host, not an edge case. A bare
Arch box has `/dev/kvm`, `ip`, `docker` and loop devices, and has no
`firecracker`, no `/opt/pyro`, no bridge and no rootfs.

## Decision

The obvious reading of "fail fast and loudly" is that the preflight should refuse
to start. It must not, for three reasons:

1. `deploy/pyro.service` sets `Restart=on-failure` with `RestartSec=5`. An exiting
   preflight is a five-second crash loop for as long as the host is unready.
2. `POST /api/images` is how an operator fixes the most common failure on a fresh
   host, and it requires the server to be running. Exiting on "no base image"
   locks the operator out of the fix for it.
3. `pyro doctor` probes `/api/health`. A server that exits reports "not reachable",
   which is strictly less information than doctor prints today.

So the division is: **systemd owns fatal, pyro owns diagnosis.** `ExecStartPre`
running `setup-bridge.sh` already fails the unit loudly when the bridge cannot be
created — that is the correct place for fatality, and it was settled in #29. The
server's job is to make the cause legible, which it cannot do from a process that
refused to start.

Readiness is asserted by one package, `internal/hostcheck`, called from three
places: the startup preflight (logs), the create path (pre-check, returns
`503` naming the failed check and its remedy), and `GET /api/ready`.
`cmd/pyro/doctor.go` becomes a renderer over the same package rather than a second
implementation — doctor cannot simply call the endpoint, because its job is
diagnosing a host where the server is down.

## Considered options

Classifying errors as they bubble up from `internal/sandbox` was rejected: it
touches a dozen return sites, and it cannot catch the checks that have no error
site at all, such as `euid != 0`. Pre-checking before the create does any work
costs five stats and one `ip` fork on a path that already forks `ip` three times.

Making a subset fatal — exit on a missing `firecracker` binary but not on a
missing image — was rejected. It adds a second severity axis whose only purpose is
choosing who exits, and exiting still helps nobody diagnose anything.

Extending `/api/health` with a readiness field, or letting it return 503, was
rejected. Three consumers already treat it as liveness: the dashboard, `pyro
doctor`, and the SSE health tick.

## Consequences

A preflight that logs failures and then serves anyway looks like a bug at a
glance. It is not: the log line and the subsequent `503` are the product.

`POST /api/sandboxes` now returns `503` with a named check for host-readiness
failures, where it previously returned an opaque `500`. Neither SDK retries, so
this does not produce retry storms, but a `503` here means operator action is
required and is not usefully retryable by a client.

The create-path pre-check narrows the window between asserting a resource and
using it; it does not close it. A bridge deleted between the check and the tap
attach still fails downstream, as it does today.
