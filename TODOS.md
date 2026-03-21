# TODOS

## Phase 3 Wiring

- [x] Wire OTEL metrics into server main.go (setup provider, add metrics middleware)
- [x] Wire quota enforcement into POST /sandboxes handler
- [x] Wire audit logging into sandbox lifecycle (create, exec, destroy)
- [x] Add /audit and /images routes to server main.go
- [ ] Wire audit logging into file ops (write, read) and TTL expiry in reaper
- [ ] Embed SvelteKit static build into Go binary (serve UI from API server)

## Testing on Homelab

- [x] Deploy updated server with Phase 3 wiring
- [x] Test audit log populates on create/exec/destroy
- [x] Test quota enforcement (exceed max-per-key, verify 429)
- [x] Test TTL reaper auto-destroy (10s TTL sandbox → verified auto-destroyed)
- [ ] Test orphan recovery (kill server, restart, verify VM reclaimed or cleaned)
- [ ] Test WebSocket streaming exec
- [ ] Test network isolation iptables policies
- [ ] Test snapshot pool warm boot

## Real-time Dashboard Streaming

- [ ] In-memory event bus (pub/sub) in Go server — publish on create/exec/destroy/expire
- [ ] SSE endpoint: `GET /events?api_key=KEY` — streams sandbox lifecycle + health ticks
- [ ] Replace SvelteKit polling with EventSource on dashboard, sandboxes, audit pages
- [ ] Health tick every 5s over SSE (active count, host resource usage)
- [ ] Keep WebSocket for exec streaming only (`GET /sandboxes/{id}/ws`)

## Pre-Public

- [ ] Remove ssh root@homelab.local targets from Makefile
- [ ] Add README.md
- [ ] Add LICENSE
- [ ] CI: GitHub Actions for go test + go build

## Deferred

- [ ] Multi-host clustering (out of scope for v1)
- [ ] GPU passthrough
- [ ] Prometheus /metrics endpoint serving (OTEL provider is wired, needs HTTP handler)
- [ ] Audit logging for file read/write operations
