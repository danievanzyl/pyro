# TODOS

## Phase 3 Wiring (in progress)

- [ ] Wire OTEL metrics into server main.go (setup provider, add metrics middleware)
- [ ] Wire quota enforcement into POST /sandboxes handler
- [ ] Wire audit logging into sandbox lifecycle (create, exec, file ops, destroy, TTL expiry)
- [ ] Add /audit and /images routes to server main.go (currently only in router.go)
- [ ] Embed SvelteKit static build into Go binary (serve UI from API server)

## Testing on Homelab

- [ ] Test TTL reaper auto-destroy (create short-TTL sandbox, wait, verify gone)
- [ ] Test orphan recovery (kill server, restart, verify VM reclaimed or cleaned)
- [ ] Test WebSocket streaming exec
- [ ] Test network isolation iptables policies
- [ ] Test snapshot pool warm boot

## Pre-Public

- [ ] Remove ssh root@homelab.local targets from Makefile
- [ ] Add README.md
- [ ] Add LICENSE
- [ ] CI: GitHub Actions for go test + go build

## Deferred

- [ ] Multi-host clustering (out of scope for v1)
- [ ] GPU passthrough
- [ ] Prometheus /metrics endpoint serving
