// Package db provides SQLite database connectivity and operations.
package db

import (
	"fmt"
	"sync"
	"time"
)

// RateLimiter implements a sliding window rate limiter for audit operations.
type RateLimiter struct {
	mu      sync.RWMutex
	windows map[string][]time.Time // roadmapName -> list of request timestamps
	limit   int                    // max requests per window
	window  time.Duration          // time window
}

// NewRateLimiter creates a new rate limiter with the specified limit and window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		windows: make(map[string][]time.Time),
		limit:   limit,
		window:  window,
	}
}

// Allow checks if a request is allowed for the given key (roadmap name).
// Returns true if the request is allowed, false if rate limited.
func (rl *RateLimiter) Allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// Get existing timestamps for this key
	timestamps := rl.windows[key]

	// Remove timestamps outside the window
	var valid []time.Time
	for _, ts := range timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}

	// Check if we've hit the limit
	if len(valid) >= rl.limit {
		rl.windows[key] = valid // Update with cleaned list
		return false
	}

	// Add current timestamp and update
	valid = append(valid, now)
	rl.windows[key] = valid
	return true
}

// Cleanup removes old entries for keys that haven't been used recently.
// Should be called periodically to prevent memory leaks.
func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-rl.window)
	for key, timestamps := range rl.windows {
		var valid []time.Time
		for _, ts := range timestamps {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}
		if len(valid) == 0 {
			delete(rl.windows, key)
		} else {
			rl.windows[key] = valid
		}
	}
}

// auditRateLimiter is the global rate limiter for audit operations.
// Limit: 100 entries per minute per roadmap.
var auditRateLimiter = NewRateLimiter(100, time.Minute)

// ErrRateLimitExceeded is returned when the rate limit is exceeded.
var ErrRateLimitExceeded = fmt.Errorf("rate limit exceeded: maximum %d audit entries per %v", 100, time.Minute)

// checkAuditRateLimit checks if an audit entry can be logged for the given roadmap.
// Returns nil if allowed, ErrRateLimitExceeded if rate limited.
func checkAuditRateLimit(roadmapName string) error {
	if !auditRateLimiter.Allow(roadmapName) {
		return fmt.Errorf("%w for roadmap %q", ErrRateLimitExceeded, roadmapName)
	}
	return nil
}

// IsRateLimitError checks if an error is a rate limit error.
func IsRateLimitError(err error) bool {
	return err == ErrRateLimitExceeded
}
