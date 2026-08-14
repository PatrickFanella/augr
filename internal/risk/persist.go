package risk

import (
	"context"
	"log/slog"

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
	KillSwitch         KillSwitchStatus                       `json:"kill_switch"`
	MarketKillSwitches map[domain.MarketType]KillSwitchStatus `json:"market_kill_switches"`
}

// WithStatePersister attaches a StatePersister and immediately loads any
// previously persisted kill-switch state. An operator-activated kill-switch
// will be restored so it survives process restarts. Returns the receiver for
// chaining.
func (e *RiskEngineImpl) WithStatePersister(ctx context.Context, p StatePersister) *RiskEngineImpl {
	e.persister = p
	state, err := p.Load(ctx)
	if err != nil {
		e.logger.Error("risk: failed to load persisted state, failing closed",
			slog.String("error", err.Error()))
		e.state.mu.Lock()
		e.activateKillSwitchLocked("risk state restore failed: " + err.Error())
		e.state.mu.Unlock()
		return e
	}

	e.state.mu.Lock()
	defer e.state.mu.Unlock()
	if state.KillSwitch.Active {
		e.state.ks = state.KillSwitch
		e.logger.Warn("risk: restored active kill switch from persistent state",
			slog.String("reason", state.KillSwitch.Reason))
	}
	for mt, mks := range state.MarketKillSwitches {
		if mks.Active {
			e.state.mks[mt] = mks
			e.logger.Warn("risk: restored active market kill switch from persistent state",
				slog.String("market_type", string(mt)),
				slog.String("reason", mks.Reason))
		}
	}
	return e
}

// buildPersistedStateLocked snapshots the current API-toggle kill-switch state.
// Must be called with e.state.mu held.
func (e *RiskEngineImpl) buildPersistedStateLocked() PersistedRiskState {
	mks := make(map[domain.MarketType]KillSwitchStatus, len(e.state.mks))
	for k, v := range e.state.mks {
		mks[k] = v
	}
	return PersistedRiskState{
		KillSwitch:         e.state.ks,
		MarketKillSwitches: mks,
	}
}

// saveState writes the risk state to the persister if one is configured. The
// in-memory safety action happens first, but callers receive persistence errors
// so they never mistake a restart-unsafe toggle for a durable one.
func (e *RiskEngineImpl) saveState(ctx context.Context, state PersistedRiskState) error {
	if e.persister == nil {
		return nil
	}
	if err := e.persister.Save(ctx, state); err != nil {
		e.logger.Error("risk: failed to persist kill switch state",
			slog.String("error", err.Error()))
		return err
	}
	return nil
}
