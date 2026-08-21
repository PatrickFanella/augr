package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel"
)

type WheelRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ wheel.Store = (*WheelRepo)(nil)

func NewWheelRepo(pool *pgxpool.Pool) *WheelRepo { return &WheelRepo{pool: pool} }

type wheelPolicyEnvelope struct {
	Schema       string `json:"schema"`
	Version      string `json:"version"`
	DecimalScale int    `json:"decimal_scale"`
}

type wheelScenarioEnvelope struct {
	Schema          string            `json:"schema"`
	State           string            `json:"state"`
	PolicyID        string            `json:"policy_id"`
	PolicySHA256    string            `json:"policy_sha256"`
	UnderlyingID    string            `json:"underlying_id"`
	InitialCapital  string            `json:"initial_capital"`
	EvaluationStart string            `json:"evaluation_start"`
	EvaluationEnd   string            `json:"evaluation_end"`
	Mode            string            `json:"mode"`
	Events          []json.RawMessage `json:"events"`
}

type wheelEventEnvelope struct {
	Sequence       int    `json:"sequence"`
	Kind           string `json:"kind"`
	OccurredAt     string `json:"occurred_at"`
	EvidenceID     string `json:"evidence_id"`
	EvidenceSHA256 string `json:"evidence_sha256"`
}

type wheelReportEnvelope struct {
	Schema                string             `json:"schema"`
	State                 string             `json:"state"`
	PolicyID              string             `json:"policy_id"`
	PolicySHA256          string             `json:"policy_sha256"`
	ScenarioID            string             `json:"scenario_id"`
	ScenarioSHA256        string             `json:"scenario_sha256"`
	UnderlyingID          string             `json:"underlying_id"`
	InitialCapital        string             `json:"initial_capital"`
	EvaluationStart       string             `json:"evaluation_start"`
	EvaluationEnd         string             `json:"evaluation_end"`
	Transitions           []wheel.Transition `json:"transitions"`
	EndingCash            string             `json:"ending_cash"`
	EndingShares          string             `json:"ending_shares"`
	EndingCollateral      string             `json:"ending_collateral"`
	EndingOptionLiability string             `json:"ending_option_liability"`
	EndingNetLiquidation  string             `json:"ending_net_liquidation"`
	PremiumIncome         string             `json:"premium_income"`
	DividendIncome        string             `json:"dividend_income"`
	TotalFees             string             `json:"total_fees"`
	CappedUpside          string             `json:"capped_upside"`
	AfterCostTotalReturn  string             `json:"after_cost_total_return"`
}

func (repo *WheelRepo) RegisterPolicy(ctx context.Context, value *wheel.Policy) (*wheel.Policy, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: wheel policy is required")
	}
	var envelope wheelPolicyEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	_, err := repo.pool.Exec(ctx, `INSERT INTO wheel_v1_policies(id,schema_name,version,decimal_scale,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,convert_from($6,'UTF8')::jsonb,$7) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.Version, envelope.DecimalScale, value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert wheel policy", err)
	}
	got, err := repo.GetPolicy(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: wheel policy conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *WheelRepo) GetPolicy(ctx context.Context, id uuid.UUID) (*wheel.Policy, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: wheel policy identity is required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM wheel_v1_policies WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	value, err := wheel.PolicyFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct wheel policy %s: %w", id, err)
	}
	return value, nil
}

func (repo *WheelRepo) RegisterScenario(ctx context.Context, value *wheel.Scenario) (*wheel.Scenario, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: wheel scenario is required")
	}
	var envelope wheelScenarioEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO wheel_v1_scenarios(id,schema_name,state,policy_id,policy_sha256,underlying_id,initial_capital,evaluation_start,evaluation_end,mode,event_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,convert_from($13,'UTF8')::jsonb,$14) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.PolicyID, envelope.PolicySHA256, envelope.UnderlyingID, envelope.InitialCapital, parseWheelTime(envelope.EvaluationStart), parseWheelTime(envelope.EvaluationEnd), envelope.Mode, len(envelope.Events), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert wheel scenario", err)
	}
	if err = repo.stage("wheel_scenario"); err != nil {
		return nil, err
	}
	for _, raw := range envelope.Events {
		var event wheelEventEnvelope
		if err = json.Unmarshal(raw, &event); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO wheel_v1_source_observations(scenario_id,sequence,event_kind,occurred_at,evidence_id,evidence_sha256,canonical_event)
			VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(scenario_id,sequence) DO NOTHING`, value.ID(), event.Sequence, event.Kind, parseWheelTime(event.OccurredAt), event.EvidenceID, event.EvidenceSHA256, string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert wheel source observation", err)
		}
		if err = repo.stage("wheel_source_observation"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit wheel scenario", err)
	}
	got, err := repo.GetScenario(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: wheel scenario conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *WheelRepo) GetScenario(ctx context.Context, id uuid.UUID) (*wheel.Scenario, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: wheel scenario identity is required")
	}
	var digest string
	var raw []byte
	var policyID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,policy_id FROM wheel_v1_scenarios WHERE id=$1`, id).Scan(&digest, &raw, &policyID)
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
	value, err := wheel.ScenarioFromCanonical(id, digest, raw, policy)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct wheel scenario %s: %w", id, err)
	}
	var envelope wheelScenarioEnvelope
	_ = json.Unmarshal(raw, &envelope)
	rows, err := repo.pool.Query(ctx, `SELECT canonical_event FROM wheel_v1_source_observations WHERE scenario_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	index := 0
	for rows.Next() {
		var normalized []byte
		if err = rows.Scan(&normalized); err != nil || index >= len(envelope.Events) || !jsonEqual(normalized, envelope.Events[index]) {
			return nil, fmt.Errorf("postgres: normalized wheel scenario %s does not reconstruct", id)
		}
		index++
	}
	if err = rows.Err(); err != nil || index != len(envelope.Events) {
		return nil, fmt.Errorf("postgres: normalized wheel scenario %s does not reconstruct", id)
	}
	return value, nil
}

func (repo *WheelRepo) RecordReport(ctx context.Context, value *wheel.Report) (*wheel.Report, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: wheel report is required")
	}
	var envelope wheelReportEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO wheel_v1_reports(id,schema_name,state,policy_id,policy_sha256,scenario_id,scenario_sha256,underlying_id,initial_capital,evaluation_start,evaluation_end,transition_count,
		ending_cash,ending_shares,ending_collateral,ending_option_liability,ending_net_liquidation,premium_income,dividend_income,total_fees,capped_upside,after_cost_total_return,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,convert_from($24,'UTF8')::jsonb,$25) ON CONFLICT(id) DO NOTHING`,
		value.ID(), envelope.Schema, envelope.State, envelope.PolicyID, envelope.PolicySHA256, envelope.ScenarioID, envelope.ScenarioSHA256, envelope.UnderlyingID, envelope.InitialCapital,
		parseWheelTime(envelope.EvaluationStart), parseWheelTime(envelope.EvaluationEnd), len(envelope.Transitions), envelope.EndingCash, envelope.EndingShares, envelope.EndingCollateral,
		envelope.EndingOptionLiability, envelope.EndingNetLiquidation, envelope.PremiumIncome, envelope.DividendIncome, envelope.TotalFees, envelope.CappedUpside, envelope.AfterCostTotalReturn,
		value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert wheel report", err)
	}
	if err = repo.stage("wheel_report"); err != nil {
		return nil, err
	}
	for _, transition := range envelope.Transitions {
		selected := any(nil)
		if transition.SelectedInstrumentID != uuid.Nil.String() {
			selected = transition.SelectedInstrumentID
		}
		canonical, _ := json.Marshal(transition)
		_, err = tx.Exec(ctx, `INSERT INTO wheel_v1_transitions(report_id,sequence,event_kind,occurred_at,action,reason,selected_instrument_id,cash,shares,collateral,option_liability,net_liquidation,canonical_transition)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13::jsonb) ON CONFLICT(report_id,sequence) DO NOTHING`, value.ID(), transition.Sequence, transition.EventKind, parseWheelTime(transition.OccurredAt), transition.Action, transition.Reason, selected, transition.Cash, transition.Shares, transition.Collateral, transition.OptionLiability, transition.NetLiquidation, string(canonical))
		if err != nil {
			return nil, evaluationWriteError("insert wheel transition", err)
		}
		if transition.SelectedInstrumentID != uuid.Nil.String() {
			_, err = tx.Exec(ctx, `INSERT INTO wheel_v1_selected_contracts(report_id,transition_sequence,instrument_id) VALUES($1,$2,$3) ON CONFLICT(report_id,transition_sequence) DO NOTHING`, value.ID(), transition.Sequence, transition.SelectedInstrumentID)
			if err != nil {
				return nil, evaluationWriteError("insert wheel selected contract", err)
			}
			if err = repo.stage("wheel_selected_contract"); err != nil {
				return nil, err
			}
		}
		for effectSequence, effect := range transition.Effects {
			_, err = tx.Exec(ctx, `INSERT INTO wheel_v1_economic_effects(report_id,transition_sequence,effect_sequence,kind,instrument_id,quantity,amount,evidence_id,evidence_sha256)
				VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(report_id,transition_sequence,effect_sequence) DO NOTHING`, value.ID(), transition.Sequence, effectSequence, effect.Kind, effect.InstrumentID, effect.Quantity, effect.Amount, effect.EvidenceID, effect.EvidenceSHA256)
			if err != nil {
				return nil, evaluationWriteError("insert wheel economic effect", err)
			}
			if err = repo.stage("wheel_economic_effect"); err != nil {
				return nil, err
			}
		}
		if err = repo.stage("wheel_transition"); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit wheel report", err)
	}
	got, err := repo.GetReport(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: wheel report conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *WheelRepo) GetReport(ctx context.Context, id uuid.UUID) (*wheel.Report, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: wheel report identity is required")
	}
	var digest string
	var raw []byte
	var scenarioID, policyID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,scenario_id,policy_id FROM wheel_v1_reports WHERE id=$1`, id).Scan(&digest, &raw, &scenarioID, &policyID)
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
	value, err := wheel.ReportFromCanonical(id, digest, raw, policy, scenario)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct wheel report %s: %w", id, err)
	}
	var envelope wheelReportEnvelope
	_ = json.Unmarshal(raw, &envelope)
	for _, transition := range envelope.Transitions {
		var canonical []byte
		if err = repo.pool.QueryRow(ctx, `SELECT canonical_transition FROM wheel_v1_transitions WHERE report_id=$1 AND sequence=$2`, id, transition.Sequence).Scan(&canonical); err != nil {
			return nil, fmt.Errorf("postgres: normalized wheel report %s does not reconstruct", id)
		}
		expected, _ := json.Marshal(transition)
		if !jsonEqual(canonical, expected) {
			return nil, fmt.Errorf("postgres: normalized wheel report %s does not reconstruct", id)
		}
		effects, loadErr := repo.loadWheelEffects(ctx, id, transition.Sequence)
		if loadErr != nil || !reflect.DeepEqual(effects, transition.Effects) {
			return nil, fmt.Errorf("postgres: normalized wheel report %s does not reconstruct", id)
		}
		var selected uuid.UUID
		selectErr := repo.pool.QueryRow(ctx, `SELECT instrument_id FROM wheel_v1_selected_contracts WHERE report_id=$1 AND transition_sequence=$2`, id, transition.Sequence).Scan(&selected)
		if transition.SelectedInstrumentID == uuid.Nil.String() && !errors.Is(selectErr, pgx.ErrNoRows) || transition.SelectedInstrumentID != uuid.Nil.String() && (selectErr != nil || selected.String() != transition.SelectedInstrumentID) {
			return nil, fmt.Errorf("postgres: normalized wheel report %s does not reconstruct", id)
		}
	}
	return value, nil
}

func (repo *WheelRepo) loadWheelEffects(ctx context.Context, reportID uuid.UUID, sequence int) ([]wheel.Effect, error) {
	rows, err := repo.pool.Query(ctx, `SELECT kind,instrument_id::text,quantity,amount,evidence_id::text,evidence_sha256 FROM wheel_v1_economic_effects WHERE report_id=$1 AND transition_sequence=$2 ORDER BY effect_sequence`, reportID, sequence)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := []wheel.Effect{}
	for rows.Next() {
		var value wheel.Effect
		if err = rows.Scan(&value.Kind, &value.InstrumentID, &value.Quantity, &value.Amount, &value.EvidenceID, &value.EvidenceSHA256); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *WheelRepo) stage(value string) error {
	if repo.afterStage != nil {
		return repo.afterStage(value)
	}
	return nil
}

func parseWheelTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}

func jsonEqual(left, right []byte) bool {
	var a, b any
	return json.Unmarshal(left, &a) == nil && json.Unmarshal(right, &b) == nil && reflect.DeepEqual(a, b)
}
