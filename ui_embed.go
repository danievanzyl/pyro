package firecrackerlacker

import "embed"

// UIBuild holds the SvelteKit static build output.
// Run `cd ui && bun run build` before `go build` to populate this.
//
//go:embed ui/build/*
var UIBuild embed.FS
