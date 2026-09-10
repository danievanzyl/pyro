// Package ui embeds the SvelteKit static build output.
// Run `cd ui && bun run build` before `go build` to populate this.
package ui

import "embed"

//go:embed build/*
var Build embed.FS
