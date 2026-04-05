package data

import "sync"

var (
	globalLimiterMu sync.RWMutex
	globalLimiter   *RateLimiter
)

// SetGlobalLimiter sets the shared rate limiter used by all data providers.
func SetGlobalLimiter(l *RateLimiter) {
	globalLimiterMu.Lock()
	defer globalLimiterMu.Unlock()
	globalLimiter = l
}

// GetGlobalLimiter returns the shared rate limiter, or nil if not set.
func GetGlobalLimiter() *RateLimiter {
	globalLimiterMu.RLock()
	defer globalLimiterMu.RUnlock()
	return globalLimiter
}
