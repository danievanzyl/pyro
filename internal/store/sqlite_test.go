package store

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := New(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCreateAndGetSandbox(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	sb := &Sandbox{
		ID:         "sb-1",
		APIKeyID:   "key-1",
		State:      StateCreating,
		Image:      "default",
		PID:        1234,
		SocketPath: "/tmp/fc-sb-1.sock",
		VsockCID:   3,
		TapDevice:  "tap-sb1",
		IP:         "172.16.0.2",
		CreatedAt:  time.Now().UTC().Truncate(time.Second),
		ExpiresAt:  time.Now().UTC().Add(time.Hour).Truncate(time.Second),
		StateDir:   "/var/lib/fc/sb-1",
	}

	if err := s.CreateSandbox(ctx, sb); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetSandbox(ctx, "sb-1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected sandbox, got nil")
	}
	if got.ID != "sb-1" {
		t.Errorf("ID = %q, want %q", got.ID, "sb-1")
	}
	if got.State != StateCreating {
		t.Errorf("State = %q, want %q", got.State, StateCreating)
	}
	if got.PID != 1234 {
		t.Errorf("PID = %d, want 1234", got.PID)
	}
}

func TestGetSandboxNotFound(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	got, err := s.GetSandbox(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestListSandboxes(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	for i := range 3 {
		sb := &Sandbox{
			ID:        "sb-" + string(rune('a'+i)),
			APIKeyID:  "key-1",
			State:     StateRunning,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		}
		s.CreateSandbox(ctx, sb)
	}
	// Different key.
	s.CreateSandbox(ctx, &Sandbox{
		ID:        "sb-other",
		APIKeyID:  "key-2",
		State:     StateRunning,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	// Destroyed sandbox should not appear.
	s.CreateSandbox(ctx, &Sandbox{
		ID:        "sb-dead",
		APIKeyID:  "key-1",
		State:     StateDestroyed,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})

	list, err := s.ListSandboxes(ctx, "key-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("len = %d, want 3", len(list))
	}
}

func TestGetExpiredSandboxes(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	// Expired + running.
	s.CreateSandbox(ctx, &Sandbox{
		ID:        "sb-expired",
		APIKeyID:  "key-1",
		State:     StateRunning,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})
	// Not expired.
	s.CreateSandbox(ctx, &Sandbox{
		ID:        "sb-alive",
		APIKeyID:  "key-1",
		State:     StateRunning,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	// Expired but already destroyed.
	s.CreateSandbox(ctx, &Sandbox{
		ID:        "sb-already-dead",
		APIKeyID:  "key-1",
		State:     StateDestroyed,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	})

	expired, err := s.GetExpiredSandboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 {
		t.Errorf("len = %d, want 1", len(expired))
	}
	if len(expired) > 0 && expired[0].ID != "sb-expired" {
		t.Errorf("ID = %q, want %q", expired[0].ID, "sb-expired")
	}
}

func TestUpdateSandboxState(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	s.CreateSandbox(ctx, &Sandbox{
		ID:        "sb-1",
		APIKeyID:  "key-1",
		State:     StateCreating,
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})

	if err := s.UpdateSandboxState(ctx, "sb-1", StateRunning); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetSandbox(ctx, "sb-1")
	if got.State != StateRunning {
		t.Errorf("State = %q, want %q", got.State, StateRunning)
	}
}

func TestUpdateSandboxStateNotFound(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	err := s.UpdateSandboxState(ctx, "nonexistent", StateRunning)
	if err == nil {
		t.Fatal("expected error for nonexistent sandbox")
	}
}

func TestAPIKeyLifecycle(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	ak := &APIKey{
		ID:        "ak-1",
		Key:       "pk_testkey123",
		Name:      "test",
		CreatedAt: time.Now().UTC(),
	}
	if err := s.CreateAPIKey(ctx, ak); err != nil {
		t.Fatal(err)
	}

	got, err := s.ValidateAPIKey(ctx, "pk_testkey123")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected API key, got nil")
	}
	if got.Name != "test" {
		t.Errorf("Name = %q, want %q", got.Name, "test")
	}
}

func TestAPIKeyNotFound(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	got, err := s.ValidateAPIKey(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for nonexistent key")
	}
}

func TestAPIKeyExpired(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	ak := &APIKey{
		ID:        "ak-expired",
		Key:       "pk_expired123",
		Name:      "expired",
		CreatedAt: time.Now().UTC(),
		ExpiresAt: time.Now().UTC().Add(-time.Hour),
	}
	if err := s.CreateAPIKey(ctx, ak); err != nil {
		t.Fatal(err)
	}

	got, err := s.ValidateAPIKey(ctx, "pk_expired123")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Errorf("expected nil for expired key, got %+v", got)
	}
}

func TestSandboxIsExpired(t *testing.T) {
	sb := &Sandbox{ExpiresAt: time.Now().Add(-time.Second)}
	if !sb.IsExpired() {
		t.Error("expected expired")
	}

	sb2 := &Sandbox{ExpiresAt: time.Now().Add(time.Hour)}
	if sb2.IsExpired() {
		t.Error("expected not expired")
	}
}

func TestSandboxRemainingTTL(t *testing.T) {
	sb := &Sandbox{ExpiresAt: time.Now().Add(-time.Second)}
	if sb.RemainingTTL() != 0 {
		t.Errorf("RemainingTTL = %v, want 0", sb.RemainingTTL())
	}

	sb2 := &Sandbox{ExpiresAt: time.Now().Add(time.Hour)}
	if sb2.RemainingTTL() < 59*time.Minute {
		t.Errorf("RemainingTTL = %v, want ~1h", sb2.RemainingTTL())
	}
}

func TestGetAllActiveSandboxes(t *testing.T) {
	s := testStore(t)
	ctx := t.Context()

	s.CreateSandbox(ctx, &Sandbox{ID: "sb-1", APIKeyID: "k", State: StateRunning, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)})
	s.CreateSandbox(ctx, &Sandbox{ID: "sb-2", APIKeyID: "k", State: StateCreating, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)})
	s.CreateSandbox(ctx, &Sandbox{ID: "sb-3", APIKeyID: "k", State: StateDestroyed, CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour)})

	active, err := s.GetAllActiveSandboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 2 {
		t.Errorf("len = %d, want 2", len(active))
	}
}

// Ensure temp dir cleanup works.
func TestDatabaseFileCreated(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("database file not created")
	}
}
