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
	"github.com/PatrickFanella/get-rich-quick/internal/strategy/momentum"
)

type MomentumRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ momentum.Store = (*MomentumRepo)(nil)

func NewMomentumRepo(pool *pgxpool.Pool) *MomentumRepo { return &MomentumRepo{pool: pool} }

type momentumPolicyEnvelope struct {
	Schema       string `json:"schema"`
	Version      string `json:"version"`
	DecimalScale int    `json:"decimal_scale"`
}

type momentumScenarioEnvelope struct {
	Schema          string            `json:"schema"`
	State           string            `json:"state"`
	PolicyID        string            `json:"policy_id"`
	PolicySHA256    string            `json:"policy_sha256"`
	InitialCapital  string            `json:"initial_capital"`
	EvaluationStart string            `json:"evaluation_start"`
	EvaluationEnd   string            `json:"evaluation_end"`
	Mode            string            `json:"mode"`
	Rebalances      []json.RawMessage `json:"rebalances"`
}

type momentumSourceEnvelope struct {
	Sequence                int               `json:"sequence"`
	OccurredAt              string            `json:"occurred_at"`
	BenchmarkEvidenceID     string            `json:"benchmark_evidence_id"`
	BenchmarkEvidenceSHA256 string            `json:"benchmark_evidence_sha256"`
	Members                 []json.RawMessage `json:"members"`
}

type momentumMemberEnvelope struct {
	InstrumentID    string `json:"instrument_id"`
	VenueContractID string `json:"venue_contract_id"`
	EvidenceSHA256  string `json:"evidence_sha256"`
}

type momentumReportEnvelope struct {
	Schema               string            `json:"schema"`
	State                string            `json:"state"`
	PolicyID             string            `json:"policy_id"`
	PolicySHA256         string            `json:"policy_sha256"`
	ScenarioID           string            `json:"scenario_id"`
	ScenarioSHA256       string            `json:"scenario_sha256"`
	InitialCapital       string            `json:"initial_capital"`
	EvaluationStart      string            `json:"evaluation_start"`
	EvaluationEnd        string            `json:"evaluation_end"`
	EndingCash           string            `json:"ending_cash"`
	EndingEquity         string            `json:"ending_equity"`
	CumulativeTurnover   string            `json:"cumulative_turnover"`
	TotalCost            string            `json:"total_cost"`
	AfterCostTotalReturn string            `json:"after_cost_total_return"`
	Rebalances           []json.RawMessage `json:"rebalances"`
	Regimes              []json.RawMessage `json:"regimes"`
}

type momentumRebalanceEnvelope struct {
	Sequence             int               `json:"sequence"`
	OccurredAt           string            `json:"occurred_at"`
	Regime               string            `json:"regime"`
	DesiredTurnover      string            `json:"desired_turnover"`
	AppliedTurnover      string            `json:"applied_turnover"`
	TurnoverScale        string            `json:"turnover_scale"`
	RemainingTargetDrift string            `json:"remaining_target_drift"`
	Cost                 string            `json:"cost"`
	Cash                 string            `json:"cash"`
	Equity               string            `json:"equity"`
	Ranks                []json.RawMessage `json:"ranks"`
	Trades               []json.RawMessage `json:"trades"`
	Holdings             []json.RawMessage `json:"holdings"`
}

func (repo *MomentumRepo) RegisterPolicy(ctx context.Context, value *momentum.Policy) (*momentum.Policy, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: momentum policy is required")
	}
	var envelope momentumPolicyEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	_, err := repo.pool.Exec(ctx, `INSERT INTO momentum_v1_policies(id,schema_name,version,decimal_scale,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,convert_from($6,'UTF8')::jsonb,$7) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.Version, envelope.DecimalScale, value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert momentum policy", err)
	}
	got, err := repo.GetPolicy(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: momentum policy conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *MomentumRepo) GetPolicy(ctx context.Context, id uuid.UUID) (*momentum.Policy, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: momentum policy identity is required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM momentum_v1_policies WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	value, err := momentum.PolicyFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct momentum policy %s: %w", id, err)
	}
	return value, nil
}

func (repo *MomentumRepo) RegisterScenario(ctx context.Context, value *momentum.Scenario) (*momentum.Scenario, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: momentum scenario is required")
	}
	var envelope momentumScenarioEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO momentum_v1_scenarios(id,schema_name,state,policy_id,policy_sha256,initial_capital,evaluation_start,evaluation_end,mode,rebalance_count,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,convert_from($12,'UTF8')::jsonb,$13) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.PolicyID, envelope.PolicySHA256, envelope.InitialCapital, parseMomentumTime(envelope.EvaluationStart), parseMomentumTime(envelope.EvaluationEnd), envelope.Mode, len(envelope.Rebalances), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert momentum scenario", err)
	}
	if err = repo.stage("momentum_scenario"); err != nil {
		return nil, err
	}
	for _, raw := range envelope.Rebalances {
		var source momentumSourceEnvelope
		if err = json.Unmarshal(raw, &source); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO momentum_v1_source_rebalances(scenario_id,sequence,occurred_at,benchmark_evidence_id,benchmark_evidence_sha256,member_count,canonical_rebalance)
			VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(scenario_id,sequence) DO NOTHING`, value.ID(), source.Sequence, parseMomentumTime(source.OccurredAt), source.BenchmarkEvidenceID, source.BenchmarkEvidenceSHA256, len(source.Members), string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert momentum source rebalance", err)
		}
		if err = repo.stage("momentum_source_rebalance"); err != nil {
			return nil, err
		}
		for memberSequence, memberRaw := range source.Members {
			var member momentumMemberEnvelope
			if err = json.Unmarshal(memberRaw, &member); err != nil {
				return nil, err
			}
			_, err = tx.Exec(ctx, `INSERT INTO momentum_v1_universe_members(scenario_id,rebalance_sequence,member_sequence,instrument_id,venue_contract_id,evidence_sha256,canonical_member)
				VALUES($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT(scenario_id,rebalance_sequence,member_sequence) DO NOTHING`, value.ID(), source.Sequence, memberSequence, member.InstrumentID, member.VenueContractID, member.EvidenceSHA256, string(memberRaw))
			if err != nil {
				return nil, evaluationWriteError("insert momentum universe member", err)
			}
			if err = repo.stage("momentum_universe_member"); err != nil {
				return nil, err
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit momentum scenario", err)
	}
	got, err := repo.GetScenario(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: momentum scenario conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *MomentumRepo) GetScenario(ctx context.Context, id uuid.UUID) (*momentum.Scenario, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: momentum scenario identity is required")
	}
	var digest string
	var raw []byte
	var policyID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,policy_id FROM momentum_v1_scenarios WHERE id=$1`, id).Scan(&digest, &raw, &policyID)
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
	value, err := momentum.ScenarioFromCanonical(id, digest, raw, policy)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct momentum scenario %s: %w", id, err)
	}
	if err = repo.verifyScenarioRows(ctx, id, raw); err != nil {
		return nil, err
	}
	return value, nil
}

func (repo *MomentumRepo) verifyScenarioRows(ctx context.Context, id uuid.UUID, raw []byte) error {
	var envelope momentumScenarioEnvelope
	_ = json.Unmarshal(raw, &envelope)
	for _, expected := range envelope.Rebalances {
		var source momentumSourceEnvelope
		_ = json.Unmarshal(expected, &source)
		var got []byte
		if err := repo.pool.QueryRow(ctx, `SELECT canonical_rebalance FROM momentum_v1_source_rebalances WHERE scenario_id=$1 AND sequence=$2`, id, source.Sequence).Scan(&got); err != nil || !jsonEqual(got, expected) {
			return fmt.Errorf("postgres: normalized momentum scenario %s does not reconstruct", id)
		}
		rows, err := repo.pool.Query(ctx, `SELECT canonical_member FROM momentum_v1_universe_members WHERE scenario_id=$1 AND rebalance_sequence=$2 ORDER BY member_sequence`, id, source.Sequence)
		if err != nil {
			return err
		}
		index := 0
		for rows.Next() {
			var member []byte
			if rows.Scan(&member) != nil || index >= len(source.Members) || !jsonEqual(member, source.Members[index]) {
				rows.Close()
				return fmt.Errorf("postgres: normalized momentum scenario %s does not reconstruct", id)
			}
			index++
		}
		rows.Close()
		if index != len(source.Members) {
			return fmt.Errorf("postgres: normalized momentum scenario %s does not reconstruct", id)
		}
	}
	return nil
}

func (repo *MomentumRepo) RecordReport(ctx context.Context, value *momentum.Report) (*momentum.Report, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: momentum report is required")
	}
	var envelope momentumReportEnvelope
	if err := json.Unmarshal(value.CanonicalBytes(), &envelope); err != nil {
		return nil, err
	}
	tx, err := repo.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = tx.Exec(ctx, `INSERT INTO momentum_v1_reports(id,schema_name,state,policy_id,policy_sha256,scenario_id,scenario_sha256,initial_capital,evaluation_start,evaluation_end,rebalance_count,regime_count,ending_cash,ending_equity,cumulative_turnover,total_cost,after_cost_total_return,sha256,canonical_bytes,canonical_json,created_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,convert_from($19,'UTF8')::jsonb,$20) ON CONFLICT(id) DO NOTHING`, value.ID(), envelope.Schema, envelope.State, envelope.PolicyID, envelope.PolicySHA256, envelope.ScenarioID, envelope.ScenarioSHA256, envelope.InitialCapital, parseMomentumTime(envelope.EvaluationStart), parseMomentumTime(envelope.EvaluationEnd), len(envelope.Rebalances), len(envelope.Regimes), envelope.EndingCash, envelope.EndingEquity, envelope.CumulativeTurnover, envelope.TotalCost, envelope.AfterCostTotalReturn, value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("insert momentum report", err)
	}
	if err = repo.stage("momentum_report"); err != nil {
		return nil, err
	}
	for _, raw := range envelope.Rebalances {
		var rebalance momentumRebalanceEnvelope
		if err = json.Unmarshal(raw, &rebalance); err != nil {
			return nil, err
		}
		_, err = tx.Exec(ctx, `INSERT INTO momentum_v1_rebalances(report_id,sequence,occurred_at,regime,desired_turnover,applied_turnover,turnover_scale,remaining_target_drift,cost,cash,equity,rank_count,trade_count,holding_count,canonical_rebalance)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15::jsonb) ON CONFLICT(report_id,sequence) DO NOTHING`, value.ID(), rebalance.Sequence, parseMomentumTime(rebalance.OccurredAt), rebalance.Regime, rebalance.DesiredTurnover, rebalance.AppliedTurnover, rebalance.TurnoverScale, rebalance.RemainingTargetDrift, rebalance.Cost, rebalance.Cash, rebalance.Equity, len(rebalance.Ranks), len(rebalance.Trades), len(rebalance.Holdings), string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert momentum rebalance", err)
		}
		if err = repo.insertReportChildren(ctx, tx, value.ID(), rebalance); err != nil {
			return nil, err
		}
	}
	for sequence, raw := range envelope.Regimes {
		_, err = tx.Exec(ctx, `INSERT INTO momentum_v1_regimes(report_id,sequence,canonical_regime) VALUES($1,$2,$3::jsonb) ON CONFLICT(report_id,sequence) DO NOTHING`, value.ID(), sequence, string(raw))
		if err != nil {
			return nil, evaluationWriteError("insert momentum regime", err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit momentum report", err)
	}
	got, err := repo.GetReport(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if got.Digest() != value.Digest() || !bytes.Equal(got.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: momentum report conflict: %w", repository.ErrIdempotencyConflict)
	}
	return got, nil
}

func (repo *MomentumRepo) insertReportChildren(ctx context.Context, tx pgx.Tx, reportID uuid.UUID, rebalance momentumRebalanceEnvelope) error {
	groups := []struct {
		stage, table string
		values       []json.RawMessage
	}{{"momentum_rank", "momentum_v1_ranks", rebalance.Ranks}, {"momentum_trade", "momentum_v1_trades", rebalance.Trades}, {"momentum_holding", "momentum_v1_holdings", rebalance.Holdings}}
	for _, group := range groups {
		for sequence, raw := range group.values {
			_, err := tx.Exec(ctx, fmt.Sprintf(`INSERT INTO %s(report_id,rebalance_sequence,sequence,canonical_value) VALUES($1,$2,$3,$4::jsonb) ON CONFLICT(report_id,rebalance_sequence,sequence) DO NOTHING`, group.table), reportID, rebalance.Sequence, sequence, string(raw))
			if err != nil {
				return evaluationWriteError("insert "+group.stage, err)
			}
			if err = repo.stage(group.stage); err != nil {
				return err
			}
		}
	}
	return repo.stage("momentum_rebalance")
}

func (repo *MomentumRepo) GetReport(ctx context.Context, id uuid.UUID) (*momentum.Report, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: momentum report identity is required")
	}
	var digest string
	var raw []byte
	var policyID, scenarioID uuid.UUID
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,policy_id,scenario_id FROM momentum_v1_reports WHERE id=$1`, id).Scan(&digest, &raw, &policyID, &scenarioID)
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
	value, err := momentum.ReportFromCanonical(id, digest, raw, policy, scenario)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct momentum report %s: %w", id, err)
	}
	if err = repo.verifyReportRows(ctx, id, raw); err != nil {
		return nil, err
	}
	return value, nil
}

func (repo *MomentumRepo) verifyReportRows(ctx context.Context, id uuid.UUID, raw []byte) error {
	var envelope momentumReportEnvelope
	_ = json.Unmarshal(raw, &envelope)
	for _, expected := range envelope.Rebalances {
		var rebalance momentumRebalanceEnvelope
		_ = json.Unmarshal(expected, &rebalance)
		var got []byte
		if err := repo.pool.QueryRow(ctx, `SELECT canonical_rebalance FROM momentum_v1_rebalances WHERE report_id=$1 AND sequence=$2`, id, rebalance.Sequence).Scan(&got); err != nil || !jsonEqual(got, expected) {
			return fmt.Errorf("postgres: normalized momentum report %s does not reconstruct", id)
		}
		for table, values := range map[string][]json.RawMessage{"momentum_v1_ranks": rebalance.Ranks, "momentum_v1_trades": rebalance.Trades, "momentum_v1_holdings": rebalance.Holdings} {
			rows, queryErr := repo.pool.Query(ctx, fmt.Sprintf(`SELECT canonical_value FROM %s WHERE report_id=$1 AND rebalance_sequence=$2 ORDER BY sequence`, table), id, rebalance.Sequence)
			if queryErr != nil {
				return queryErr
			}
			index := 0
			for rows.Next() {
				var child []byte
				if rows.Scan(&child) != nil || index >= len(values) || !jsonEqual(child, values[index]) {
					rows.Close()
					return fmt.Errorf("postgres: normalized momentum report %s does not reconstruct", id)
				}
				index++
			}
			rows.Close()
			if index != len(values) {
				return fmt.Errorf("postgres: normalized momentum report %s does not reconstruct", id)
			}
		}
	}
	return nil
}

func (repo *MomentumRepo) stage(value string) error {
	if repo.afterStage != nil {
		return repo.afterStage(value)
	}
	return nil
}

func parseMomentumTime(value string) time.Time {
	parsed, _ := time.Parse("2006-01-02T15:04:05.000000Z", value)
	return parsed
}
