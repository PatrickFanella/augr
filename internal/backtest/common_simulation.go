package backtest

import (
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/simulation"
)

// CommonSimulation is the canonical backtest path into the immutable
// simulation venue. It intentionally adds no fill, timing, fee, lifecycle, or
// evidence behavior of its own.
type CommonSimulation struct {
	venue *simulation.Venue
}

// NewCommonSimulation validates the exact policy through the common venue.
func NewCommonSimulation(policy *simulation.Policy) (*CommonSimulation, error) {
	venue, err := simulation.NewVenue(policy)
	if err != nil {
		return nil, fmt.Errorf("construct backtest common simulation: %w", err)
	}
	return &CommonSimulation{venue: venue}, nil
}

// PolicyVersion returns the content-addressed policy version used by Evaluate.
func (adapter *CommonSimulation) PolicyVersion() string {
	if adapter == nil || adapter.venue == nil {
		return ""
	}
	return adapter.venue.PolicyVersion()
}

// PolicyDigest returns the exact policy digest used by Evaluate.
func (adapter *CommonSimulation) PolicyDigest() string {
	if adapter == nil || adapter.venue == nil {
		return ""
	}
	return adapter.venue.PolicyDigest()
}

// Evaluate delegates one canonical request without mutation.
func (adapter *CommonSimulation) Evaluate(request simulation.EvaluationRequest) (*simulation.Result, error) {
	if adapter == nil || adapter.venue == nil {
		return nil, fmt.Errorf("backtest common simulation is not initialized")
	}
	return adapter.venue.Evaluate(request)
}
