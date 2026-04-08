package risk

import (
	"context"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// StatePersister is an optional persistence layer for runtime risk state.
// Implementations write kill-switch activations to durable storage so that
// an operator-activated kill-switch survives a process restart.
// File-flag and environment-variable mechanisms are always re-evaluated at
// runtime and do not need to be persisted.
type StatePersister interface {
	// Load retrieves the last persisted risk state. Returns a zero-value
	// PersistedRiskState without error when no state has been saved yet.
	Load(ctx context.Context) (PersistedRiskState, error)
	// Save writes the current API-toggle kill-switch state to durable storage.
	Save(ctx context.Context, state PersistedRiskState) error
}

// PersistedRiskState is the subset of RiskEngineImpl state that survives
// process restarts. Only API-toggle activations are stored here; file and
// environment-variable mechanisms are inherently durable and do not need DB
// persistence.
type PersistedRiskState struct {
	KillSwitch          KillSwitchStatus                       `json:"kill_switch"`
	MarketKillSwitches  map[domain.MarketType]KillSwitchStatus `json:"market_kill_switches"`
}
