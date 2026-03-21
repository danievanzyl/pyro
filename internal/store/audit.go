package store

import (
	"context"
	"time"
)

// AuditAction represents what happened.
type AuditAction string

const (
	AuditSandboxCreated   AuditAction = "sandbox.created"
	AuditSandboxDestroyed AuditAction = "sandbox.destroyed"
	AuditSandboxExec      AuditAction = "sandbox.exec"
	AuditSandboxFileWrite AuditAction = "sandbox.file.write"
	AuditSandboxFileRead  AuditAction = "sandbox.file.read"
	AuditSandboxExpired   AuditAction = "sandbox.expired"
	AuditAPIKeyCreated    AuditAction = "api_key.created"
)

// AuditEntry is a single audit log record.
type AuditEntry struct {
	ID        int64       `json:"id"`
	Timestamp time.Time   `json:"timestamp"`
	Action    AuditAction `json:"action"`
	APIKeyID  string      `json:"api_key_id"`
	SandboxID string      `json:"sandbox_id,omitempty"`
	Detail    string      `json:"detail,omitempty"`
}

// migrateAudit adds the audit_log table.
func (s *Store) migrateAudit() error {
	schema := `
	CREATE TABLE IF NOT EXISTS audit_log (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		timestamp  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		action     TEXT NOT NULL,
		api_key_id TEXT NOT NULL DEFAULT '',
		sandbox_id TEXT NOT NULL DEFAULT '',
		detail     TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX IF NOT EXISTS idx_audit_timestamp ON audit_log(timestamp);
	CREATE INDEX IF NOT EXISTS idx_audit_sandbox_id ON audit_log(sandbox_id);
	CREATE INDEX IF NOT EXISTS idx_audit_api_key_id ON audit_log(api_key_id);
	`
	_, err := s.db.Exec(schema)
	return err
}

// LogAudit writes an audit entry.
func (s *Store) LogAudit(ctx context.Context, entry *AuditEntry) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO audit_log (timestamp, action, api_key_id, sandbox_id, detail)
		 VALUES (?, ?, ?, ?, ?)`,
		time.Now().UTC(), entry.Action, entry.APIKeyID, entry.SandboxID, entry.Detail)
	return err
}

// ListAuditEntries returns recent audit entries, newest first.
func (s *Store) ListAuditEntries(ctx context.Context, limit int) ([]*AuditEntry, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, timestamp, action, api_key_id, sandbox_id, detail
		 FROM audit_log ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Action, &e.APIKeyID, &e.SandboxID, &e.Detail); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ListAuditBySandbox returns audit entries for a specific sandbox.
func (s *Store) ListAuditBySandbox(ctx context.Context, sandboxID string) ([]*AuditEntry, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, timestamp, action, api_key_id, sandbox_id, detail
		 FROM audit_log WHERE sandbox_id = ? ORDER BY id DESC`, sandboxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*AuditEntry
	for rows.Next() {
		e := &AuditEntry{}
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Action, &e.APIKeyID, &e.SandboxID, &e.Detail); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
