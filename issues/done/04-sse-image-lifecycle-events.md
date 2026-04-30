# SSE image lifecycle events

## Parent

danievanzyl/pyro#2

## What to build

Emit image lifecycle events on the existing SSE `/events` stream so dashboards and SDKs can react to pull state changes without polling. Per-layer progress events (`image.layer_progress`) carry layer digest, bytes done, and bytes total — enough to render a progress bar.

End-to-end coverage:

- `internal/observability` (or wherever the existing SSE bus lives) — add image event types. Same emitter pattern as existing sandbox lifecycle and health-tick events.
- `internal/sandbox/imagestate` — ledger transitions emit `image.pulling`, `image.extracting`, `image.ready`, `image.failed` events as side effects of `Update`/`Complete`/`Fail`. Inject the event emitter as a dependency to keep the ledger testable.
- `internal/sandbox/registry` — `Puller` exposes a per-layer progress callback. The orchestrator in `images.go` translates progress callbacks into `image.layer_progress` SSE events. Throttle callback frequency (e.g. emit every N MB or every M ms) to avoid flooding the stream on small layers.
- Event payloads:
  - `image.pulling`: `{name, source}`
  - `image.layer_progress`: `{name, layer_digest, bytes_done, bytes_total}`
  - `image.extracting`: `{name}`
  - `image.ready`: `{name, digest, size}`
  - `image.failed`: `{name, error}`

## Acceptance criteria

- [ ] Connecting a client to `/events?api_key=...` and triggering `POST /images` produces in order: `image.pulling`, one or more `image.layer_progress`, `image.extracting`, `image.ready`.
- [ ] Failed pull emits `image.pulling` followed by `image.failed` with `error` populated.
- [ ] `image.layer_progress` events include `layer_digest`, `bytes_done`, `bytes_total`. `bytes_done` increases monotonically per layer.
- [ ] Progress events are throttled — small layers (<1 MB) emit at most a couple of progress events; large layers emit at a steady cadence.
- [ ] Existing SSE event types (`sandbox.*`, health ticks) continue to work unchanged.
- [ ] `image.layer_progress` events do not appear after `image.extracting` for that image.
- [ ] Event emitter tests verify ledger transitions trigger the correct events with the right payload.

## Blocked by

- 03-async-status-and-ledger.md
