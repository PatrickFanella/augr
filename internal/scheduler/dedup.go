package scheduler

import (
	"sync"

	"github.com/google/uuid"
)

// strategyDedup prevents concurrent execution of the same strategy.
type strategyDedup struct {
	inFlight sync.Map
}

// TryAcquire returns true and marks the strategy as in-flight if it was not
// already running. Returns false if the strategy is already in-flight.
func (d *strategyDedup) TryAcquire(strategyID uuid.UUID) bool {
	_, loaded := d.inFlight.LoadOrStore(strategyID, struct{}{})
	return !loaded
}

// Release marks the strategy as no longer in-flight.
func (d *strategyDedup) Release(strategyID uuid.UUID) {
	d.inFlight.Delete(strategyID)
}
