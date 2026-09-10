# The guest kernel is a host resource, not a per-sandbox choice

A pyro host has exactly one guest kernel, at the path given by `--kernel`
(default `/opt/pyro/images/vmlinux`). Base images are rootfs-only. There is no
per-image kernel, no versioned kernel store, and no way for a caller to pick a
kernel per sandbox.

## Context

Three incompatible layouts for the same word coexisted in the tree: an
image-owned kernel (`<name>/vmlinux`, asserted by the `internal/sandbox/images.go`
package doc), a shared host kernel (`images/vmlinux`, what `pyro build-kernel`,
`pyro doctor` and both systemd units actually use), and a versioned listable
store (`images/vmlinux-<version>`, which `ListKernels` scanned for and **no code
path ever wrote**).

The third layout was not merely unused — it broke the product. `handleCreateSandbox`
called `ResolveKernel` on every create, `ResolveKernel` delegated to `ListKernels`,
and `ListKernels` matched only `vmlinux-<version>`. On any host provisioned the
documented way, every `POST /sandboxes` returned
`400 {"error":"kernel: no kernels found in /opt/pyro/images"}`, and the `--kernel`
flag was never consulted. Unit tests stayed green because no fixture wrote a
versioned kernel name and no test exercised the create path with an image manager
attached.

## Decision

The host-singular layout wins, because it is the only one anything writes.
Removed: `ListKernels`, `ResolveKernel`, `GET /kernels`, the `kernel` field on
`CreateSandboxRequest`, the per-image kernel fallback in `ImageManager.Get`, and
the kernel selector in the dashboard. `Manager.CreateSandbox` already fell back to
the configured `--kernel` when `VMResources.KernelPath` was empty, so restoring the
create path meant deleting the resolution call, not adding a fallback.

## Considered options

A host-level versioned store — teach `pyro build-kernel` to write
`vmlinux-<version>` and redefine `--kernel` as "latest from the store" — was
rejected. It would touch `build-kernel`, `doctor`, both service units, the router
and the UI to deliver per-sandbox kernel choice that no caller has ever asked for.
Widening `ListKernels` to also match bare `vmlinux` was rejected as a patch that
leaves the vocabulary conflict in place.

## Consequences

`kernel` disappearing from `CreateSandboxRequest` is visible to SDK users who set
it. Changing the host's kernel is a `--kernel` change plus a server restart, and
it applies to every sandbox at once. Reintroducing per-sandbox kernel selection is
additive but is a deliberate reversal of this ADR, not a gap to be filled in.
