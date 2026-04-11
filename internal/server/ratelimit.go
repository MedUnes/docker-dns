package server

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const rateLimiterCleanupInterval = 5 * time.Minute
const rateLimiterIdleTimeout = 10 * time.Minute

type limiterEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimiter is a per-IP token-bucket rate limiter.
// Idle entries (no activity for rateLimiterIdleTimeout) are removed
// by a background goroutine to prevent unbounded memory growth.
type RateLimiter struct {
	mu      sync.Mutex
	entries map[string]*limiterEntry
	limit   rate.Limit
	burst   int
	log     *slog.Logger
}

func newRateLimiter(qps float64, burst int, log *slog.Logger) *RateLimiter {
	return &RateLimiter{
		entries: make(map[string]*limiterEntry),
		limit:   rate.Limit(qps),
		burst:   burst,
		log:     log,
	}
}

// Allow returns true if the client IP has not exceeded its quota.
func (r *RateLimiter) Allow(ip string) bool {
	r.mu.Lock()
	e, ok := r.entries[ip]
	if !ok {
		e = &limiterEntry{limiter: rate.NewLimiter(r.limit, r.burst)}
		r.entries[ip] = e
	}
	e.lastSeen = time.Now()
	ok = e.limiter.Allow()
	r.mu.Unlock()
	return ok
}

// cleanupLoop removes entries that have been idle longer than rateLimiterIdleTimeout.
// It runs until ctx is cancelled.
func (r *RateLimiter) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(rateLimiterCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.cleanup()
		}
	}
}

func (r *RateLimiter) cleanup() {
	cutoff := time.Now().Add(-rateLimiterIdleTimeout)
	r.mu.Lock()
	for ip, e := range r.entries {
		if e.lastSeen.Before(cutoff) {
			delete(r.entries, ip)
		}
	}
	r.mu.Unlock()
	r.log.Debug("rate limiter cleanup complete", "remaining", len(r.entries))
}
