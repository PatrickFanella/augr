package wheel

import (
	"context"

	"github.com/google/uuid"
)

// Store is the append-only persistence boundary for Wheel V1 research evidence.
// It deliberately exposes no promotion, scheduling, deployment, or execution API.
type Store interface {
	RegisterPolicy(context.Context, *Policy) (*Policy, error)
	GetPolicy(context.Context, uuid.UUID) (*Policy, error)
	RegisterScenario(context.Context, *Scenario) (*Scenario, error)
	GetScenario(context.Context, uuid.UUID) (*Scenario, error)
	RecordReport(context.Context, *Report) (*Report, error)
	GetReport(context.Context, uuid.UUID) (*Report, error)
}
