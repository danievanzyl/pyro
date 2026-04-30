# Disk cap (`MaxImageSizeMB`)

## Parent

danievanzyl/pyro#2

## What to build

Add a configurable per-image size cap so a careless `tensorflow:latest-gpu` pull can't fill the host disk. The check happens *before* any bytes are written: `Σ layer_size × 1.3 > MaxImageSizeMB → 413 Payload Too Large`. The factor of 1.3 accounts for ext4 overhead and the headroom already used by the existing rootfs auto-sizer (commit `783cffe`).

Default: 4096 MB (covers slim Python/Node and `nvidia/cuda:*-runtime`, rejects full CUDA dev images that are typically inappropriate as sandbox bases anyway). Operators override via config (same mechanism as `ImagesDir`, `AgentBinaryPath`, etc.).

End-to-end coverage:

- `internal/sandbox/imageops` — new `SizeBudget` module: pure-logic `Check(layerSizes []int64, capMB int) error`. Returns a sentinel error type (e.g. `ErrImageTooLarge`) that the API layer maps to `413`.
- `internal/sandbox/registry` — `Puller` exposes `LayerSizes(ctx, ref) ([]int64, error)` that resolves the manifest without downloading layers (just a HEAD/GET on the manifest).
- `internal/sandbox/images.go` — `CreateFromRegistry` calls `Puller.LayerSizes` first, then `SizeBudget.Check` against `cfg.MaxImageSizeMB`. On failure, mark the ledger entry as `failed` with the size-cap reason and return immediately. No partial directory is created.
- `internal/sandbox/images.go` — `ImageConfig` struct gains `MaxImageSizeMB int`. Default constant `defaultMaxImageSizeMB = 4096` applied if zero.
- `internal/api/images.go` — handler maps `ErrImageTooLarge` → `413 Payload Too Large` with body `{error: "image too large", limit_mb, estimated_mb}`.

## Acceptance criteria

- [ ] `POST /images {source: "<some-large-image>"}` where `Σ layer_size × 1.3 > MaxImageSizeMB` returns `413` synchronously (before async pull starts).
- [ ] Response body contains the configured limit and the estimated decompressed size for debuggability.
- [ ] No directory is created under `{ImagesDir}/{name}/` when size-capped.
- [ ] Default `MaxImageSizeMB` is 4096 if not configured.
- [ ] Operator-supplied override via the existing config mechanism (env var or config file — match what already wires `ImagesDir`) takes effect.
- [ ] `python:3.12-slim`, `node:22-slim`, `nvidia/cuda:12-runtime` all pass the default cap. Full `nvidia/cuda:12-devel` is rejected at default.
- [ ] Unit tests for `SizeBudget`: pure logic, runs on macOS. Cover at-cap, just-under, just-over, empty layer list (edge case → reject as malformed).
- [ ] Integration test (or manual smoke) verifies the 413 path against a fixture-served large manifest.

## Blocked by

- 01-sync-pull-and-register-mvp.md
