package data

import (
	"context"
	"math"
	"sync"
	"time"
)

// RateLimiter implements a thread-safe token bucket rate limiter.
type RateLimiter struct {
	mu         sync.Mutex
	capacity   float64
	tokens     float64
	interval   time.Duration
	lastRefill time.Time
}

// Reservation represents a token acquired from a RateLimiter that can either
// be committed for use or canceled and returned to the bucket.
type Reservation struct {
	limiter *RateLimiter
	once    sync.Once
}

// NewRateLimiter constructs a token bucket that allows requestsPerInterval
// requests every interval. The bucket starts full.
func NewRateLimiter(requestsPerInterval int, interval time.Duration) *RateLimiter {
	if requestsPerInterval <= 0 {
		requestsPerInterval = 1
	}
	if interval <= 0 {
		interval = time.Second
	}

	now := time.Now()
	return &RateLimiter{
		capacity:   float64(requestsPerInterval),
		tokens:     float64(requestsPerInterval),
		interval:   interval,
		lastRefill: now,
	}
}

// Wait blocks until a token is available or the context is canceled.
func (r *RateLimiter) Wait(ctx context.Context) error {
	reservation, err := r.Reserve(ctx)
	if err != nil {
		return err
	}
	reservation.Commit()

	return nil
}

// Reserve blocks until a token is available or the context is canceled.
// The returned reservation should be committed once the guarded operation
// proceeds, or canceled to return the token to the bucket.
func (r *RateLimiter) Reserve(ctx context.Context) (*Reservation, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		wait := r.acquireOrWaitDuration(time.Now())
		if wait == 0 {
			return &Reservation{limiter: r}, nil
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

// TryAcquire performs a non-blocking token acquisition attempt.
func (r *RateLimiter) TryAcquire() bool {
	return r.acquireOrWaitDuration(time.Now()) == 0
}

func (r *RateLimiter) acquireOrWaitDuration(now time.Time) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill(now)
	if r.tokens >= 1 {
		r.tokens--
		return 0
	}

	missingTokens := 1 - r.tokens
	waitNanos := math.Ceil(missingTokens * float64(r.interval) / r.capacity)
	if waitNanos < 0 {
		waitNanos = 0
	}

	return time.Duration(waitNanos)
}

func (r *RateLimiter) refill(now time.Time) {
	if !now.After(r.lastRefill) {
		return
	}

	elapsed := now.Sub(r.lastRefill)
	refilled := float64(elapsed) * r.capacity / float64(r.interval)
	if refilled <= 0 {
		return
	}

	r.tokens = min(r.capacity, r.tokens+refilled)
	r.lastRefill = now
}

// Commit marks the reservation as used.
func (r *Reservation) Commit() {
	if r == nil {
		return
	}

	r.once.Do(func() {})
}

// Cancel returns the reserved token to the originating limiter.
func (r *Reservation) Cancel() {
	if r == nil {
		return
	}

	r.once.Do(func() {
		r.limiter.refund(time.Now())
	})
}

func (r *RateLimiter) refund(now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.refill(now)
	r.tokens = min(r.capacity, r.tokens+1)
}
