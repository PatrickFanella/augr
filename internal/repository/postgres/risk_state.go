package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

// RiskStatePersister implements risk.StatePersister using the risk_state table.
// The table holds exactly one row (id = 1) with JSONB columns for kill-switch state.
type RiskStatePersister struct {
	pool *pgxpool.Pool
}

// NewRiskStatePersister returns a RiskStatePersister backed by the given pool.
func NewRiskStatePersister(pool *pgxpool.Pool) *RiskStatePersister {
	return &RiskStatePersister{pool: pool}
}

// Load retrieves persisted risk state from the database.
// Returns a zero-value PersistedRiskState without error when no state exists.
func (r *RiskStatePersister) Load(ctx context.Context) (risk.PersistedRiskState, error) {
	var ksJSON, mksJSON []byte
	err := r.pool.QueryRow(ctx,
		`SELECT kill_switch, market_kill_switches FROM risk_state WHERE id = 1`,
	).Scan(&ksJSON, &mksJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return risk.PersistedRiskState{}, nil
		}
		return risk.PersistedRiskState{}, fmt.Errorf("postgres: load risk state: %w", err)
	}

	var state risk.PersistedRiskState
	if len(ksJSON) > 2 { // more than just '{}'
		if err := json.Unmarshal(ksJSON, &state.KillSwitch); err != nil {
			return risk.PersistedRiskState{}, fmt.Errorf("postgres: unmarshal kill switch: %w", err)
		}
	}
	if len(mksJSON) > 2 {
		if err := json.Unmarshal(mksJSON, &state.MarketKillSwitches); err != nil {
			return risk.PersistedRiskState{}, fmt.Errorf("postgres: unmarshal market kill switches: %w", err)
		}
	}
	return state, nil
}

// Save persists the current kill-switch state to the database using an UPSERT.
func (r *RiskStatePersister) Save(ctx context.Context, state risk.PersistedRiskState) error {
	ksJSON, err := json.Marshal(state.KillSwitch)
	if err != nil {
		return fmt.Errorf("postgres: marshal kill switch: %w", err)
	}
	mksJSON, err := json.Marshal(state.MarketKillSwitches)
	if err != nil {
		return fmt.Errorf("postgres: marshal market kill switches: %w", err)
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO risk_state (id, kill_switch, market_kill_switches, updated_at)
		 VALUES (1, $1, $2, NOW())
		 ON CONFLICT (id) DO UPDATE
		   SET kill_switch          = EXCLUDED.kill_switch,
		       market_kill_switches = EXCLUDED.market_kill_switches,
		       updated_at           = EXCLUDED.updated_at`,
		ksJSON, mksJSON,
	)
	if err != nil {
		return fmt.Errorf("postgres: save risk state: %w", err)
	}
	return nil
}
