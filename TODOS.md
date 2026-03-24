# TODOS

## Phase 3 Wiring

- [x] Wire OTEL metrics into server main.go (setup provider, add metrics middleware)
- [x] Wire quota enforcement into POST /sandboxes handler
- [x] Wire audit logging into sandbox lifecycle (create, exec, destroy)
- [x] Add /audit and /images routes to server main.go
- [x] Wire audit logging into file ops (write, read) and TTL expiry in reaper
- [x] Embed SvelteKit static build into Go binary (serve UI from API server)

## Real-time Dashboard Streaming

- [x] In-memory event bus (pub/sub) in Go server — publish on create/exec/destroy/expire
- [x] SSE endpoint: `GET /events?api_key=KEY` — streams sandbox lifecycle + health ticks
- [x] Dashboard page uses EventSource with polling fallback
- [x] Health tick every 5s over SSE (active count)
- [x] Keep WebSocket for exec streaming only (`GET /sandboxes/{id}/ws`)
- [x] Add EventSource to sandboxes + audit pages (currently dashboard only)

## Testing on Homelab

- [x] Deploy updated server with Phase 3 wiring
- [x] Test audit log populates on create/exec/destroy
- [x] Test quota enforcement (exceed max-per-key, verify 429)
- [x] Test TTL reaper auto-destroy (10s TTL sandbox → verified auto-destroyed)
- [ ] Deploy + test embedded UI served from Go binary on :8080
- [ ] Test SSE real-time events in dashboard
- [ ] Test orphan recovery (kill server, restart, verify VM reclaimed or cleaned)
- [ ] Test WebSocket streaming exec
- [ ] Test network isolation iptables policies
- [ ] Test snapshot pool warm boot

## Pre-Public

- [x] Remove ssh root@homelab.local targets from Makefile
- [x] Add README.md
- [x] Add LICENSE
- [ ] CI: GitHub Actions for go test + go build

## Deferred

- [ ] Multi-host clustering (out of scope for v1)
- [ ] GPU passthrough
- [ ] Prometheus /metrics endpoint serving (OTEL provider is wired, needs HTTP handler)
