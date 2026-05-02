# SDK Python — images surface

## Parent

danievanzyl/pyro#2

## What to build

Add an `Images` resource to the Python SDK exposing the new image-registration API. Three callers we want to serve:

1. **Notebook / script user** — wants one idempotent call: "make sure `python:3.12` is registered, wait if needed, then proceed." → `pyro.images.ensure(name=..., source=...)`.
2. **CI / build user** — wants explicit "create now, block until ready, surface failures." → `pyro.images.create_and_wait(...)`.
3. **Long-running service / dashboard** — wants "kick off the pull, don't block startup, observe via events." → `pyro.images.create(...)` returning an operation handle with `.wait(timeout=...)`.

End-to-end coverage:

- `sdk/python/src/pyro_sdk/images.py` — new module. Public API:
  - `Images.create(name, source=None, dockerfile=None, force=False) -> PullOperation`
  - `Images.get(name) -> ImageInfo`
  - `Images.create_and_wait(name, source=None, dockerfile=None, force=False, timeout=None) -> ImageInfo`
  - `Images.ensure(name, source=None, dockerfile=None, timeout=None) -> ImageInfo`
- `PullOperation.wait(timeout=None) -> ImageInfo` — prefers the SSE `/events` stream (already used elsewhere?) when reachable, falls back to interval polling on `GET /images/{name}` (e.g. 1s interval, exponential backoff to 5s ceiling). Raises `ImageRegistrationError` on `failed` status, `TimeoutError` on timeout.
- `Images.ensure` semantics:
  - `GET /images/{name}` first.
  - If `ready` and `source` matches the requested source → no-op, return existing.
  - If `ready` and `source` differs → raise `ImageConflictError` (don't silently force; that's an explicit `create(..., force=True)` decision).
  - If `pulling`/`extracting` → attach by polling.
  - If `failed` or 404 → call `create()` and `wait()`.
- `sdk/python/src/pyro_sdk/client.py` — expose `pyro.images` alongside the existing `pyro.sandbox`.
- `sdk/python/src/pyro_sdk/models.py` — `ImageInfo` dataclass with `name`, `status`, `digest`, `source`, `error`, `labels`, `size`, `created_at`. `omitzero`-friendly parsing.
- Both sync and async client styles supported, matching existing `sandbox.create` / `await pyro.sandbox.create` patterns visible in `client.py`.

## Acceptance criteria

- [ ] `await pyro.images.ensure(name="py312", source="python:3.12")` registers if missing, waits for ready, returns the `ImageInfo`. Second call is a no-op (returns immediately).
- [ ] `await pyro.images.create_and_wait(...)` blocks until ready and surfaces failures as `ImageRegistrationError` with the server's error message.
- [ ] `op = await pyro.images.create(...)` returns immediately with status `pulling`. `await op.wait(timeout=180)` blocks until ready or timeout.
- [ ] `op.wait()` uses SSE when the server supports it; verifiable by monkey-patching the SSE client and checking it was consulted before any polling fallback.
- [ ] `await pyro.images.get(name)` returns the current `ImageInfo`; raises a clean 404-mapped exception if the image doesn't exist.
- [ ] `ensure` with a source that differs from the existing image's recorded source raises `ImageConflictError`.
- [ ] Tests against a fake server (httpx mock or similar — match existing SDK test pattern) cover: ensure-fresh, ensure-existing, create-and-wait happy path, create-and-wait failure path, timeout path.
- [ ] README example updated with the `images.ensure` happy path.

## Blocked by

- 03-async-status-and-ledger.md
- 04-sse-image-lifecycle-events.md
