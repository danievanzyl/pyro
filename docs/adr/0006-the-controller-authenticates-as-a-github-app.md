# The runner controller authenticates as a GitHub App, and pyro stores no second credential

The runner controller needs a GitHub credential to register runner scale sets and
mint per-job JIT configs. It authenticates as a **GitHub App**, installed on
selected repositories with `Administration: Read and write` and
`Metadata: Read`. The App's private key is the only secret pyro stores, and it
lives in a file on the host named by `--github-app-key-file` — never in a flag
value, never in `deploy/pyro.service`, which is public.

## Considered options

**Classic PAT.** Rejected. It is the only other credential that never expires,
but its `repo` scope grants code write to every repository the account can
reach, including repositories owned by unrelated organisations the account is a
member of. A single-host homelab box should not hold that.

**Fine-grained PAT.** Rejected, and it was the close call — its blast radius is
identical to the App's, and it is quicker to create. It loses on expiry:
fine-grained tokens are capped at 366 days, and expiry is silent. The controller
would start returning 401, jobs would queue in GitHub indefinitely, and nobody
would notice until someone opened a pull request. A credential that renews
itself is worth ten minutes of setup on a project with no monitoring.

**GitHub App.** Chosen. Installation tokens renew indefinitely, so the
credential never expires unattended, and the permission set is bounded to the
repositories selected at install time. Choosing it costs nothing structurally:
`github.com/actions/scaleset` offers `NewClientWithGitHubApp` alongside
`NewClientWithPersonalAccessToken`, and `golang-jwt/jwt` is a static import of
that library regardless of which one is used. App auth is three config fields
instead of one.

## Consequences

**Short-lived tokens are not the benefit.** The App private key is a permanent
secret on the host; host compromise grants indefinite access until the key is
revoked. The App wins on *scope* and on *never expiring*, and on nothing else.
`Administration: write` still permits deleting a selected repository or making a
private one public, which is the real exfiltration path — so the App is
installed on selected repositories, never on all of them.

**Runner scale sets are repository-scoped.** Every repository that will use
these runners belongs to a personal account, and GitHub has no user-account-level
runner. That means one scale set, one message session and one goroutine per
enabled repository. Moving the repositories into an organisation would collapse
that to one of each and narrow the permission to org `Self-hosted runners:
write`, which cannot touch repository contents or settings at all — recorded as
an available improvement, not a decision taken here.

**pyro holds no second credential.** The controller runs in-process and calls
`Manager.CreateSandbox` directly, which takes an API key *ID*, not a key. It
owns a row in `api_keys` for attribution and audit, whose key value is generated
at startup and never emitted. There is no `pk_` secret to configure, store or
rotate. Because sandbox reads and deletes are filtered by owning key, runner
sandboxes are not visible under the operator's own key — the runner UI is where
they are listed and cancelled.

**Rotation costs in-flight jobs.** Nothing expires in normal operation; the
client library refreshes every derived token itself. The only rotation event is
replacing the private key or reinstalling the App, and that needs a restart —
which destroys every running sandbox, because pyro tears down all active
sandboxes on `SIGTERM`. Rotate during an idle window. Revoking the credential
does not stop a job already running: the runner inside the sandbox holds its own
single-use JIT credential and finishes. New jobs simply stop being picked up.

**Auth failure is reported, not silent.** A revoked or rejected credential
surfaces as a degraded host-readiness check and an error log, consistent with
[ADR 0002](0002-host-readiness-is-reported-never-fatal.md). Without that, a
revoked credential is indistinguishable from an idle queue.
