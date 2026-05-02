# Image config plumbing (ENV / WORKDIR / USER)

## Parent

danievanzyl/pyro#2

## What to build

Honor the OCI image's `Config.Env`, `Config.WorkingDir`, and `Config.User` as defaults for sandbox exec calls. Without this slice, registering `python:3.12` works but `pyrovm exec python` fails because `PATH` doesn't include `/usr/local/bin`.

At image registration time (both registry-pull and Dockerfile flows), write `/etc/pyro/image-config.json` into the rootfs containing `{env, workdir, user}`. pyro-agent loads this file at boot, holds the values in memory, and applies them as defaults for every exec request received over vsock. Per-request `env` and `cwd` from the API merge over (and override) the image defaults.

End-to-end coverage:

- `internal/sandbox/registry` — `ConfigExtractor` projects the OCI manifest's `Config` block into a flat `ImageConfig` struct.
- `internal/sandbox/imageops` — new `ConfigWriter` writes `image-config.json` into a mounted rootfs.
- `internal/sandbox/images.go` — `CreateFromRegistry` calls `ConfigWriter` after layer extraction. `CreateFromDockerfile` is updated to extract the same fields from `docker inspect` output and write the same JSON file (parity backfill).
- `cmd/agent/main.go` — pyro-agent loads `/etc/pyro/image-config.json` at startup with a graceful default if the file is missing (preserves backward compat for any pre-existing images). Exec handler merges image defaults with per-request env/cwd: image env is the base, per-request env entries override on key collision; per-request cwd wins if set, else image workdir.
- USER handling: if `Config.User` is set and resolves to a name (not a numeric UID) that isn't present in the rootfs `/etc/passwd`, pyro-agent logs a warning and runs the exec as root rather than failing. Numeric UIDs are applied directly via `os/exec`'s `SysProcAttr.Credential`.

## Acceptance criteria

- [ ] After registering `python:3.12`, executing `python --version` (no path) succeeds because `PATH` from the image is set.
- [ ] After registering an image with `WORKDIR /app`, `pwd` inside an exec returns `/app` when no per-request cwd is given.
- [ ] Per-request `env` overrides image `Env` on key conflict; non-conflicting image env vars are still present.
- [ ] Per-request `cwd` overrides image `WorkingDir` when set.
- [ ] Image `USER` set to a numeric UID is applied to exec processes.
- [ ] Image `USER` set to a name not present in `/etc/passwd` produces a warning log and the exec runs as root (no crash).
- [ ] `CreateFromDockerfile` also produces `/etc/pyro/image-config.json` from `docker inspect` output. A sandbox booted from a Dockerfile-built image with `ENV` directives sees those env vars.
- [ ] Existing images without the config file boot normally; agent uses empty defaults.
- [ ] Unit tests for `ConfigExtractor` (registry manifest → flat struct) and `ConfigWriter` (writes JSON to a tmpdir). Both pure-Go, runnable on macOS.
- [ ] Agent merge logic covered by unit tests — image defaults, per-request override, cwd precedence, USER fallback.

## Blocked by

- 01-sync-pull-and-register-mvp.md
