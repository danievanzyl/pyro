# Surface OCI labels on `GET /images/{name}`

## Parent

danievanzyl/pyro#2

## What to build

Extract `Config.Labels` from the OCI manifest at registration time and surface them on `GET /images/{name}`. Useful for provenance — `org.opencontainers.image.source`, `org.opencontainers.image.revision`, `org.opencontainers.image.created`, etc. Tiny slice; mostly plumbing.

End-to-end coverage:

- `internal/sandbox/registry` — `ConfigExtractor` already pulls `Env`/`WorkingDir`/`User` (slice 02). Extend it to also pull `Labels`.
- `internal/sandbox/images.go` — `ImageInfo` struct gains `Labels map[string]string` with `omitzero` JSON tag. Persist labels alongside the image (e.g. as a small `image-labels.json` next to `rootfs.ext4`, or as part of `image-config.json` — pick whichever requires the smaller diff against slice 02's structure). On `GET`, the manager reads and returns them.
- `internal/api/images.go` — no change beyond the JSON struct gaining the field.
- Dockerfile flow: `docker inspect` already returns `Config.Labels`; `CreateFromDockerfile` populates the same field for parity.

## Acceptance criteria

- [ ] `GET /images/{name}` for a registry-pulled image returns `labels` populated from the OCI manifest's `Config.Labels`.
- [ ] `GET /images/{name}` for a Dockerfile-built image returns `labels` populated from `docker inspect`.
- [ ] An image with no labels returns `labels` omitted from the JSON (`omitzero` honored).
- [ ] Existing images registered before this slice (no labels file on disk) return without labels and don't error.
- [ ] Unit test: manifest with multiple labels → `ImageInfo.Labels` populated correctly.

## Blocked by

- 01-sync-pull-and-register-mvp.md
