# The runner dashboard stops the controller and destroys sandboxes; GitHub cancels jobs

The dashboard's runner page carries three controls with two different subjects.
**Start** and **Stop** act on the runner controller: one global switch that gates
message fetching, leaving in-flight jobs untouched. **Destroy** acts on one runner
sandbox and ends it immediately. There is no Cancel, because pyro cannot cancel a
GitHub Actions job and does not need to — GitHub's own Cancel already reaches the
controller over the message session.

## Context

#20 recorded the ask as "a list of runners, with start, stop and cancel", and #27
carried three working readings: start resumes the controller, stop pauses it, and
cancel destroys one runner VM, after which "GitHub sees the runner vanish and fails
or requeues the job (up to 3 times per #16)".

Under #20 D4 — strictly one job to one microVM — the three verbs do not share a
subject. Start and stop belong to the controller, which after #25 is one message
session per enabled repository. Destroy belongs to a sandbox. And cancel belongs to
a job, which is the one of the three pyro does not own.

The requeue premise is wrong. `actions/scaleset` documents the three-attempt
reassignment as covering a job "assigned to your scale set but not acquired by a
runner in time". A job whose runner has already started it and then dies is not
requeued: it fails with "The self-hosted runner … lost communication with the
server". Destroying a busy runner sandbox therefore fails that job permanently.

Nor is the failure prompt. The runner holds a job lease rather than sending a
heartbeat — `JobDispatcher.RenewJobRequestAsync` renews every 60 seconds and the
service only gives up at `LockedUntil + 5 minutes`. Reports of a dead runner
holding a job for eleven minutes (actions/runner#4598) match. The VM disappears at
once; the GitHub job reads "in progress" for roughly ten minutes afterwards.

## Decision

**Cancel is GitHub's verb and pyro does not borrow it.** Three independent reasons,
any one sufficient:

There is no per-job cancel endpoint to call. Enumerated against GitHub's own
OpenAPI description and the public GraphQL schema, the only job-scoped write in the
whole Actions API is `rerun`. Cancellation is run-scoped — `/runs/{id}/cancel` and
`/force-cancel` — so even with the `Actions: write` that #25 deliberately did not
take, the button would cancel an entire workflow run to stop one job.

Cancel is already wired, inbound. Cancelling a running job delivers
`JobCompleted{Result: "canceled"}` to the message session, with `RunnerName` set and
`FinishTime` left at its zero value. The controller learns of the cancellation with
no REST polling, and destroys that sandbox itself. The operator's cancel button is
the one already on the GitHub Actions page.

And the word would mislead on the one screen where both meanings are visible.
CONTEXT.md's standing rule is to avoid overloaded terms; a pyro control labelled
Cancel next to a GitHub control labelled Cancel, doing something materially
different, is the case the rule exists for.

**Destroy survives on its own merits, narrowed.** It is not the ordinary way a
runner ends — that is the runner process exiting and #15's `on_exit: destroy`. It
covers two cases. A sandbox that is genuinely stuck, where the alternative today is
the TTL backstop or a restart that destroys every other sandbox too (#52). And
beating the message lag: scaleset#122 shows an in-flight `GetMessage` long poll not
waking for a lifecycle message, so a GitHub-side cancel can take about fifty
seconds to arrive. Destroy frees the slot now. Its confirmation states what it
does: the job fails, and GitHub will show it running for about ten more minutes.

**Stop gates the fetch; it does not close the session.** The controller's loop
checks one in-memory flag before calling `GetMessage`. No message is delivered, so
none is left unacknowledged and the three-attempt reassignment counter is never
burned while stopped. In-flight sandboxes need nothing from the controller — #15
put `on_exit: destroy` server-side — so they finish normally. One global switch,
not one per repository. The flag is not persisted: a restart resumes, which is
correct because the reason to stop is usually to open the idle window #25 requires
for credential rotation, and rotation ends in a restart.

**Stop is a short operational action, not a way to disable the controller.** Jobs
queued for a self-hosted runner die at GitHub's 24-hour job-queue limit, and the
message session returns at most 50 messages per response with larger backlogs
truncated. #21 deliberately has no REST catch-up poller, so a long enough stop can
strand jobs that GitHub still holds and pyro will never be told about. Disabling
the controller properly is removing its GitHub configuration and restarting.

**The list is new routes, because it cannot be anything else.** `store.Sandbox`
has no job, repository or label column, and #15 explicitly refused to add `labels`
or `metadata` to it; that state lives in the controller's own table. Three routes,
registered only when the controller is configured, per ADR 0003:

- `GET /api/runners` returns an envelope,
  `{"controller": {"state", "repos", "max_concurrent", "last_error"}, "runners": [...]}`.
  A bare array would have four indistinguishable empty cases — idle, stopped,
  unconfigured, and credential failure — and the fourth is the one an operator must
  not mistake for the first.
- `PUT /api/runners/controller` sets the switch.
- `DELETE /api/runners/{sandbox_id}` destroys one runner sandbox. It **must** first
  confirm that id appears in the controller's table; without that check it is a
  general bypass of the owner check rather than a runner-scoped one.

**The controller's table stores no status.** `GET /api/runners` joins its job rows
to `sandboxes` and returns only those whose sandbox is still active. Anything else
needs a writer for a "finished" column, and under this decision nothing is watching
while the controller is stopped — rows would read running forever. The join cannot
go stale, and `Manager.Reconcile` already repairs sandbox state after a restart, so
the list self-heals at no cost. Rows are keyed on `RunnerRequestID`, the same value
#21 already uses for its uniqueness constraint and the only identifier carried on
all four message types.

**Runner sandboxes stay absent from `GET /api/sandboxes`, and #52 is accepted as
filed.** The filter being bypassed is not a security boundary: any valid API key
can already list, mint and delete API keys, read the entire audit log across all
keys, and — because `sse.go` validates a key and then subscribes to an unfiltered
bus — receive events for every sandbox on the host, runner sandboxes included.
Sandbox read and write is the only key-scoped surface in the API. Deleting the
owner check outright would be simpler still and would make the question disappear,
but it drops the API's only key-scoping and is a one-way door for any future
tenancy claim, so it stays a separate decision.

## Consequences

ADR 0003 states that the controller's own API key row means "the dashboard filters
them like any client's". That is no longer the whole picture: the runner page reads
the controller's table and acts on runner sandboxes directly, and that page is the
only place they are listed or destroyed. `docs/runners.md` must say so, or the
generic list omitting them reads as a defect.

The unfiltered SSE bus stops being an oversight and becomes something this page
depends on. The runner page refetches on `sandbox.destroyed` and
`sandbox.workload.exited`, which it only receives because the bus does not filter by
key. One new event type is added, `runner.started`; the end of a runner is already
covered by #15's events.

The controller must handle `JobCompleted` by destroying that job's sandbox,
whatever the result. #21 enumerates six client calls and no such handler. ARC has
exactly that gap — `HandleJobCompleted` only sets a dirty flag and teardown is
count-based — and it is open as ARC#4603, filed by someone running Firecracker
whose runner stayed live for seven hours. Per-job teardown is available to us and
not to ARC, because the abandoned attempt's `JobCompleted` does arrive.

A row shows repository, job display name linked to
`https://github.com/{owner}/{repo}/actions/runs/{run_id}`, requested label, sandbox
id, elapsed time and sandbox state. The message's `JobID` is a GUID string and not
the REST job id, and no field carries the REST job id at all, so `WorkflowRunID`
plus `JobDisplayName` is the only route back to the job in GitHub. The sandbox id is
not a link: `/sandboxes/{id}` answers 404 for a runner sandbox.

The page is reachable from a new top-level nav entry and is the dashboard's only
new surface. No Python or TypeScript SDK method and no `pyro` CLI command — the
SDKs serve sandbox users, this serves the host operator.
