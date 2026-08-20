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

	"github.com/PatrickFanella/get-rich-quick/internal/completeset"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type CompleteSetRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ completeset.Store = (*CompleteSetRepo)(nil)

func NewCompleteSetRepo(pool *pgxpool.Pool) *CompleteSetRepo { return &CompleteSetRepo{pool: pool} }

type completeSetEnvelope struct {
	Schema                 string            `json:"schema"`
	State                  string            `json:"state"`
	Reason                 string            `json:"reason"`
	RecorderID             string            `json:"recorder_id"`
	RecorderSHA256         string            `json:"recorder_sha256"`
	CandidateKey           string            `json:"candidate_key"`
	MarketID               string            `json:"market_id"`
	Outcomes               []string          `json:"outcomes"`
	SetQuantity            string            `json:"set_quantity"`
	PayoutPerSet           string            `json:"payout_per_set"`
	AvailableCapital       string            `json:"available_capital"`
	MinimumProfit          string            `json:"minimum_profit"`
	EntryCost              string            `json:"entry_cost"`
	Payout                 string            `json:"payout"`
	AfterCostProfit        string            `json:"after_cost_profit"`
	WorstOrphanKey         string            `json:"worst_orphan_key"`
	WorstOrphanLoss        string            `json:"worst_orphan_loss"`
	ReservedCapital        string            `json:"reserved_capital"`
	ProfitAfterOrphanGuard string            `json:"profit_after_orphan_guard"`
	Bindings               []json.RawMessage `json:"bindings"`
	Legs                   []json.RawMessage `json:"legs"`
	Scenarios              []json.RawMessage `json:"scenarios"`
}

type completeBindingRow struct {
	OutcomeID      string `json:"outcome_id"`
	EntrySequence  int    `json:"entry_sequence"`
	UnwindSequence int    `json:"unwind_sequence"`
}
type completeLegRow struct {
	Sequence       int    `json:"sequence"`
	OutcomeID      string `json:"outcome_id"`
	EntrySequence  int    `json:"entry_sequence"`
	UnwindSequence int    `json:"unwind_sequence"`
	EntryCost      string `json:"entry_cost"`
	UnwindProceeds string `json:"unwind_proceeds"`
	OrphanLoss     string `json:"orphan_loss"`
}
type completeScenarioLegRow struct {
	Sequence       int    `json:"sequence"`
	OutcomeID      string `json:"outcome_id"`
	EntryCost      string `json:"entry_cost"`
	UnwindProceeds string `json:"unwind_proceeds"`
	Loss           string `json:"loss"`
}
type completeScenarioRow struct {
	Sequence       int                      `json:"sequence"`
	Key            string                   `json:"key"`
	EntryCost      string                   `json:"entry_cost"`
	UnwindProceeds string                   `json:"unwind_proceeds"`
	Loss           string                   `json:"loss"`
	Legs           []completeScenarioLegRow `json:"legs"`
}

func (r *CompleteSetRepo) RegisterCandidate(ctx context.Context, value *completeset.Candidate) (*completeset.Candidate, error) {
	if r == nil || r.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: complete set candidate is required")
	}
	var envelope completeSetEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	scenarioLegCount := 0
	for _, raw := range envelope.Scenarios {
		var row completeScenarioRow
		if json.Unmarshal(raw, &row) != nil {
			return nil, fmt.Errorf("postgres: decode complete set scenario")
		}
		scenarioLegCount += len(row.Legs)
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO complete_set_candidates(id,schema_name,state,reason,recorder_id,recorder_sha256,candidate_key,market_id,outcome_count,binding_count,leg_count,scenario_count,scenario_leg_count,set_quantity,payout_per_set,available_capital,minimum_profit,entry_cost,payout,after_cost_profit,worst_orphan_key,worst_orphan_loss,reserved_capital,profit_after_orphan_guard,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,convert_from($26,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.Reason, envelope.RecorderID, envelope.RecorderSHA256, envelope.CandidateKey, envelope.MarketID, len(envelope.Outcomes), len(envelope.Bindings), len(envelope.Legs), len(envelope.Scenarios), scenarioLegCount, envelope.SetQuantity, envelope.PayoutPerSet, envelope.AvailableCapital, envelope.MinimumProfit, envelope.EntryCost, envelope.Payout, envelope.AfterCostProfit, envelope.WorstOrphanKey, envelope.WorstOrphanLoss, envelope.ReservedCapital, envelope.ProfitAfterOrphanGuard, value.Digest(), value.CanonicalBytes())
	if err != nil {
		return nil, completeSetWriteError("insert parent", err)
	}
	if err = r.stage("parent"); err != nil {
		return nil, err
	}
	for sequence, raw := range envelope.Bindings {
		var row completeBindingRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO complete_set_bindings(candidate_id,sequence,outcome_id,entry_sequence,unwind_sequence,canonical_row) VALUES($1,$2,$3,$4,$5,$6::jsonb) ON CONFLICT(candidate_id,sequence) DO NOTHING`, value.ID(), sequence, row.OutcomeID, row.EntrySequence, row.UnwindSequence, string(raw))
		if err != nil {
			return nil, completeSetWriteError("insert binding", err)
		}
		if err = r.stage("binding"); err != nil {
			return nil, err
		}
	}
	for _, raw := range envelope.Legs {
		var row completeLegRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO complete_set_legs(candidate_id,sequence,outcome_id,entry_sequence,unwind_sequence,entry_cost,unwind_proceeds,orphan_loss,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9::jsonb) ON CONFLICT(candidate_id,sequence) DO NOTHING`, value.ID(), row.Sequence, row.OutcomeID, row.EntrySequence, row.UnwindSequence, row.EntryCost, row.UnwindProceeds, row.OrphanLoss, string(raw))
		if err != nil {
			return nil, completeSetWriteError("insert leg", err)
		}
		if err = r.stage("leg"); err != nil {
			return nil, err
		}
	}
	for _, raw := range envelope.Scenarios {
		var row completeScenarioRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO complete_set_orphan_scenarios(candidate_id,sequence,scenario_key,entry_cost,unwind_proceeds,loss,leg_count,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(candidate_id,sequence) DO NOTHING`, value.ID(), row.Sequence, row.Key, row.EntryCost, row.UnwindProceeds, row.Loss, len(row.Legs), string(raw))
		if err != nil {
			return nil, completeSetWriteError("insert scenario", err)
		}
		if err = r.stage("scenario"); err != nil {
			return nil, err
		}
		for _, leg := range row.Legs {
			legRaw, _ := json.Marshal(leg)
			_, err = tx.Exec(ctx, `INSERT INTO complete_set_orphan_scenario_legs(candidate_id,scenario_sequence,sequence,outcome_id,entry_cost,unwind_proceeds,loss,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(candidate_id,scenario_sequence,sequence) DO NOTHING`, value.ID(), row.Sequence, leg.Sequence, leg.OutcomeID, leg.EntryCost, leg.UnwindProceeds, leg.Loss, string(legRaw))
			if err != nil {
				return nil, completeSetWriteError("insert scenario leg", err)
			}
			if err = r.stage("scenario_leg"); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, completeSetWriteError("commit", err)
	}
	recorder, err := r.loadRecorder(ctx, value.RecorderID())
	if err != nil {
		return nil, err
	}
	loaded, err := r.GetCandidate(ctx, value.ID(), recorder)
	if err != nil {
		return nil, err
	}
	if loaded.Digest() != value.Digest() || !bytes.Equal(loaded.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: complete set conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loaded, nil
}

func (r *CompleteSetRepo) GetCandidate(ctx context.Context, id uuid.UUID, recorder *predictionreplay.Recorder) (*completeset.Candidate, error) {
	if r == nil || r.pool == nil || id == uuid.Nil || recorder == nil {
		return nil, fmt.Errorf("postgres: complete set identity and recorder are required")
	}
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM complete_set_candidates WHERE id=$1`, id).Scan(&digest, &raw); errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var envelope completeSetEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, fmt.Errorf("postgres: decode complete set")
	}
	for table, values := range map[string][]json.RawMessage{"complete_set_bindings": envelope.Bindings, "complete_set_legs": envelope.Legs, "complete_set_orphan_scenarios": envelope.Scenarios} {
		rows, err := r.pool.Query(ctx, `SELECT canonical_row FROM `+table+` WHERE candidate_id=$1 ORDER BY sequence`, id)
		if err != nil {
			return nil, err
		}
		index := 0
		for rows.Next() {
			var normalized []byte
			if rows.Scan(&normalized) != nil || index >= len(values) || !jsonEqual(normalized, values[index]) {
				rows.Close()
				return nil, fmt.Errorf("postgres: normalized complete set %s does not reconstruct", id)
			}
			index++
		}
		rows.Close()
		if index != len(values) {
			return nil, fmt.Errorf("postgres: normalized complete set %s does not reconstruct", id)
		}
	}
	value, err := completeset.FromCanonical(id, digest, raw, recorder)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct complete set %s: %w", id, err)
	}
	return value, nil
}

func (r *CompleteSetRepo) loadRecorder(ctx context.Context, id uuid.UUID) (*predictionreplay.Recorder, error) {
	var manifestID uuid.UUID
	if err := r.pool.QueryRow(ctx, `SELECT manifest_id FROM prediction_book_fee_recorders WHERE id=$1`, id).Scan(&manifestID); err != nil {
		return nil, err
	}
	manifest, err := NewDatasetRepo(r.pool).GetDatasetManifest(ctx, manifestID)
	if err != nil {
		return nil, err
	}
	return NewPredictionReplayRepo(r.pool).GetRecorder(ctx, id, manifest)
}

func (r *CompleteSetRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}

func completeSetWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "foreign key")) {
		return fmt.Errorf("postgres: complete set %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: complete set %s: %w", action, err)
}
