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

	"github.com/PatrickFanella/get-rich-quick/internal/makerquote"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type MakerQuoteRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ makerquote.Store = (*MakerQuoteRepo)(nil)

func NewMakerQuoteRepo(pool *pgxpool.Pool) *MakerQuoteRepo { return &MakerQuoteRepo{pool: pool} }

type makerQuoteEnvelope struct {
	Schema                  string            `json:"schema"`
	State                   string            `json:"state"`
	Reason                  string            `json:"reason"`
	RecorderID              string            `json:"recorder_id"`
	RecorderSHA256          string            `json:"recorder_sha256"`
	CandidateKey            string            `json:"candidate_key"`
	MarketID                string            `json:"market_id"`
	OutcomeID               string            `json:"outcome_id"`
	Side                    string            `json:"side"`
	DecisionAt              string            `json:"decision_at"`
	QuoteBookSourceKey      string            `json:"quote_book_source_key"`
	Venue                   string            `json:"venue"`
	QuotePrice              string            `json:"quote_price"`
	QuoteQuantity           string            `json:"quote_quantity"`
	DisplayedQueue          string            `json:"displayed_queue"`
	PriorQueue              string            `json:"prior_queue"`
	QueueAhead              string            `json:"queue_ahead"`
	StartingInventory       string            `json:"starting_inventory"`
	InventoryLimit          string            `json:"inventory_limit"`
	HourlyInventoryCostRate string            `json:"hourly_inventory_cost_rate"`
	MinimumExpectedNet      string            `json:"minimum_expected_net"`
	FilledScenarioCount     int               `json:"filled_scenario_count"`
	ExpectedGrossCapture    string            `json:"expected_gross_capture"`
	ExpectedMakerFee        string            `json:"expected_maker_fee"`
	ExpectedInventoryCost   string            `json:"expected_inventory_cost"`
	ExpectedNetCapture      string            `json:"expected_net_capture"`
	Scenarios               []json.RawMessage `json:"scenarios"`
}

type makerQuoteScenarioRow struct {
	Sequence          int    `json:"sequence"`
	Key               string `json:"key"`
	Weight            string `json:"weight"`
	HorizonAt         string `json:"horizon_at"`
	QueueOutflow      string `json:"queue_outflow"`
	MarkBookSourceKey string `json:"mark_book_source_key"`
	MarkPrice         string `json:"mark_price"`
	FilledQuantity    string `json:"filled_quantity"`
	ResidualQuantity  string `json:"residual_quantity"`
	PostFillInventory string `json:"post_fill_inventory"`
	GrossCapture      string `json:"gross_capture"`
	MakerFee          string `json:"maker_fee"`
	InventoryCost     string `json:"inventory_cost"`
	NetCapture        string `json:"net_capture"`
}

func (r *MakerQuoteRepo) RegisterCandidate(ctx context.Context, value *makerquote.Candidate) (*makerquote.Candidate, error) {
	if r == nil || r.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: maker quote candidate is required")
	}
	var envelope makerQuoteEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO maker_quote_candidates(id,schema_name,state,reason,recorder_id,recorder_sha256,candidate_key,market_id,outcome_id,side,decision_at,quote_book_source_key,venue,quote_price,quote_quantity,displayed_queue,prior_queue,queue_ahead,starting_inventory,inventory_limit,hourly_inventory_cost_rate,minimum_expected_net,scenario_count,filled_scenario_count,expected_gross_capture,expected_maker_fee,expected_inventory_cost,expected_net_capture,sha256,canonical_bytes,canonical_json) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,convert_from($30,'UTF8')::jsonb) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.Reason, envelope.RecorderID, envelope.RecorderSHA256, envelope.CandidateKey, envelope.MarketID, envelope.OutcomeID, envelope.Side, envelope.DecisionAt, envelope.QuoteBookSourceKey, envelope.Venue, envelope.QuotePrice, envelope.QuoteQuantity, envelope.DisplayedQueue, envelope.PriorQueue, envelope.QueueAhead, envelope.StartingInventory, envelope.InventoryLimit, envelope.HourlyInventoryCostRate, envelope.MinimumExpectedNet, len(envelope.Scenarios), envelope.FilledScenarioCount, envelope.ExpectedGrossCapture, envelope.ExpectedMakerFee, envelope.ExpectedInventoryCost, envelope.ExpectedNetCapture, value.Digest(), value.CanonicalBytes())
	if err != nil {
		return nil, makerQuoteWriteError("insert parent", err)
	}
	if err = r.stage("parent"); err != nil {
		return nil, err
	}
	for _, raw := range envelope.Scenarios {
		var row makerQuoteScenarioRow
		if err = json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO maker_quote_scenarios(candidate_id,sequence,scenario_key,weight,horizon_at,queue_outflow,mark_book_source_key,mark_price,filled_quantity,residual_quantity,post_fill_inventory,gross_capture,maker_fee,inventory_cost,net_capture,canonical_row) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16::jsonb) ON CONFLICT(candidate_id,sequence) DO NOTHING`, value.ID(), row.Sequence, row.Key, row.Weight, row.HorizonAt, row.QueueOutflow, row.MarkBookSourceKey, row.MarkPrice, row.FilledQuantity, row.ResidualQuantity, row.PostFillInventory, row.GrossCapture, row.MakerFee, row.InventoryCost, row.NetCapture, string(raw))
		if err != nil {
			return nil, makerQuoteWriteError("insert scenario", err)
		}
		if err = r.stage("scenario"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, makerQuoteWriteError("commit", err)
	}
	recorder, err := r.loadMakerQuoteRecorder(ctx, value.RecorderID())
	if err != nil {
		return nil, err
	}
	loaded, err := r.GetCandidate(ctx, value.ID(), recorder)
	if err != nil {
		return nil, err
	}
	if loaded.Digest() != value.Digest() || !bytes.Equal(loaded.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: maker quote conflict: %w", repository.ErrIdempotencyConflict)
	}
	return loaded, nil
}

func (r *MakerQuoteRepo) GetCandidate(ctx context.Context, id uuid.UUID, recorder *predictionreplay.Recorder) (*makerquote.Candidate, error) {
	if r == nil || r.pool == nil || id == uuid.Nil || recorder == nil {
		return nil, fmt.Errorf("postgres: maker quote identity and recorder are required")
	}
	var digest string
	var raw []byte
	if err := r.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM maker_quote_candidates WHERE id=$1`, id).Scan(&digest, &raw); errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	} else if err != nil {
		return nil, err
	}
	var envelope makerQuoteEnvelope
	if json.Unmarshal(raw, &envelope) != nil {
		return nil, fmt.Errorf("postgres: decode maker quote")
	}
	rows, err := r.pool.Query(ctx, `SELECT canonical_row FROM maker_quote_scenarios WHERE candidate_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var normalized []byte
		if rows.Scan(&normalized) != nil || index >= len(envelope.Scenarios) || !jsonEqual(normalized, envelope.Scenarios[index]) {
			return nil, fmt.Errorf("postgres: normalized maker quote %s does not reconstruct", id)
		}
		index++
	}
	if index != len(envelope.Scenarios) {
		return nil, fmt.Errorf("postgres: normalized maker quote %s does not reconstruct", id)
	}
	value, err := makerquote.FromCanonical(id, digest, raw, recorder)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct maker quote %s: %w", id, err)
	}
	return value, nil
}

func (r *MakerQuoteRepo) loadMakerQuoteRecorder(ctx context.Context, id uuid.UUID) (*predictionreplay.Recorder, error) {
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

func (r *MakerQuoteRepo) stage(value string) error {
	if r.afterStage != nil {
		return r.afterStage(value)
	}
	return nil
}

func makerQuoteWriteError(action string, err error) error {
	if err != nil && (strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "does not reconstruct") || strings.Contains(err.Error(), "foreign key")) {
		return fmt.Errorf("postgres: maker quote %s conflict: %w", action, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: maker quote %s: %w", action, err)
}
