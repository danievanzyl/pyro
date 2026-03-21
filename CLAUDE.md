# firecrackerlacker

Ephemeral Firecracker microVM sandbox platform for agentic workloads.

## Architecture

- Go monorepo: `cmd/server` (API), `cmd/agent` (in-VM vsock agent), `cmd/fcctl` (CLI)
- `internal/` packages: api, sandbox, protocol, store
- SQLite for state, vsock for host↔guest communication
- JSON-over-length-prefix wire protocol

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

## API Endpoints

### Sandbox Lifecycle
- `POST /sandboxes` — create sandbox with TTL
- `GET /sandboxes` — list active sandboxes
- `GET /sandboxes/{id}` — get sandbox details
- `DELETE /sandboxes/{id}` — destroy sandbox
- `POST /sandboxes/{id}/exec` — execute command (sync)
- `PUT /sandboxes/{id}/files/*` — write file into sandbox
- `GET /sandboxes/{id}/files/*` — read file from sandbox
- `GET /sandboxes/{id}/ws?api_key=KEY` — WebSocket streaming exec

### Images
- `GET /images` — list base images
- `GET /images/{name}` — get image info
- `POST /images` — create image from Dockerfile

### System
- `GET /health` — health check

## Phases

- Phase 1 (done): Core sandbox API, auth, TTL reaper, vsock exec
- Phase 2 (done): Snapshot pools, file API, WebSocket streaming, image mgmt
- Phase 3 (done): OTEL metrics, SvelteKit dashboard, network policies, quotas, audit log
