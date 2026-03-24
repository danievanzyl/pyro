package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Store provides data access for sandboxes and API keys.
type Store struct {
	db *sql.DB
}

// New creates a Store backed by SQLite at the given path.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1) // SQLite single-writer

	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.migrateAudit(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate audit: %w", err)
	}
	return s, nil
}

func (s *Store) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sandboxes (
		id          TEXT PRIMARY KEY,
		api_key_id  TEXT NOT NULL,
		state       TEXT NOT NULL DEFAULT 'creating',
		image       TEXT NOT NULL DEFAULT 'default',
		pid         INTEGER NOT NULL DEFAULT 0,
		socket_path TEXT NOT NULL DEFAULT '',
		vsock_cid   INTEGER NOT NULL DEFAULT 0,
		tap_device  TEXT NOT NULL DEFAULT '',
		ip          TEXT NOT NULL DEFAULT '',
		created_at  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at  DATETIME NOT NULL,
		state_dir   TEXT NOT NULL DEFAULT ''
	);

	CREATE INDEX IF NOT EXISTS idx_sandboxes_expires_at ON sandboxes(expires_at);
	CREATE INDEX IF NOT EXISTS idx_sandboxes_api_key_id ON sandboxes(api_key_id);
	CREATE INDEX IF NOT EXISTS idx_sandboxes_state ON sandboxes(state);

	CREATE TABLE IF NOT EXISTS api_keys (
		id         TEXT PRIMARY KEY,
		key        TEXT NOT NULL UNIQUE,
		name       TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		expires_at DATETIME
	);

	CREATE INDEX IF NOT EXISTS idx_api_keys_key ON api_keys(key);
	`
	_, err := s.db.Exec(schema)
	return err
}

// Close closes the database connection.
func (s *Store) Close() error {
	return s.db.Close()
}

// CreateSandbox inserts a new sandbox record.
func (s *Store) CreateSandbox(ctx context.Context, sb *Sandbox) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sandboxes (id, api_key_id, state, image, pid, socket_path, vsock_cid, tap_device, ip, created_at, expires_at, state_dir)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sb.ID, sb.APIKeyID, sb.State, sb.Image, sb.PID, sb.SocketPath,
		sb.VsockCID, sb.TapDevice, sb.IP, sb.CreatedAt, sb.ExpiresAt, sb.StateDir,
	)
	return err
}

// GetSandbox retrieves a sandbox by ID.
func (s *Store) GetSandbox(ctx context.Context, id string) (*Sandbox, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, api_key_id, state, image, pid, socket_path, vsock_cid, tap_device, ip, created_at, expires_at, state_dir
		 FROM sandboxes WHERE id = ?`, id)
	return scanSandbox(row)
}

// ListSandboxes returns active sandboxes for a given API key.
func (s *Store) ListSandboxes(ctx context.Context, apiKeyID string) ([]*Sandbox, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, api_key_id, state, image, pid, socket_path, vsock_cid, tap_device, ip, created_at, expires_at, state_dir
		 FROM sandboxes WHERE api_key_id = ? AND state != 'destroyed'
		 ORDER BY created_at DESC`, apiKeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sandboxes []*Sandbox
	for rows.Next() {
		sb, err := scanSandboxRows(rows)
		if err != nil {
			return nil, err
		}
		sandboxes = append(sandboxes, sb)
	}
	return sandboxes, rows.Err()
}

// GetExpiredSandboxes returns sandboxes past their TTL that are still running.
func (s *Store) GetExpiredSandboxes(ctx context.Context) ([]*Sandbox, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, api_key_id, state, image, pid, socket_path, vsock_cid, tap_device, ip, created_at, expires_at, state_dir
		 FROM sandboxes WHERE expires_at < ? AND state IN ('creating', 'running')`,
		time.Now().UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sandboxes []*Sandbox
	for rows.Next() {
		sb, err := scanSandboxRows(rows)
		if err != nil {
			return nil, err
		}
		sandboxes = append(sandboxes, sb)
	}
	return sandboxes, rows.Err()
}

// UpdateSandboxState changes the state of a sandbox.
func (s *Store) UpdateSandboxState(ctx context.Context, id string, state SandboxState) error {
	result, err := s.db.ExecContext(ctx,
		`UPDATE sandboxes SET state = ? WHERE id = ?`, state, id)
	if err != nil {
		return err
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("sandbox %s not found", id)
	}
	return nil
}

// UpdateSandboxPID updates the PID and socket path after VM starts.
func (s *Store) UpdateSandboxPID(ctx context.Context, id string, pid int, socketPath string) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE sandboxes SET pid = ?, socket_path = ?, state = 'running' WHERE id = ?`,
		pid, socketPath, id)
	return err
}

// DeleteSandbox removes a sandbox record.
func (s *Store) DeleteSandbox(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM sandboxes WHERE id = ?`, id)
	return err
}

// GetAllActiveSandboxes returns all non-destroyed sandboxes (for startup reconciliation).
func (s *Store) GetAllActiveSandboxes(ctx context.Context) ([]*Sandbox, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, api_key_id, state, image, pid, socket_path, vsock_cid, tap_device, ip, created_at, expires_at, state_dir
		 FROM sandboxes WHERE state != 'destroyed'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sandboxes []*Sandbox
	for rows.Next() {
		sb, err := scanSandboxRows(rows)
		if err != nil {
			return nil, err
		}
		sandboxes = append(sandboxes, sb)
	}
	return sandboxes, rows.Err()
}

// ValidateAPIKey checks if a key exists and is not expired.
func (s *Store) ValidateAPIKey(ctx context.Context, key string) (*APIKey, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, key, name, created_at, expires_at FROM api_keys WHERE key = ?`, key)

	ak := &APIKey{}
	var expiresAt sql.NullTime
	err := row.Scan(&ak.ID, &ak.Key, &ak.Name, &ak.CreatedAt, &expiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if expiresAt.Valid {
		ak.ExpiresAt = expiresAt.Time
		if time.Now().After(ak.ExpiresAt) {
			return nil, nil // expired
		}
	}
	return ak, nil
}

// CreateAPIKey inserts a new API key.
func (s *Store) CreateAPIKey(ctx context.Context, ak *APIKey) error {
	var expiresAt *time.Time
	if !ak.ExpiresAt.IsZero() {
		expiresAt = &ak.ExpiresAt
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO api_keys (id, key, name, created_at, expires_at)
		 VALUES (?, ?, ?, ?, ?)`,
		ak.ID, ak.Key, ak.Name, ak.CreatedAt, expiresAt)
	return err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSandbox(row *sql.Row) (*Sandbox, error) {
	sb := &Sandbox{}
	err := row.Scan(
		&sb.ID, &sb.APIKeyID, &sb.State, &sb.Image, &sb.PID,
		&sb.SocketPath, &sb.VsockCID, &sb.TapDevice, &sb.IP,
		&sb.CreatedAt, &sb.ExpiresAt, &sb.StateDir,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return sb, err
}

func scanSandboxRows(rows *sql.Rows) (*Sandbox, error) {
	sb := &Sandbox{}
	err := rows.Scan(
		&sb.ID, &sb.APIKeyID, &sb.State, &sb.Image, &sb.PID,
		&sb.SocketPath, &sb.VsockCID, &sb.TapDevice, &sb.IP,
		&sb.CreatedAt, &sb.ExpiresAt, &sb.StateDir,
	)
	return sb, err
}
