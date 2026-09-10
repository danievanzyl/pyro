package pyro

import "embed"

// UIBuild holds the SvelteKit static build output.
// Run `cd ui && bun run build` before `go build` to populate this.
//
//go:embed ui/build/*
var UIBuild embed.FS

// ServiceUnit is the systemd unit pyro setup installs. Go's embed directive
// cannot cross a package directory boundary, so this lives at the module
// root next to deploy/, not in cmd/pyro where it's consumed.
//
//go:embed deploy/pyro.service
var ServiceUnit []byte
