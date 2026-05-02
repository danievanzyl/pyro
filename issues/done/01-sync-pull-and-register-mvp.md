# Sync pull-and-register MVP

## Parent

danievanzyl/pyro#2

## What to build

Extend `POST /images` to accept a `source` field carrying a Docker/OCI image reference (e.g. `python:3.12`, `docker.io/library/node:22-slim`). The handler pulls the image from the registry using a pure-Go path (no Docker daemon), flattens layers onto an ext4 filesystem, injects the pyro-agent binary, and registers the result as a named base image. Pull is synchronous in this slice — the request blocks until the image is `ready`, mirroring the existing `CreateFromDockerfile` UX.

After this slice, a user can `POST /images {name: "py312", source: "python:3.12"}` and then `POST /sandboxes {image: "py312"}` and exec `/usr/local/bin/python --version` inside the sandbox. The image's `ENV` is *not* yet honored (slice 02 adds that), so the user must pass the absolute path.

End-to-end coverage:

- `internal/sandbox/registry` — new module wrapping `go-containerregistry/crane`. Pin platform to `linux/amd64`. Reject manifest with no amd64 variant. Wire `authn.DefaultKeychain` (reads `~/.docker/config.json` and standard credential helpers — no further auth work needed in this slice).
- `internal/sandbox/imageops` — new module with `Ext4Builder` (sparse file → mkfs → mount lifecycle), `AgentInjector` (copies pyro-agent into mounted rootfs), `LayerExtractor` (streams layers onto mount with whiteout/opaque-dir handling).
- `internal/sandbox/images.go` — `ImageManager.CreateFromRegistry(ctx, name, source)` orchestrates the above. `CreateFromDockerfile` refactored to share `Ext4Builder` + `AgentInjector`.
- `internal/api/images.go` — `CreateImageRequest` extended with `Source` field, mutually exclusive with `Dockerfile`. 400 if both/neither.
- Image lands in `{ImagesDir}/{name}/rootfs.ext4` so the existing snapshot pool auto-discovery picks it up.

## Acceptance criteria

- [ ] `POST /images {"name": "py312", "source": "python:3.12"}` returns `201 Created` with `ImageInfo` once the pull and extraction finish.
- [ ] `POST /images` with both `source` and `dockerfile` set returns `400`. Same for neither set.
- [ ] `POST /images` with a manifest that has no `linux/amd64` variant returns `500` with a clear error message.
- [ ] After registration, `POST /sandboxes {"image": "py312"}` boots a sandbox successfully.
- [ ] Inside the sandbox, executing `/usr/local/bin/python --version` returns the expected Python version.
- [ ] The existing Dockerfile flow (`POST /images {name, dockerfile}`) continues to work unchanged.
- [ ] Snapshot pool auto-warms snapshots for the newly registered image (no extra wiring needed; verifies the on-disk layout matches existing convention).
- [ ] No Docker daemon is required on the host to execute the registry pull path.
- [ ] Unit tests for `internal/sandbox/registry`: amd64 selection from a multi-arch index, digest resolution from tag, rejection of arm-only manifests, layer extraction with whiteouts. Tests use `httptest` with committed OCI manifest fixtures; runnable on macOS without KVM.
- [ ] Unit tests for `internal/sandbox/imageops`: `Ext4Builder` and `AgentInjector` Linux-only (build-tag gated, same convention as `vsock_linux.go`). `LayerExtractor` whiteout/opaque-dir cases pure-Go testable on macOS.

## Blocked by

None — can start immediately.
# Issue 01 — completion notes

## Done in this slice

- New pkg `internal/sandbox/registry` — `Puller.Resolve` (amd64 selection from index, single-arch, `ErrNoAmd64Variant` rejection), `Manifest.LayerReader`. Uses go-containerregistry with `authn.DefaultKeychain`.
- New pkg `internal/sandbox/imageops`:
  - `LayerExtractor` — pure-Go, OCI tar with whiteout (.wh.<name>) and opaque dir (.wh..wh..opq). Handles regular files, dirs, symlinks, hardlinks. Path-escape-safe.
  - `Ext4Builder` (Linux build-tag) — sparse file → mkfs.ext4 → mount → unmount.
  - `Ext4Builder` (other) — stub returning errLinuxOnly.
  - `AgentInjector` — pure-Go file copy of pyro-agent into mounted rootfs.
- `ImageManager.CreateFromRegistry(ctx, name, source, puller)` orchestrates resolve → ext4 build → layer extract → agent inject → unmount.
- `POST /images` accepts `source` mutually exclusive with `dockerfile`; 400 on both/neither.
- Tests on macOS: registry httptest with in-process registry from go-containerregistry, layer extractor whiteout/opaque/symlink/path-escape, API mutual-exclusion.

## Not in this slice

- ENV/WORKDIR/USER plumbing → issue 02.
- Async / 202 / status ledger → issue 03.
- Per-layer SSE progress → issue 04.

## Remote KVM verification

Sync pull happy path verifies on a Linux KVM host (`make build-linux deploy`). Cross-compile + macOS unit tests pass; deeper integration (Firecracker boot from registry-pulled python:3.12) is a remote-host smoke test the user should run.
