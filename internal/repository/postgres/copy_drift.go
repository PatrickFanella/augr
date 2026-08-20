package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/copydrift"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type CopyDriftRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ copydrift.Store = (*CopyDriftRepo)(nil)

func NewCopyDriftRepo(pool *pgxpool.Pool) *CopyDriftRepo { return &CopyDriftRepo{pool: pool} }

type copyDriftEnvelope struct {
	Schema                 string         `json:"schema"`
	State                  string         `json:"state"`
	SubscriptionID         string         `json:"subscription_id"`
	OriginType             string         `json:"origin_type"`
	OriginID               string         `json:"origin_id"`
	SourceObservationID    string         `json:"source_observation_id"`
	SessionKey             string         `json:"session_key"`
	CalculationVersion     int            `json:"calculation_version"`
	MaximumSessionTurnover string         `json:"maximum_session_turnover"`
	SessionBudget          string         `json:"session_budget"`
	StartingDrift          string         `json:"starting_drift"`
	PreparedTurnover       string         `json:"prepared_turnover"`
	ResidualDrift          string         `json:"residual_drift"`
	Converged              bool           `json:"converged"`
	Legs                   []copyDriftLeg `json:"legs"`
}

type copyDriftLeg struct {
	Sequence          int    `json:"sequence"`
	InstrumentKey     string `json:"instrument_key"`
	Side              string `json:"side"`
	CurrentValue      string `json:"current_value"`
	TargetValue       string `json:"target_value"`
	StartingDrift     string `json:"starting_drift"`
	RequestedNotional string `json:"requested_notional"`
	ProjectedValue    string `json:"projected_value"`
	ResidualDrift     string `json:"residual_drift"`
}

func (r *CopyDriftRepo) RegisterRun(ctx context.Context, value *copydrift.Run) (*copydrift.Run, error) {
	if r == nil || r.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: copy drift run is required")
	}
	var envelope copyDriftEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO copy_target_drift_runs(id,schema_name,state,subscription_id,origin_type,origin_id,source_observation_id,session_key,calculation_version,maximum_session_turnover,session_budget,starting_drift,prepared_turnover,residual_drift,converged,leg_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,convert_from($18,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.SubscriptionID, envelope.OriginType, envelope.OriginID, envelope.SourceObservationID, envelope.SessionKey, envelope.CalculationVersion, envelope.MaximumSessionTurnover, envelope.SessionBudget, envelope.StartingDrift, envelope.PreparedTurnover, envelope.ResidualDrift, envelope.Converged, len(envelope.Legs), value.Digest(), value.CanonicalBytes())
	if err != nil {
		return nil, copyDriftWriteError("insert run", err)
	}
	if err = r.stage("run"); err != nil {
		return nil, err
	}
	for _, leg := range envelope.Legs {
		raw, _ := json.Marshal(leg)
		_, err = tx.Exec(ctx, `INSERT INTO copy_target_drift_legs(run_id,sequence,instrument_key,side,current_value,target_value,starting_drift,requested_notional,projected_value,residual_drift,canonical_leg) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb) ON CONFLICT(run_id,sequence) DO NOTHING`, value.ID(), leg.Sequence, leg.InstrumentKey, leg.Side, leg.CurrentValue, leg.TargetValue, leg.StartingDrift, leg.RequestedNotional, leg.ProjectedValue, leg.ResidualDrift, string(raw))
		if err != nil {
			return nil, copyDriftWriteError("insert leg", err)
		}
		if err = r.stage("leg"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, copyDriftWriteError("commit run", err)
	}
	loaded, err := r.GetRun(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if loaded.Digest() != value.Digest() || !bytes.Equal(loaded.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: copy drift run conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loaded, nil
}

func (r *CopyDriftRepo) GetRun(ctx context.Context, id uuid.UUID) (*copydrift.Run, error) {
	if r == nil || r.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: copy drift run identity is required")
	}
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM copy_target_drift_runs WHERE id=$1`, id).Scan(&digest, &raw); errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var envelope copyDriftEnvelope
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT canonical_leg FROM copy_target_drift_legs WHERE run_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var normalized []byte
		if rows.Scan(&normalized) != nil || index >= len(envelope.Legs) || !jsonEqual(normalized, mustCopyDriftJSON(envelope.Legs[index])) {
			return nil, fmt.Errorf("postgres: normalized copy drift run %s does not reconstruct", id)
		}
		index++
	}
	if rows.Err() != nil || index != len(envelope.Legs) {
		return nil, fmt.Errorf("postgres: normalized copy drift run %s does not reconstruct", id)
	}
	value, err := copydrift.FromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct copy drift run %s: %w", id, err)
	}
	return value, nil
}

func (r *CopyDriftRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}
func mustCopyDriftJSON(value any) []byte { raw, _ := json.Marshal(value); return raw }
func copyDriftWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct")) {
		return fmt.Errorf("postgres: copy drift %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: copy drift %s: %w", action, err)
}
