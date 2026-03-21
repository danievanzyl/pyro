package sandbox

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
)

// Reaper periodically destroys sandboxes that have exceeded their TTL.
//
//	┌──────────┐    tick     ┌──────────────┐    expired?   ┌───────────┐
//	│  Sleep   │───────────▶│ Query expired │──── yes ─────▶│ Destroy   │
//	│ interval │            │  sandboxes    │               │  sandbox  │
//	└──────────┘            └──────────────┘               └───────────┘
//	     ▲                        │ no                           │
//	     └────────────────────────┘                              │
//	     └───────────────────────────────────────────────────────┘
type Reaper struct {
	manager  *Manager
	interval time.Duration
	log      *slog.Logger
}

// NewReaper creates a TTL reaper.
func NewReaper(manager *Manager, interval time.Duration, log *slog.Logger) *Reaper {
	return &Reaper{
		manager:  manager,
		interval: interval,
		log:      log,
	}
}

// Run starts the reaper loop. Blocks until ctx is cancelled.
func (r *Reaper) Run(ctx context.Context) {
	r.log.Info("reaper started", "interval", r.interval)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			r.log.Info("reaper stopped")
			return
		case <-ticker.C:
			r.tick(ctx)
		}
	}
}

func (r *Reaper) tick(ctx context.Context) {
	expired, err := r.manager.store.GetExpiredSandboxes(ctx)
	if err != nil {
		r.log.Error("reaper query failed", "err", err)
		return
	}

	for _, sb := range expired {
		overdue := time.Since(sb.ExpiresAt).Round(time.Second)
		r.log.Info("reaping expired sandbox", "id", sb.ID,
			"expired_at", sb.ExpiresAt, "overdue", overdue)

		if err := r.manager.DestroySandbox(ctx, sb.ID); err != nil {
			r.log.Error("reaper destroy failed", "id", sb.ID, "err", err)
			continue
		}

		r.manager.store.LogAudit(ctx, &store.AuditEntry{
			Action:    store.AuditSandboxExpired,
			APIKeyID:  sb.APIKeyID,
			SandboxID: sb.ID,
			Detail:    fmt.Sprintf("overdue=%s", overdue),
		})
	}
}
