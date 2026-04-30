// Package imagestate owns the in-memory ledger of in-flight and recently
// failed image pulls. The ledger is the source of truth for image status
// while a pull is in progress; once a pull completes successfully, the
// ledger entry is dropped and disk metadata becomes authoritative.
//
// Failed entries linger for FailedTTL (default 1h) so callers can debug
// pull failures via GET /images/{name}.
package imagestate

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// Status enumerates the legal states of an image pull.
type Status string

const (
	StatusPulling    Status = "pulling"
	StatusExtracting Status = "extracting"
	StatusReady      Status = "ready"
	StatusFailed     Status = "failed"
)

// ErrInvalidTransition signals a forbidden status change.
var ErrInvalidTransition = errors.New("invalid status transition")

// ErrUnknown signals the ledger has no entry for a name.
var ErrUnknown = errors.New("no ledger entry for name")

// DefaultFailedTTL is how long a failed entry remains queryable.
const DefaultFailedTTL = time.Hour

// Clock returns the current time. Pluggable for tests.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// RealClock returns a wall-clock implementation.
func RealClock() Clock { return realClock{} }

// PullOp captures the live state of a single pull.
type PullOp struct {
	Name        string
	Source      string
	Status      Status
	Digest      string
	Error       string
	StartedAt   time.Time
	UpdatedAt   time.Time
	CompletedAt time.Time
}

// Clone returns a defensive copy. Callers outside the ledger receive
// clones so they cannot mutate ledger state directly.
func (p *PullOp) Clone() *PullOp {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// Ledger is the in-memory image-pull state map.
type Ledger struct {
	mu        sync.Mutex
	entries   map[string]*PullOp
	clock     Clock
	failedTTL time.Duration
}

// New constructs a Ledger with the supplied clock. A nil clock falls back
// to wall time. Pass DefaultFailedTTL or a custom TTL.
func New(clock Clock, failedTTL time.Duration) *Ledger {
	if clock == nil {
		clock = RealClock()
	}
	if failedTTL <= 0 {
		failedTTL = DefaultFailedTTL
	}
	return &Ledger{
		entries:   make(map[string]*PullOp),
		clock:     clock,
		failedTTL: failedTTL,
	}
}

// Begin starts a pull for name.
//
// If an entry already exists in pulling/extracting, returns the existing
// op with attached=true (single-flight attach — see slice 05). If an
// existing entry is in failed state, it is replaced with a fresh pulling
// entry. Returns attached=false in the fresh-pull and replace-failed
// paths.
func (l *Ledger) Begin(name, source string) (*PullOp, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked()
	if existing, ok := l.entries[name]; ok {
		switch existing.Status {
		case StatusPulling, StatusExtracting:
			return existing.Clone(), true
		}
		// failed (or stale ready) → replace.
	}
	now := l.clock.Now()
	op := &PullOp{
		Name:      name,
		Source:    source,
		Status:    StatusPulling,
		StartedAt: now,
		UpdatedAt: now,
	}
	l.entries[name] = op
	return op.Clone(), false
}

// Update advances the status of a pull. Allowed: pulling→extracting.
// Other transitions return ErrInvalidTransition.
func (l *Ledger) Update(name string, next Status) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.entries[name]
	if !ok {
		return ErrUnknown
	}
	if !canTransition(op.Status, next) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, op.Status, next)
	}
	op.Status = next
	op.UpdatedAt = l.clock.Now()
	return nil
}

// SetDigest stamps the resolved manifest digest onto the entry. Safe to
// call once the puller has resolved; idempotent.
func (l *Ledger) SetDigest(name, digest string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.entries[name]
	if !ok {
		return ErrUnknown
	}
	op.Digest = digest
	return nil
}

// Complete marks the pull successful and drops the ledger entry. Disk
// metadata becomes authoritative. Idempotent.
func (l *Ledger) Complete(name string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.entries[name]
	if !ok {
		return ErrUnknown
	}
	if !canTransition(op.Status, StatusReady) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, op.Status, StatusReady)
	}
	delete(l.entries, name)
	return nil
}

// Fail records a pull failure. The entry remains queryable until
// FailedTTL elapses (or until a fresh Begin replaces it).
func (l *Ledger) Fail(name string, errIn error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	op, ok := l.entries[name]
	if !ok {
		return ErrUnknown
	}
	if !canTransition(op.Status, StatusFailed) {
		return fmt.Errorf("%w: %s → %s", ErrInvalidTransition, op.Status, StatusFailed)
	}
	now := l.clock.Now()
	op.Status = StatusFailed
	op.UpdatedAt = now
	op.CompletedAt = now
	if errIn != nil {
		op.Error = errIn.Error()
	}
	return nil
}

// Get returns a clone of the ledger entry for name, or nil if there is
// no live entry. Expired failed entries are GC'd before lookup.
func (l *Ledger) Get(name string) *PullOp {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked()
	op, ok := l.entries[name]
	if !ok {
		return nil
	}
	return op.Clone()
}

func (l *Ledger) gcLocked() {
	now := l.clock.Now()
	for name, op := range l.entries {
		if op.Status == StatusFailed && now.Sub(op.CompletedAt) > l.failedTTL {
			delete(l.entries, name)
		}
	}
}

// canTransition encodes the allowed state machine.
//
//	pulling    → extracting | failed
//	extracting → ready      | failed
//	ready      → terminal
//	failed     → terminal
func canTransition(from, to Status) bool {
	switch from {
	case StatusPulling:
		return to == StatusExtracting || to == StatusFailed
	case StatusExtracting:
		return to == StatusReady || to == StatusFailed
	}
	return false
}
