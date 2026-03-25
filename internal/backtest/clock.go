package backtest

import (
	"sync"
	"time"
)

// Clock exposes the current simulated time during backtests.
type Clock interface {
	Now() time.Time
}

// SimulatedClock is advanced by the replay iterator as each bar is consumed.
type SimulatedClock struct {
	mu      sync.RWMutex
	now     time.Time
	started bool
}

func newSimulatedClock() *SimulatedClock {
	return &SimulatedClock{}
}

// Now returns the current simulated time. It returns the zero value until the
// first replay bar advances the clock.
func (c *SimulatedClock) Now() time.Time {
	if c == nil {
		return time.Time{}
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	if !c.started {
		return time.Time{}
	}

	return c.now
}

func (c *SimulatedClock) set(now time.Time) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	c.now = now
	c.started = true
}
