package agent

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

// RunContextRegistry tracks active run cancellation functions for best-effort cleanup.
type RunContextRegistry struct {
	mu      sync.Mutex
	cancels map[uuid.UUID]context.CancelFunc
}

// NewRunContextRegistry creates an empty active-run registry.
func NewRunContextRegistry() *RunContextRegistry {
	return &RunContextRegistry{cancels: make(map[uuid.UUID]context.CancelFunc)}
}

// Register stores the cancel func for a running pipeline.
func (r *RunContextRegistry) Register(runID uuid.UUID, cancel context.CancelFunc) {
	if r == nil || cancel == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cancels[runID] = cancel
}

// Deregister removes a pipeline from the registry after it exits.
func (r *RunContextRegistry) Deregister(runID uuid.UUID) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cancels, runID)
}

// Cancel invokes the cancel func for an active pipeline if one is registered.
func (r *RunContextRegistry) Cancel(runID uuid.UUID) bool {
	if r == nil {
		return false
	}
	r.mu.Lock()
	cancel, ok := r.cancels[runID]
	if ok {
		delete(r.cancels, runID)
	}
	r.mu.Unlock()
	if ok {
		cancel()
	}
	return ok
}
