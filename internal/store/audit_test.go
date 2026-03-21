package store

import (
	"context"
	"testing"
)

func TestAuditLogCRUD(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	// Log some entries.
	s.LogAudit(ctx, &AuditEntry{
		Action:    AuditSandboxCreated,
		APIKeyID:  "key-1",
		SandboxID: "sb-1",
		Detail:    "image=default ttl=3600",
	})
	s.LogAudit(ctx, &AuditEntry{
		Action:    AuditSandboxExec,
		APIKeyID:  "key-1",
		SandboxID: "sb-1",
		Detail:    "cmd=echo hello",
	})
	s.LogAudit(ctx, &AuditEntry{
		Action:    AuditSandboxDestroyed,
		APIKeyID:  "key-1",
		SandboxID: "sb-1",
		Detail:    "reason=ttl_expired",
	})

	// List all.
	entries, err := s.ListAuditEntries(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("len = %d, want 3", len(entries))
	}
	// Newest first.
	if entries[0].Action != AuditSandboxDestroyed {
		t.Errorf("first entry = %q, want %q", entries[0].Action, AuditSandboxDestroyed)
	}

	// List by sandbox.
	sbEntries, err := s.ListAuditBySandbox(ctx, "sb-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(sbEntries) != 3 {
		t.Errorf("sandbox entries = %d, want 3", len(sbEntries))
	}

	// List by nonexistent sandbox.
	empty, err := s.ListAuditBySandbox(ctx, "nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Errorf("expected 0 entries, got %d", len(empty))
	}
}

func TestAuditLogLimit(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()

	for range 5 {
		s.LogAudit(ctx, &AuditEntry{Action: AuditSandboxCreated, APIKeyID: "k"})
	}

	entries, err := s.ListAuditEntries(ctx, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Errorf("len = %d, want 3", len(entries))
	}
}
