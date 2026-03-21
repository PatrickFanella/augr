package risk

import (
	"context"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// RiskEngine defines hard risk controls that must be enforced independent of
// any model-driven risk analysis.
type RiskEngine interface {
	// CheckPreTrade evaluates whether an order should be allowed before submission.
	// If err is non-nil, approved/reason should be ignored by callers.
	CheckPreTrade(ctx context.Context, order *domain.Order, portfolio Portfolio) (approved bool, reason string, err error)
	// CheckPositionLimits evaluates whether adding quantity for ticker stays within limits.
	// If err is non-nil, approved/reason should be ignored by callers.
	CheckPositionLimits(ctx context.Context, ticker string, quantity float64, portfolio Portfolio) (approved bool, reason string, err error)
	GetStatus(ctx context.Context) (EngineStatus, error)
	TripCircuitBreaker(ctx context.Context, reason string) error
	ResetCircuitBreaker(ctx context.Context) error
	IsKillSwitchActive(ctx context.Context) (bool, error)
	ActivateKillSwitch(ctx context.Context, reason string) error
	DeactivateKillSwitch(ctx context.Context) error
}
