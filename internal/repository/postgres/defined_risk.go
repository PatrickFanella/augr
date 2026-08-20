package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk"
)

type DefinedRiskRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ definedrisk.Store = (*DefinedRiskRepo)(nil)

func NewDefinedRiskRepo(pool *pgxpool.Pool) *DefinedRiskRepo { return &DefinedRiskRepo{pool: pool} }

type (
	definedRiskPolicyEnvelope struct {
		Schema        string                    `json:"schema"`
		Version       string                    `json:"version"`
		ExecutionMode definedrisk.ExecutionMode `json:"execution_mode"`
		DecimalScale  int                       `json:"decimal_scale"`
	}
	definedRiskQuoteEnvelope struct {
		EvidenceID     string `json:"evidence_id"`
		EvidenceSHA256 string `json:"evidence_sha256"`
		AvailableAt    string `json:"available_at"`
	}
	definedRiskLegEnvelope struct {
		InstrumentID    string                    `json:"instrument_id"`
		VenueContractID string                    `json:"venue_contract_id"`
		OptionType      string                    `json:"option_type"`
		Strike          string                    `json:"strike"`
		Position        string                    `json:"position"`
		Entry           definedRiskQuoteEnvelope  `json:"entry"`
		Unwind          *definedRiskQuoteEnvelope `json:"unwind"`
	}
	definedRiskScenarioEnvelope struct {
		Schema                         string               `json:"schema"`
		State                          string               `json:"state"`
		PolicyID                       string               `json:"policy_id"`
		PolicySHA256                   string               `json:"policy_sha256"`
		Strategy                       definedrisk.Strategy `json:"strategy"`
		InitialCapital                 string               `json:"initial_capital"`
		RequestedContracts             int                  `json:"requested_contracts"`
		DecisionAt                     string               `json:"decision_at"`
		ExpiryAt                       string               `json:"expiry_at"`
		TerminalUnderlying             string               `json:"terminal_underlying"`
		TerminalAvailableAt            string               `json:"terminal_available_at"`
		TerminalEvidenceID             string               `json:"terminal_evidence_id"`
		TerminalEvidenceSHA256         string               `json:"terminal_evidence_sha256"`
		TerminalPartitionContentSHA256 string               `json:"terminal_partition_content_sha256"`
		TerminalSourceKey              string               `json:"terminal_source_key"`
		Mode                           string               `json:"mode"`
		Legs                           []json.RawMessage    `json:"legs"`
	}
)

type definedRiskReportEnvelope struct {
	Schema                   string                    `json:"schema"`
	State                    string                    `json:"state"`
	PolicyID                 string                    `json:"policy_id"`
	PolicySHA256             string                    `json:"policy_sha256"`
	ScenarioID               string                    `json:"scenario_id"`
	ScenarioSHA256           string                    `json:"scenario_sha256"`
	Strategy                 definedrisk.Strategy      `json:"strategy"`
	ExecutionMode            definedrisk.ExecutionMode `json:"execution_mode"`
	Outcome                  string                    `json:"outcome"`
	Reason                   string                    `json:"reason"`
	Contracts                int                       `json:"contracts"`
	Width                    string                    `json:"width"`
	NetPremiumPerContract    string                    `json:"net_premium_per_contract"`
	MaximumLossPerContract   string                    `json:"maximum_loss_per_contract"`
	MaximumRewardPerContract string                    `json:"maximum_reward_per_contract"`
	OrphanReservePerContract string                    `json:"orphan_reserve_per_contract"`
	ReservedCapital          string                    `json:"reserved_capital"`
	EntryFees                string                    `json:"entry_fees"`
	UnwindFees               string                    `json:"unwind_fees"`
	OrphanLoss               string                    `json:"orphan_loss"`
	ExpirationPayoff         string                    `json:"expiration_payoff"`
	EndingCash               string                    `json:"ending_cash"`
	AfterCostTotalReturn     string                    `json:"after_cost_total_return"`
	Fills                    []json.RawMessage         `json:"fills"`
}
type definedRiskFillEnvelope struct {
	Sequence       int    `json:"sequence"`
	InstrumentID   string `json:"instrument_id"`
	Action         string `json:"action"`
	Quantity       int    `json:"quantity"`
	Price          string `json:"price"`
	Fee            string `json:"fee"`
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

func (repo *DefinedRiskRepo) RegisterPolicy(ctx context.Context, value *definedrisk.Policy) (*definedrisk.Policy, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: defined-risk policy is required")
	}
	var envelope definedRiskPolicyEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	_, err := repo.pool.Exec(ctx, `INSERT INTO defined_risk_v1_policies(id,schema_name,version,execution_mode,decimal_scale,sha256,canonical_bytes,canonical_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::jsonb,$8) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.Version, envelope.ExecutionMode, envelope.DecimalScale, value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert defined-risk policy", err)
	}
	got, err := repo.GetPolicy(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: defined-risk policy conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *DefinedRiskRepo) GetPolicy(ctx context.Context, id uuid.UUID) (*definedrisk.Policy, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: defined-risk policy identity is required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM defined_risk_v1_policies WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	value, err := definedrisk.PolicyFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct defined-risk policy %s: %w", id, err)
	}
	return value, nil
}

func (repo *DefinedRiskRepo) RegisterScenario(ctx context.Context, value *definedrisk.Scenario) (*definedrisk.Scenario, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: defined-risk scenario is required")
	}
	var envelope definedRiskScenarioEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO defined_risk_v1_scenarios(id,schema_name,state,policy_id,policy_sha256,strategy,initial_capital,requested_contracts,decision_at,expiry_at,mode,terminal_underlying,terminal_available_at,terminal_evidence_id,terminal_evidence_sha256,terminal_partition_sha256,terminal_source_key,leg_count,sha256,canonical_bytes,canonical_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,convert_from($20,'UTF8')::jsonb,$21) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.PolicyID, envelope.PolicySHA256, envelope.Strategy, envelope.InitialCapital, envelope.RequestedContracts, parseDefinedRiskTime(envelope.DecisionAt), parseDefinedRiskTime(envelope.ExpiryAt), envelope.Mode, envelope.TerminalUnderlying, parseDefinedRiskTime(envelope.TerminalAvailableAt), envelope.TerminalEvidenceID, envelope.TerminalEvidenceSHA256, envelope.TerminalPartitionContentSHA256, envelope.TerminalSourceKey, len(envelope.Legs), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert defined-risk scenario", err)
	}
	if err = repo.stage("defined_risk_scenario"); err != nil {
		return nil, err
	}
	for sequence, raw := range envelope.Legs {
		var leg definedRiskLegEnvelope
		if err = json.Unmarshal(raw, &leg); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO defined_risk_v1_legs(scenario_id,sequence,instrument_id,venue_contract_id,option_type,strike,position,canonical_leg) VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb) ON CONFLICT(scenario_id,sequence) DO NOTHING`, value.ID(), sequence, leg.InstrumentID, leg.VenueContractID, leg.OptionType, leg.Strike, leg.Position, string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert defined-risk leg", err)
		}
		if err = repo.stage("defined_risk_leg"); err != nil {
			return nil, err
		}
		observations := []struct {
			kind  string
			value *definedRiskQuoteEnvelope
			raw   json.RawMessage
		}{{"entry", &leg.Entry, extractJSONField(raw, "entry")}, {"unwind", leg.Unwind, extractJSONField(raw, "unwind")}}
		for _, observation := range observations {
			if observation.value == nil {
				continue
			}
			_, err = tx.Exec(ctx, `INSERT INTO defined_risk_v1_observations(scenario_id,leg_sequence,kind,evidence_id,evidence_sha256,available_at,canonical_quote) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(scenario_id,leg_sequence,kind) DO NOTHING`, value.ID(), sequence, observation.kind, observation.value.EvidenceID, observation.value.EvidenceSHA256, parseDefinedRiskTime(observation.value.AvailableAt), string(observation.raw))
			if err != nil {
				return nil, evaluationWriteError("insert defined-risk observation", err)
			}
			if err = repo.stage("defined_risk_observation"); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit defined-risk scenario", err)
	}
	got, err := repo.GetScenario(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: defined-risk scenario conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *DefinedRiskRepo) GetScenario(ctx context.Context, id uuid.UUID) (*definedrisk.Scenario, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: defined-risk scenario identity is required")
	}
	var digest string
	var raw []byte
	var policyID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,policy_id FROM defined_risk_v1_scenarios WHERE id=$1`, id).Scan(&digest, &raw, &policyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	policy, err := repo.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	value, err := definedrisk.ScenarioFromCanonical(id, digest, raw, policy)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct defined-risk scenario %s: %w", id, err)
	}
	if err = repo.verifyScenarioRows(ctx, id, raw); err != nil {
		return nil, err
	}
	return value, nil
}

func (repo *DefinedRiskRepo) verifyScenarioRows(ctx context.Context, id uuid.UUID, raw []byte) error {
	var envelope definedRiskScenarioEnvelope
	_ = json.Unmarshal(raw, &envelope)
	rows, err := repo.pool.Query(ctx, `SELECT canonical_leg FROM defined_risk_v1_legs WHERE scenario_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var got []byte
		if rows.Scan(&got) != nil || index >= len(envelope.Legs) || !jsonEqual(got, envelope.Legs[index]) {
			return fmt.Errorf("postgres: normalized defined-risk scenario %s does not reconstruct", id)
		}
		index++
	}
	if index != len(envelope.Legs) {
		return fmt.Errorf("postgres: normalized defined-risk scenario %s does not reconstruct", id)
	}
	for sequence, rawLeg := range envelope.Legs {
		var leg definedRiskLegEnvelope
		_ = json.Unmarshal(rawLeg, &leg)
		expected := map[string]json.RawMessage{"entry": extractJSONField(rawLeg, "entry")}
		if leg.Unwind != nil {
			expected["unwind"] = extractJSONField(rawLeg, "unwind")
		}
		obs, queryErr := repo.pool.Query(ctx, `SELECT kind,canonical_quote FROM defined_risk_v1_observations WHERE scenario_id=$1 AND leg_sequence=$2 ORDER BY kind`, id, sequence)
		if queryErr != nil {
			return queryErr
		}
		seen := 0
		for obs.Next() {
			var kind string
			var got []byte
			if obs.Scan(&kind, &got) != nil || !jsonEqual(got, expected[kind]) {
				obs.Close()
				return fmt.Errorf("postgres: normalized defined-risk scenario %s does not reconstruct", id)
			}
			seen++
		}
		obs.Close()
		if seen != len(expected) {
			return fmt.Errorf("postgres: normalized defined-risk scenario %s does not reconstruct", id)
		}
	}
	return nil
}

func (repo *DefinedRiskRepo) RecordReport(ctx context.Context, value *definedrisk.Report) (*definedrisk.Report, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: defined-risk report is required")
	}
	var envelope definedRiskReportEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO defined_risk_v1_reports(id,schema_name,state,policy_id,policy_sha256,scenario_id,scenario_sha256,strategy,execution_mode,outcome,reason,contracts,fill_count,width,net_premium_per_contract,maximum_loss_per_contract,maximum_reward_per_contract,orphan_reserve_per_contract,reserved_capital,entry_fees,unwind_fees,orphan_loss,expiration_payoff,ending_cash,after_cost_total_return,sha256,canonical_bytes,canonical_json,created_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,convert_from($27,'UTF8')::jsonb,$28) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.PolicyID, envelope.PolicySHA256, envelope.ScenarioID, envelope.ScenarioSHA256, envelope.Strategy, envelope.ExecutionMode, envelope.Outcome, envelope.Reason, envelope.Contracts, len(envelope.Fills), envelope.Width, envelope.NetPremiumPerContract, envelope.MaximumLossPerContract, envelope.MaximumRewardPerContract, envelope.OrphanReservePerContract, envelope.ReservedCapital, envelope.EntryFees, envelope.UnwindFees, envelope.OrphanLoss, envelope.ExpirationPayoff, envelope.EndingCash, envelope.AfterCostTotalReturn, value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert defined-risk report", err)
	}
	if err = repo.stage("defined_risk_report"); err != nil {
		return nil, err
	}
	for _, raw := range envelope.Fills {
		var fill definedRiskFillEnvelope
		if err = json.Unmarshal(raw, &fill); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO defined_risk_v1_fills(report_id,sequence,instrument_id,action,quantity,price,fee,evidence_id,evidence_sha256,canonical_fill) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb) ON CONFLICT(report_id,sequence) DO NOTHING`, value.ID(), fill.Sequence, fill.InstrumentID, fill.Action, fill.Quantity, fill.Price, fill.Fee, fill.EvidenceID, fill.EvidenceSHA256, string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert defined-risk fill", err)
		}
		if err = repo.stage("defined_risk_fill"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit defined-risk report", err)
	}
	got, err := repo.GetReport(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: defined-risk report conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *DefinedRiskRepo) GetReport(ctx context.Context, id uuid.UUID) (*definedrisk.Report, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: defined-risk report identity is required")
	}
	var digest string
	var raw []byte
	var policyID, scenarioID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,policy_id,scenario_id FROM defined_risk_v1_reports WHERE id=$1`, id).Scan(&digest, &raw, &policyID, &scenarioID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	policy, err := repo.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, err
	}
	scenario, err := repo.GetScenario(ctx, scenarioID)
	if err != nil {
		return nil, err
	}
	value, err := definedrisk.ReportFromCanonical(id, digest, raw, policy, scenario)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct defined-risk report %s: %w", id, err)
	}
	if err = repo.verifyReportRows(ctx, id, raw); err != nil {
		return nil, err
	}
	return value, nil
}

func (repo *DefinedRiskRepo) verifyReportRows(ctx context.Context, id uuid.UUID, raw []byte) error {
	var envelope definedRiskReportEnvelope
	_ = json.Unmarshal(raw, &envelope)
	rows, err := repo.pool.Query(ctx, `SELECT canonical_fill FROM defined_risk_v1_fills WHERE report_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var got []byte
		if rows.Scan(&got) != nil || index >= len(envelope.Fills) || !jsonEqual(got, envelope.Fills[index]) {
			return fmt.Errorf("postgres: normalized defined-risk report %s does not reconstruct", id)
		}
		index++
	}
	if index != len(envelope.Fills) {
		return fmt.Errorf("postgres: normalized defined-risk report %s does not reconstruct", id)
	}
	return nil
}

func (repo *DefinedRiskRepo) stage(value string) error {
	if repo.afterStage != nil {
		return repo.afterStage(value)
	}
	return nil
}

func parseDefinedRiskTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}

func extractJSONField(raw []byte, key string) json.RawMessage {
	var value map[string]json.RawMessage
	_ = json.Unmarshal(raw, &value)
	return value[key]
}
