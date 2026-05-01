package sandbox

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stageSnap creates a fake snapshot dir under root and returns the
// snapshot record matching it. The dir contents simulate the on-disk
// shape (mem + vmstate files) so Invalidate's removal is verifiable.
func stageSnap(t *testing.T, root, image string) *snapshot {
	t.Helper()
	id := "snap-" + image + "-" + t.Name() + "-" + time.Now().Format("150405.000000000")
	// Replace any path-unsafe characters in the test name.
	id = filepath.Base(id)
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	mem := filepath.Join(dir, "mem")
	snap := filepath.Join(dir, "vmstate")
	if err := os.WriteFile(mem, []byte("mem"), 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snap, []byte("snap"), 0o640); err != nil {
		t.Fatal(err)
	}
	return &snapshot{
		ID:        id,
		Image:     image,
		MemFile:   mem,
		SnapFile:  snap,
		CreatedAt: time.Now(),
	}
}

func newTestPool(t *testing.T) *Pool {
	t.Helper()
	dir := t.TempDir()
	p, err := NewPool(PoolConfig{SnapshotDir: dir, TargetSize: 0}, nil,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// TestPool_Invalidate_DrainsAndRemovesFiles seeds two ready snapshots
// for image-a plus one for image-b. Invalidate("image-a") empties
// only image-a's slice and removes only its on-disk dirs.
func TestPool_Invalidate_DrainsAndRemovesFiles(t *testing.T) {
	p := newTestPool(t)
	root := p.cfg.SnapshotDir

	a1 := stageSnap(t, root, "image-a")
	a2 := stageSnap(t, root, "image-a")
	b1 := stageSnap(t, root, "image-b")

	p.mu.Lock()
	p.ready["image-a"] = []*snapshot{a1, a2}
	p.ready["image-b"] = []*snapshot{b1}
	p.mu.Unlock()

	p.Invalidate("image-a")

	if got := p.Available("image-a"); got != 0 {
		t.Errorf("image-a ready = %d want 0", got)
	}
	if got := p.Available("image-b"); got != 1 {
		t.Errorf("image-b ready = %d want 1", got)
	}
	for _, s := range []*snapshot{a1, a2} {
		if _, err := os.Stat(filepath.Join(root, s.ID)); !os.IsNotExist(err) {
			t.Errorf("snapshot dir %s should be removed (err=%v)", s.ID, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, b1.ID)); err != nil {
		t.Errorf("image-b dir should remain: %v", err)
	}
}

// TestPool_Invalidate_IdempotentUnknown verifies invalidating an image
// the pool has never heard of is a no-op (no panic, no error).
func TestPool_Invalidate_IdempotentUnknown(t *testing.T) {
	p := newTestPool(t)
	p.Invalidate("never-seen") // must not panic
	if got := p.Available("never-seen"); got != 0 {
		t.Errorf("unknown image available = %d want 0", got)
	}
}

// TestPool_Invalidate_RepeatCallNoop confirms a second invalidate after
// the slice is already drained is also a no-op.
func TestPool_Invalidate_RepeatCallNoop(t *testing.T) {
	p := newTestPool(t)
	a := stageSnap(t, p.cfg.SnapshotDir, "image-a")
	p.mu.Lock()
	p.ready["image-a"] = []*snapshot{a}
	p.mu.Unlock()

	p.Invalidate("image-a")
	p.Invalidate("image-a") // second call: nothing left
	if got := p.Available("image-a"); got != 0 {
		t.Errorf("image-a ready = %d want 0 after second invalidate", got)
	}
}
