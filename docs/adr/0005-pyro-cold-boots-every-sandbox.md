# pyro cold-boots every sandbox and never restores from a snapshot

Every sandbox is a fresh Firecracker boot. pyro does not keep warm snapshots, does
not call `PUT /snapshot/load`, and the snapshot pool that shipped in Phase 2 is
removed rather than left dormant. Start latency is a per-host property to be
measured, not a fixed budget to optimise against.

## Context

`internal/sandbox/pool.go` booted a VM, paused it through the Firecracker API and
wrote a full snapshot per image. It never ran. `Claim()` had no callers, and — the
fact that reframes the question — **there was no restore code anywhere in the
repository**. Nothing called `PUT /snapshot/load`. `Claim()` returned a struct no
code could consume, so "make the pool real" was never a matter of adding a call
site; it was the whole restore path, unwritten.

The pool had also never executed on any host. `pool_test.go` exercised only
`Invalidate`, against hand-staged files. `deploy/pyro.service` stopped passing
`--pool-size` in #36, and `cmd/pyro/setup_test.go` asserts it stays off — a test
that CI does not run, because `ci.yml` is pinned to `./internal/...` (#39). Six
commits of maintenance had gone into code that had never served a sandbox.

Nor was there a latency number to justify the work. On the host this platform
targets, Firecracker was not installed, no state directory existed, and ninety
days of intact journal contained no `create:` line. The premise circulating in the
ticket — that Firecracker's docs claim sub-30ms restore — does not hold either:
at v1.17.0 the docs give no restore latency figure at all, only "depends on the
memory size, vCPU count and emulated devices count". The published `<125 ms` is
cold boot.

## Decision

Cold boot, on demand, one microVM per sandbox. Five independent reasons, any one
of which is sufficient.

**Firecracker calls the pattern insecure.** `snapshot-support.md` states: "we
consider resuming execution from the same state more than once insecure." Its
worked Example 3 — load snapshot S into VM B, load the same S into VM C — is
precisely a warm pool, and labels both microVMs insecure. This is the vendor's own
position on the feature, not a third party's.

**A snapshot fixes the machine shape.** `vcpu_count` and `mem_size_mib` are
pre-boot-only and are restored from the snapshot; `/machine-config` is rejected
post-boot. The pool hardcoded 1 vCPU and 256 MiB, so a restored sandbox could
never be the multi-core, multi-gigabyte VM a CI job needs.

**Clones collide on the network.** `guest_mac` cannot be overridden at load, and
every clone resumes with the same guest IP. Firecracker's prescribed answer is a
network namespace per clone with veth pairs and per-namespace NAT. pyro runs one
shared bridge with port isolation; adopting snapshots would mean rewriting the
network model to serve them.

**Distribution upgrades poison the pool.** The guest platform takes Firecracker
from the distribution package (ADR 0001's sibling decision in #29). The snapshot
format's major version bumps on every change to microVM state, and a mismatch ends
the Firecracker process rather than failing softly. Snapshots are also bound to
the exact CPU model. A routine system upgrade would silently invalidate every
warm snapshot.

**Per-job state survives the snapshot.** VMGenID reseeds the guest kernel CSPRNG
on resume, but Firecracker documents a race window before that lands and states
plainly that unique identifiers, cached random numbers and cryptographic tokens
"will still be replicated". The guest wall clock resumes from snapshot time.
pyro-agent has no clock sync and no reseed step. A sandbox that talks TLS to an
external service with a stale clock and a replayed entropy pool fails in ways that
look like a pyro bug.

## Considered options

**Wiring up restore** was rejected. It costs a restore path, a
namespace-per-sandbox network rewrite, a guest clock and entropy protocol, and a
pinned Firecracker version — to optimise a number nobody had measured, against a
vendor recommendation not to.

**Leaving the pool dormant and deciding later** was rejected. That is the status
quo which produced six maintenance commits on never-executed code, a `--pool-size`
flag advertising a capability that does not exist, and a README feature bullet
claiming "sub-second sandbox creation with snapshot pools". Dead code that a flag
advertises is a trap for the next reader.

**A pool of warm booted sandboxes** — VMs that have completed boot but not yet
received their workload — was not rejected on merit. It is the designated answer
if a host ever proves to need one, and every objection above dissolves for it:
separate VMs have their own CIDs, addresses, clocks and entropy, and no coupling
to the Firecracker build. It is not built, because nothing yet shows a host that
needs it.

## Consequences

Start latency becomes something each host answers for itself. The create path is
instrumented end to end so a given host and its storage can be measured, rather
than a single number being assumed for all of them. The known avoidable cost is
the agent-readiness poll, a fixed 200 ms tick with no fast first attempt, which
quantises every measured boot upward.

The per-sandbox rootfs copy is not a cost worth optimising: `copyFile` reflinks
through the `FICLONE` ioctl, verified O(1) on this platform's btrfs regardless of
image size. Copy-on-write was already the answer, and it is already in place. The
one caveat is that reflink fails on tmpfs, so a state directory must not be placed
there.

Removing the pool also removes `fcapi.go`, which had no other caller. pyro is left
with no Firecracker HTTP API client at all: it configures each microVM through a
config file at spawn and talks to the guest only over vsock. Restoring that client
is part of the cost of ever reversing this decision.
