# Async status, ledger, and failure cleanup

## Parent

danievanzyl/pyro#2

## What to build

Make `POST /images` async. Instead of blocking until the pull finishes, the handler kicks off a background pull and returns `202 Accepted` immediately with the initial status. Clients poll `GET /images/{name}` to observe the transition `pulling → extracting → ready` (or `failed`). Sandboxes booted against a non-`ready` image return `409 Conflict` with a clear "image is still pulling" message.

A new in-memory ledger module tracks active pulls and recently-failed pulls. Failed pulls leave a queryable status entry for at least one hour so users can debug why their pull failed without losing the error.

End-to-end coverage:

- `internal/sandbox/imagestate` — new module owning the ledger. Public surface: `Begin(name) (*pullOp, attached bool)`, `Update(name, status)`, `Complete(name, info)`, `Fail(name, err)`, `Get(name)`. Status transitions enforced: `pulling → extracting → ready | failed`. Failed entries TTL out after 1 hour or when a new registration with the same name starts. Clock injected via interface for testability.
- `internal/sandbox/images.go` — `CreateFromRegistry` reworked to dispatch a goroutine and return immediately. The goroutine drives ledger transitions, performs the pull/extract, and on failure calls `os.RemoveAll(images/<name>)` to remove the partial directory.
- `internal/api/images.go` — handler returns `202` with `{name, status: "pulling", source}`. `ImageInfo` (Go struct + JSON) gains: `Status`, `Error`, `Digest`, `Source`. Use `omitzero` JSON tags. `GET /images/{name}` reads from both the ledger (for in-flight/recently-failed) and disk (for ready images).
- `internal/api/sandboxes.go` — sandbox creation against an image whose ledger status is not `ready` returns `409` with a descriptive body.
- Existing Dockerfile flow stays sync (separate slice if we want it async later) — no behavior change for `dockerfile`-set requests in this slice.

## Acceptance criteria

- [ ] `POST /images {name, source}` returns `202 Accepted` within ~100ms regardless of image size.
- [ ] Response body includes `status: "pulling"` and the supplied `source`.
- [ ] `GET /images/{name}` returns the current status; transitions through `pulling`, `extracting`, `ready`.
- [ ] On successful completion, `GET /images/{name}` returns `digest` matching the resolved manifest digest.
- [ ] On failure (e.g. registry unreachable, manifest not found, no amd64 variant), status becomes `failed` with `error` set to a meaningful message.
- [ ] Failed pull's partial directory under `{ImagesDir}/{name}/` is removed.
- [ ] Failed entry remains queryable via `GET /images/{name}` for at least 1 hour after failure.
- [ ] Failed entry is replaced (not duplicated) when a new `POST /images` for the same name starts.
- [ ] `POST /sandboxes {image: "<not-yet-ready>"}` returns `409` with a body indicating the image is still pulling.
- [ ] `POST /sandboxes {image: "<failed>"}` returns `409` with the failure reason surfaced.
- [ ] Existing `POST /images` with `dockerfile` set continues to behave synchronously (no regression).
- [ ] Unit tests for `imagestate`: single happy-path transition, failure path with cleanup, TTL expiry of failed entries (with fake clock), status-transition rejection (e.g. cannot go `ready → pulling`).

## Blocked by

- 01-sync-pull-and-register-mvp.md
