package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/copyorigin"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type CopyOriginRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ copyorigin.Store = (*CopyOriginRepo)(nil)

func NewCopyOriginRepo(pool *pgxpool.Pool) *CopyOriginRepo { return &CopyOriginRepo{pool: pool} }

type copyOriginEnvelope struct {
	Schema              string            `json:"schema"`
	State               string            `json:"state"`
	SubscriptionID      string            `json:"subscription_id"`
	OriginType          string            `json:"origin_type"`
	OriginID            string            `json:"origin_id"`
	SourceObservationID string            `json:"source_observation_id"`
	CalculationVersion  int               `json:"calculation_version"`
	Intents             []json.RawMessage `json:"intents"`
}

type copyOriginIntentEnvelope struct {
	ID                  string `json:"id"`
	InstrumentKey       string `json:"instrument_key"`
	SourceObservationID string `json:"source_observation_id"`
}

func (r *CopyOriginRepo) RegisterRun(ctx context.Context, run *copyorigin.Run) (*copyorigin.Run, error) {
	if r == nil || r.pool == nil || run == nil {
		return nil, fmt.Errorf("postgres: copy origin run is required")
	}
	var envelope copyOriginEnvelope
	if err := json.Unmarshal(run.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO copy_origin_rebalance_runs(id,schema_name,state,subscription_id,origin_type,origin_id,source_observation_id,calculation_version,intent_count,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,convert_from($11,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, run.ID(), envelope.Schema, envelope.State, envelope.SubscriptionID, envelope.OriginType, envelope.OriginID, envelope.SourceObservationID, envelope.CalculationVersion, len(envelope.Intents), run.Digest(), run.CanonicalBytes())
	if err != nil {
		return nil, fmt.Errorf("postgres: insert copy origin run: %w", err)
	}
	if r.afterStage != nil {
		if err = r.afterStage("run"); err != nil {
			return nil, err
		}
	}
	for sequence, raw := range envelope.Intents {
		var intent copyOriginIntentEnvelope
		if err = json.Unmarshal(raw, &intent); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO copy_origin_rebalance_intents(run_id,sequence,intent_id,instrument_key,source_observation_id,canonical_intent) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT(run_id,sequence) DO NOTHING`, run.ID(), sequence, intent.ID, intent.InstrumentKey, intent.SourceObservationID, string(raw))
		if err != nil {
			return nil, fmt.Errorf("postgres: insert copy origin run intent: %w", err)
		}
		if r.afterStage != nil {
			if err = r.afterStage("intent"); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	got, err := r.GetRun(ctx, run.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != run.Digest() || !bytes.Equal(got.CanonicalBytes(), run.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: copy origin run conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (r *CopyOriginRepo) GetRun(ctx context.Context, id uuid.UUID) (*copyorigin.Run, error) {
	if r == nil || r.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: copy origin run identity is required")
	}
	var digest string
	var raw []byte
	err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM copy_origin_rebalance_runs WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var envelope copyOriginEnvelope
	if err = json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, `SELECT canonical_intent FROM copy_origin_rebalance_intents WHERE run_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var child []byte
		if rows.Scan(&child) != nil || index >= len(envelope.Intents) || !jsonEqual(child, envelope.Intents[index]) {
			return nil, fmt.Errorf("postgres: normalized copy origin run does not reconstruct")
		}
		index++
	}
	if index != len(envelope.Intents) {
		return nil, fmt.Errorf("postgres: normalized copy origin run does not reconstruct")
	}
	return copyorigin.FromCanonical(id, digest, raw)
}
