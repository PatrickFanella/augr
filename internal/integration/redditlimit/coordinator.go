// Package redditlimit coordinates Reddit feed throttling across every in-process
// consumer. Reddit applies limits at the client/provider level, not per subreddit,
// so a 429 from any feed must pause social sentiment and signal ingestion together.
package redditlimit

import (
	"math/rand/v2"
	"sync"
	"time"
)

const defaultCooldown = 15 * time.Minute

// Coordinator stores provider-wide cooldown and freshness state.
type Coordinator struct {
	mu          sync.Mutex
	cooldownTil time.Time
	lastSuccess time.Time
}

// Default is shared by all Reddit consumers in the application process.
var Default = &Coordinator{}

// Remaining returns the active provider-wide cooldown duration.
func (c *Coordinator) Remaining(now time.Time) time.Duration {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	remaining := c.cooldownTil.Sub(now)
	if remaining <= 0 {
		c.cooldownTil = time.Time{}
		return 0
	}
	return remaining
}

// Start begins or extends the provider-wide cooldown. A small positive jitter
// prevents all workers from resuming on the same boundary after Retry-After.
func (c *Coordinator) Start(now time.Time, duration time.Duration) time.Duration {
	if c == nil {
		return 0
	}
	if duration <= 0 {
		duration = defaultCooldown
	}
	jitter := time.Duration(rand.Int64N(max(int64(duration/10), 1)))
	effective := duration + jitter
	until := now.Add(effective)
	c.mu.Lock()
	if until.After(c.cooldownTil) {
		c.cooldownTil = until
	}
	c.mu.Unlock()
	return effective
}

// MarkSuccess records the last successful Reddit fetch.
func (c *Coordinator) MarkSuccess(at time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	if at.After(c.lastSuccess) {
		c.lastSuccess = at
	}
	c.mu.Unlock()
}

// LastSuccess returns the provider-wide freshness timestamp.
func (c *Coordinator) LastSuccess() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastSuccess
}
