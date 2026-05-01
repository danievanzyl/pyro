# Force re-registration and snapshot pool invalidation

## Parent

danievanzyl/pyro#2

## What to build

Allow users to deliberately replace an existing image with a new pull of the same source via `force: true`. By default, registering a name that already exists is a no-op idempotency check (the server returns the existing `ImageInfo`); `force: true` triggers a fresh pull and atomic replacement.

When a force-replace happens, the snapshot pool's warm snapshots for that image must be invalidated — they reference the old rootfs's blocks, so post-replace sandbox creates from those snapshots would corrupt. Running sandboxes are unaffected because each holds its own copy of the rootfs (`internal/sandbox/manager.go` already copies on sandbox create).

A `image.force_replaced` audit event is emitted on the SSE stream so operators can track when base images change.

End-to-end coverage:

- `internal/sandbox/pool.go` — new `Invalidate(image string)` method that drains `ready[image]` and removes the corresponding snapshot files. Pool's replenishment loop will refill on its own cadence.
- `internal/sandbox/images.go` — `CreateFromRegistry(ctx, name, source, force)` handles three cases: (a) name unknown → start pull; (b) name known + `!force` → return existing `ImageInfo` with no pull; (c) name known + `force` → call `pool.Invalidate(name)`, then start pull and atomically swap the rootfs file when extraction completes (write to a temp path, rename over the old file).
- `internal/sandbox/imagestate` — reject `force: true` when the name has an in-flight pull (`pulling`/`extracting`) — return `409 Conflict` with "can't force during active pull".
- `internal/api/images.go` — `CreateImageRequest` gains `Force bool`. Behavior wired to the manager call.
- SSE emitter — emit `image.force_replaced {name, old_digest, new_digest}` after successful force replacement.
- Audit visibility: surface `force_replaced` events distinctly from regular `image.ready` so dashboards can render them as a separate timeline (no schema change beyond the event type).

## Acceptance criteria

- [ ] `POST /images` with `name` already present and `force: false` (or omitted) returns `200 OK` with the existing `ImageInfo` and triggers no pull.
- [ ] `POST /images` with `force: true` triggers a fresh pull and updates the on-disk rootfs.
- [ ] After a successful force-replace, `GET /images/{name}` returns the new digest.
- [ ] Warm snapshots for the image present before the force-replace are removed; pool stats reflect zero ready snapshots until replenishment runs.
- [ ] Running sandboxes booted from the old image continue running normally and can complete their work without errors.
- [ ] Sandboxes created after the force-replace use the new rootfs (verifiable by checking a known-changed file inside the sandbox).
- [ ] `POST /images` with `force: true` while another pull for the same name is in flight returns `409 Conflict` with a descriptive error.
- [ ] An `image.force_replaced` SSE event with `name`, `old_digest`, `new_digest` is emitted on success.
- [ ] Atomic swap: a force pull that fails mid-extraction does not corrupt the existing rootfs — the old rootfs remains intact and `GET /images/{name}` shows `status: failed` with the prior `ready` info still recoverable (or, equivalently, the failure leaves the prior image untouched).
- [ ] Unit tests for `pool.Invalidate`: drains the ready slice, removes snapshot files, idempotent for unknown image names.

## Blocked by

- 03-async-status-and-ledger.md
- 04-sse-image-lifecycle-events.md
