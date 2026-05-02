# SDK TypeScript — images surface

## Parent

danievanzyl/pyro#2

## What to build

Mirror the Python SDK's `images` surface in the TypeScript SDK so cross-language teams have a consistent API. Same three caller personas (notebook/idempotent, CI/explicit, service/non-blocking).

End-to-end coverage:

- `sdk/typescript/src/images.ts` — new module. Public API:
  - `images.create({ name, source?, dockerfile?, force? }): Promise<PullOperation>`
  - `images.get(name): Promise<ImageInfo>`
  - `images.createAndWait({ name, source?, dockerfile?, force?, timeout? }): Promise<ImageInfo>`
  - `images.ensure({ name, source?, dockerfile?, timeout? }): Promise<ImageInfo>`
- `PullOperation.wait(timeout?: number): Promise<ImageInfo>` — SSE-preferred with interval polling fallback. Throws `ImageRegistrationError` on failure, `TimeoutError` on timeout.
- Same `ensure` semantics as Python: source-mismatch on existing ready image throws `ImageConflictError`; never silently forces.
- `sdk/typescript/src/types.ts` — `ImageInfo` interface mirroring the Python dataclass: `name`, `status`, `digest`, `source`, `error`, `labels`, `size`, `createdAt`.
- `sdk/typescript/src/client.ts` — expose `pyro.images` alongside the existing `pyro.sandbox`.
- Match existing TypeScript SDK conventions (camelCase, async/await, no callback-style API).

## Acceptance criteria

- [ ] `await pyro.images.ensure({ name: "py312", source: "python:3.12" })` registers if missing, waits, returns `ImageInfo`. Second call is a no-op.
- [ ] `await pyro.images.createAndWait(...)` blocks until ready; failures throw `ImageRegistrationError`.
- [ ] `const op = await pyro.images.create(...)`; `await op.wait(180_000)` blocks with timeout in ms (match existing SDK timeout convention).
- [ ] `op.wait()` uses SSE when the server supports it; verifiable in tests via mocked EventSource.
- [ ] `await pyro.images.get(name)` returns the current `ImageInfo`; throws clean 404 for missing names.
- [ ] `ensure` with source mismatch on an existing ready image throws `ImageConflictError`.
- [ ] Vitest/Jest (whichever the SDK uses) test suite covers: ensure-fresh, ensure-existing, createAndWait happy/failure, timeout. Use the same fake-server pattern already present in `sdk/typescript/tests/`.
- [ ] README example updated with the `images.ensure` happy path.

## Blocked by

- 03-async-status-and-ledger.md
- 04-sse-image-lifecycle-events.md
