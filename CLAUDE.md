# Pyro

Open-source Firecracker microVM sandbox platform for AI agents.

## Architecture

- Go monorepo: `cmd/server` (API), `cmd/agent` (in-VM vsock agent), `cmd/pyro` (CLI)
- `internal/` packages: api, sandbox, protocol, store
- SQLite for state, vsock for host↔guest communication
- JSON-over-length-prefix wire protocol
- Python SDK: `sdk/python/` — `pyrovm-sdk` on PyPI
- TypeScript SDK: `sdk/typescript/` — `@pyrovm/sdk` on npm

## Build

```
make build          # build all binaries
make build-agent    # cross-compile agent for Linux
make build-linux    # cross-compile everything for Linux
make test-unit      # unit tests (macOS ok)
make test           # full tests (requires Linux + KVM)
```

## Development

- Agent must be cross-compiled: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`
- vsock only works on Linux — macOS builds use stubs
- Integration tests require a KVM host with Firecracker installed
- API key prefix: `pk_` (pyro key)
- Env vars: `PYRO_API_KEY`, `PYRO_BASE_URL`

## API Endpoints

All routes live under `/api`.

### Sandbox Lifecycle
- `POST /api/sandboxes` — create sandbox with TTL
- `GET /api/sandboxes` — list active sandboxes
- `GET /api/sandboxes/{id}` — get sandbox details
- `DELETE /api/sandboxes/{id}` — destroy sandbox
- `POST /api/sandboxes/{id}/exec` — execute command (sync)
- `PUT /api/sandboxes/{id}/files/*` — write file into sandbox
- `GET /api/sandboxes/{id}/files/*` — read file from sandbox
- `GET /api/sandboxes/{id}/ws?api_key=KEY` — WebSocket streaming exec

### Images
- `GET /api/images` — list base images
- `GET /api/images/{name}` — get image info
- `POST /api/images` — create image from Dockerfile

### Streaming
- `GET /api/events?api_key=KEY` — SSE event stream (sandbox lifecycle + health ticks)

### System
- `GET /api/health` — health check
- `GET /health` — backward-compat alias for `/api/health`

## Security Notes

- SSE and WebSocket endpoints pass API keys via query param (can't set headers). Scrub `api_key=` from access logs in production.

## Phases

- Phase 1 (done): Core sandbox API, auth, TTL reaper, vsock exec
- Phase 2 (done): Snapshot pools, file API, WebSocket streaming, image mgmt
- Phase 3 (done): OTEL metrics, SvelteKit dashboard, network policies, quotas, audit log
- Phase 4 (current): Rebrand to Pyro, Python + TypeScript SDKs, examples, docs
