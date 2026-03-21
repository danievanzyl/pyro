package api

import (
	"context"
	"testing"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
)

func TestQuotaEnforcerTTLLimit(t *testing.T) {
	st := setupTestStore(t)
	qe := NewQuotaEnforcer(st, QuotaConfig{MaxTTL: 3600})

	err := qe.CheckCreateQuota(context.Background(), "test-key-id", 7200)
	if err == nil {
		t.Fatal("expected TTL quota error")
	}

	err = qe.CheckCreateQuota(context.Background(), "test-key-id", 3600)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestQuotaEnforcerConcurrentLimit(t *testing.T) {
	st := setupTestStore(t)
	ctx := context.Background()

	qe := NewQuotaEnforcer(st, QuotaConfig{MaxConcurrentSandboxes: 2, MaxTTL: 86400})

	// Create 2 sandboxes for this key.
	for i := range 2 {
		st.CreateSandbox(ctx, &store.Sandbox{
			ID:        "sb-" + string(rune('a'+i)),
			APIKeyID:  "test-key-id",
			State:     store.StateRunning,
			CreatedAt: time.Now().UTC(),
			ExpiresAt: time.Now().UTC().Add(time.Hour),
		})
	}

	err := qe.CheckCreateQuota(ctx, "test-key-id", 3600)
	if err == nil {
		t.Fatal("expected concurrent sandbox quota error")
	}
}

func TestQuotaEnforcerRateLimit(t *testing.T) {
	st := setupTestStore(t)
	ctx := context.Background()

	qe := NewQuotaEnforcer(st, QuotaConfig{RateLimit: 3, MaxTTL: 86400})

	for range 3 {
		err := qe.CheckCreateQuota(ctx, "test-key-id", 100)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	err := qe.CheckCreateQuota(ctx, "test-key-id", 100)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
}
