package trend

import (
	"context"

	"github.com/google/uuid"
)

// Store persists immutable ETF Trend V1 research evidence.
type Store interface {
	RegisterPolicy(context.Context, *Policy) (*Policy, error)
	GetPolicy(context.Context, uuid.UUID) (*Policy, error)
	RegisterScenario(context.Context, *Scenario) (*Scenario, error)
	GetScenario(context.Context, uuid.UUID) (*Scenario, error)
	RecordReport(context.Context, *Report) (*Report, error)
	GetReport(context.Context, uuid.UUID) (*Report, error)
}
