# Single-flight concurrent attach

## Parent

danievanzyl/pyro#2

## What to build

When two `POST /images` requests for the same image name arrive concurrently, the second request should attach to the in-flight pull rather than fail or start a duplicate. Both callers see the same `202 Accepted` response and observe the same status transitions when polling.

End-to-end coverage:

- `internal/sandbox/imagestate` — `Begin(name)` returns `(*pullOp, attached bool)`. First caller for a name receives `attached=false` and is expected to drive the pull. Subsequent callers while the entry is in `pulling`/`extracting` receive `attached=true` and the same `*pullOp` reference.
- `internal/sandbox/images.go` — `CreateFromRegistry` checks `attached`; if true, skips dispatching a new goroutine and just returns the current state. Both callers' HTTP handlers respond with the same `ImageInfo` payload.
- Implementation note: use `sync.Map[string]*pullOp` or a `map + sync.Mutex` — either is fine. The existing codebase pattern in `internal/sandbox` should guide the choice.
- Edge case: if the in-flight pull's `source` differs from the new request's `source`, return `409 Conflict` with a clear "name already in use with a different source — wait for completion or use force" message. This is distinct from the `force: true` flow added in slice 06.

## Acceptance criteria

- [ ] Two simultaneous `POST /images {name: "x", source: "..."}` requests with the same name and source both receive `202`. Only one actual registry pull occurs (verifiable via test fixture hit count).
- [ ] Both callers observe the same status transitions when polling `GET /images/{name}`.
- [ ] Both callers eventually see `status: "ready"` once the single underlying pull finishes.
- [ ] A second `POST /images` for the same name with a *different* `source` while the first is still pulling returns `409 Conflict`.
- [ ] After completion (`ready` or `failed`), a fresh `POST /images` for the same name follows the normal idempotency / force rules (handled in slice 06; this slice should not regress that path).
- [ ] Unit tests in `imagestate`: same-name concurrent `Begin` returns one fresh op + N attached, different-name concurrent `Begin` returns N fresh ops, attached callers see updates pushed through `Get`.
- [ ] Race detector clean (`go test -race`).

## Blocked by

- 03-async-status-and-ledger.md
