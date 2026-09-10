package ui

import (
	"io/fs"
	"testing"
)

// Guards the subdir cmd/server passes to fs.Sub(ui.Build, ...): the embed
// root is "build", not "ui/build" — a leftover root-package path would
// fail this the same way it broke serving before the split.
func TestBuildSubDirResolves(t *testing.T) {
	if _, err := fs.ReadDir(Build, "build"); err != nil {
		t.Skipf("ui/build not present (run `bun run build` in ui/): %v", err)
	}

	sub, err := fs.Sub(Build, "build")
	if err != nil {
		t.Fatalf("fs.Sub(Build, %q): %v", "build", err)
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		t.Fatalf("expected index.html at the root of the sub filesystem: %v", err)
	}
}
