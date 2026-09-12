# The runner controller lives in pyro, behind a one-way dependency boundary

The GitHub Actions runner controller is a package inside `danievanzyl/pyro`
(`internal/runner`) and a goroutine inside `pyro-server`, not a sibling repo and
not a separate binary. `internal/api` may import it; `internal/sandbox` may not.
With no GitHub configuration present the controller never starts, registers no
routes, and changes no behaviour.

## Context

pyro describes itself as "a Firecracker microVM sandbox platform for AI agents".
A GitHub Actions runner backend is a different product for a different audience,
so putting it in the same repository is a decision rather than a formality. Three
homes were live: this repo, a sibling repo consuming a pyro SDK, or a GARM
provider plugin. #20 closed build-vs-wrap as build, which removed the GARM plugin.

## Decision

The sibling repo was the real alternative, and it fails on a fact rather than a
preference. #21 locked the trigger to `github.com/actions/scaleset`, a Go library
— the only maintained client of the Actions Service message-session protocol.
`sdk/` holds a Python SDK and a TypeScript SDK and no Go SDK. So a sibling repo is
either Python or TypeScript reimplementing that protocol by hand, or Go with no
SDK to consume, hand-rolling an HTTP client for pyro. Both pay for a separation
that a single-host personal project never collects on.

The usual counterargument to vendoring a foreign concern is dependency weight, and
here it does not apply. Importing `github.com/actions/scaleset` adds three modules
to `go.mod` — `golang-jwt/jwt/v4`, `hashicorp/go-cleanhttp`, `hashicorp/go-retryablehttp`
— with `google/uuid` already present and no Kubernetes anywhere in the graph.

The sibling repo's one genuine virtue is that it forces every capability through
the public HTTP API. That virtue survives without it: #15 specifies the job
primitive as a public `POST /sandboxes` shape (`env`, `workload`), so the API is
built to completion regardless of who calls it.

What replaces the repo boundary is a compile-time one. `internal/sandbox` — the
sandbox platform proper — must never import `internal/runner`. That is the
enforceable form of "GitHub concerns do not leak into a general sandbox platform":
a build error, not a guideline. `internal/api` does import `internal/runner`, as a
concrete nil-when-disabled field on `ServerConfig`, matching `Pool` and `ImageMgr`.
The resulting api-to-runner-to-api cycle for SSE is broken the way the repo already
breaks it — `internal/runner` declares its own `Emitter` interface and `cmd/server`
passes `*api.EventBus`, exactly as `ImageManager.SetEmitter` does.

In-process rather than a second binary because `store.New` sets
`SetMaxOpenConns(1)`: the code assumes one process owns `pyro.db`, and the
controller keeps its own table in that database. In-process also sequences the
controller's restart reclaim after `Manager.Reconcile`, and lets the runner
dashboard (#27) serve from the existing embedded UI rather than a second HTTP
surface.

## Consequences

The controller calls `Manager.CreateSandbox` directly rather than looping back
through its own HTTP API, so it does not inherit what the handler does around that
call. It compensates explicitly: it owns a real API key row, so `sandboxes.api_key_id`
attributes its VMs and the dashboard filters them like any client's, and it writes
its own `store.LogAudit` entries.

That filtering is narrower than this sentence implies, which #52 surfaced and
#27 settled: runner sandboxes belong to a key the operator does not hold, so
`GET /api/sandboxes` omits them entirely. The runner page reads the controller's
own table instead and is the only place they are listed or destroyed. See
`docs/adr/0007-the-dashboard-stops-the-controller-and-destroys-sandboxes.md`.

A reader who finds GitHub Actions code in a sandbox platform and assumes it was
never considered will be wrong; this is that consideration. The one-way import
rule is cheap to hold and expensive to reinstate once breached, which is the
reason it is written down rather than left to judgement.
