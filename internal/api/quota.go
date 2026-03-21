package api

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/danievanzyl/firecrackerlacker/internal/store"
)

// QuotaConfig defines resource limits per API key.
type QuotaConfig struct {
	// MaxConcurrentSandboxes per API key. 0 = unlimited.
	MaxConcurrentSandboxes int

	// MaxTTL is the maximum TTL an API key can request (seconds).
	MaxTTL int

	// RateLimit is max sandbox creations per minute per API key. 0 = unlimited.
	RateLimit int
}

// DefaultQuota is the default quota for new API keys.
var DefaultQuota = QuotaConfig{
	MaxConcurrentSandboxes: 10,
	MaxTTL:                 86400, // 24 hours
	RateLimit:              30,    // 30 creates/min
}

// QuotaEnforcer checks and enforces per-API-key resource quotas.
type QuotaEnforcer struct {
	st     *store.Store
	config QuotaConfig

	mu          sync.Mutex
	rateCounts  map[string]*rateWindow // api_key_id → rate window
}

type rateWindow struct {
	count     int
	windowEnd time.Time
}

// NewQuotaEnforcer creates a quota enforcer.
func NewQuotaEnforcer(st *store.Store, cfg QuotaConfig) *QuotaEnforcer {
	return &QuotaEnforcer{
		st:         st,
		config:     cfg,
		rateCounts: make(map[string]*rateWindow),
	}
}

// CheckCreateQuota validates that an API key can create a new sandbox.
func (qe *QuotaEnforcer) CheckCreateQuota(ctx context.Context, apiKeyID string, requestedTTL int) error {
	// Check TTL limit.
	if qe.config.MaxTTL > 0 && requestedTTL > qe.config.MaxTTL {
		return fmt.Errorf("requested TTL %d exceeds maximum %d seconds", requestedTTL, qe.config.MaxTTL)
	}

	// Check concurrent sandbox limit.
	if qe.config.MaxConcurrentSandboxes > 0 {
		sandboxes, err := qe.st.ListSandboxes(ctx, apiKeyID)
		if err != nil {
			return fmt.Errorf("check quota: %w", err)
		}
		if len(sandboxes) >= qe.config.MaxConcurrentSandboxes {
			return fmt.Errorf("concurrent sandbox limit reached: %d/%d",
				len(sandboxes), qe.config.MaxConcurrentSandboxes)
		}
	}

	// Check rate limit.
	if qe.config.RateLimit > 0 {
		if !qe.checkRate(apiKeyID) {
			return fmt.Errorf("rate limit exceeded: max %d creates/minute", qe.config.RateLimit)
		}
	}

	return nil
}

// checkRate implements a simple fixed-window rate limiter.
func (qe *QuotaEnforcer) checkRate(apiKeyID string) bool {
	qe.mu.Lock()
	defer qe.mu.Unlock()

	now := time.Now()
	window, ok := qe.rateCounts[apiKeyID]

	if !ok || now.After(window.windowEnd) {
		// New window.
		qe.rateCounts[apiKeyID] = &rateWindow{
			count:     1,
			windowEnd: now.Add(time.Minute),
		}
		return true
	}

	if window.count >= qe.config.RateLimit {
		return false
	}

	window.count++
	return true
}
