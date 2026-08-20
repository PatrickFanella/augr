package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// EvaluationRepo persists immutable, result-bound trade and portfolio reports.
// It exposes no latest/best selection and no promotion or deployment mutation.
type EvaluationRepo struct {
	pool       *pgxpool.Pool
	afterStage func(string) error
}

var _ evaluation.Store = (*EvaluationRepo)(nil)

func NewEvaluationRepo(pool *pgxpool.Pool) *EvaluationRepo { return &EvaluationRepo{pool: pool} }

func (repo *EvaluationRepo) GetResult(ctx context.Context, id uuid.UUID) (*experimentrun.Result, error) {
	if repo == nil || repo.pool == nil {
		return nil, fmt.Errorf("postgres: get evaluation result: repository is required")
	}
	return NewExperimentRunRepo(repo.pool).GetResult(ctx, id)
}

func (repo *EvaluationRepo) RegisterPolicy(ctx context.Context, value *evaluation.Policy) (*evaluation.Policy, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: register evaluation policy: repository and policy are required")
	}
	_, err := repo.pool.Exec(ctx, `INSERT INTO evaluation_policy_artifacts(
		id,schema_name,version,frequency,periods_per_year,return_kind,cash_convention,lot_method,recovery_definition,decimal_scale,
		sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'evaluation-policy-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,convert_from($11,'UTF8')::jsonb,$12)
	ON CONFLICT(id) DO NOTHING`, value.ID(), value.Version(), value.Frequency(), value.PeriodsPerYear(), value.ReturnKind(),
		value.CashConvention(), value.LotMethod(), value.RecoveryDefinition(), value.DecimalScale(), value.Digest(), value.CanonicalBytes(), databaseNow())
	if err != nil {
		return nil, evaluationWriteError("register evaluation policy", err)
	}
	persisted, err := repo.GetPolicy(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if persisted.Digest() != value.Digest() || !bytes.Equal(persisted.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: evaluation policy identity reused with changed payload: %w", repository.ErrIdempotencyConflict)
	}
	return persisted, nil
}

func (repo *EvaluationRepo) GetPolicy(ctx context.Context, id uuid.UUID) (*evaluation.Policy, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: get evaluation policy: repository and identity are required")
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM evaluation_policy_artifacts WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get evaluation policy %s: %w", id, err)
	}
	value, err := evaluation.PolicyFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct evaluation policy %s: %w", id, err)
	}
	return value, nil
}

func (repo *EvaluationRepo) RecordEvaluation(ctx context.Context, value *evaluation.Report) (*evaluation.Report, error) {
	if repo == nil || repo.pool == nil || value == nil {
		return nil, fmt.Errorf("postgres: record evaluation: repository and report are required")
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin evaluation: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	createdAt := databaseNow()
	execution := value.Execution()
	_, err = tx.Exec(ctx, `INSERT INTO trade_portfolio_evaluations(
		id,schema_name,state,result_id,result_sha256,experiment_id,program_id,plan_id,account_id,manifest_id,quality_result_id,mode,
		policy_id,policy_sha256,evaluation_start,evaluation_end,open_lot_count,attempted_orders,filled_orders,attempted_quantity,
		filled_quantity,observation_count,closed_trade_count,metric_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'trade-portfolio-evaluation-v1','completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
		$20,$21,$22,$23,$24,convert_from($24,'UTF8')::jsonb,$25) ON CONFLICT(id) DO NOTHING`, value.ID(), value.ResultID(),
		value.ResultDigest(), value.ExperimentID(), value.ProgramID(), value.PlanID(), value.AccountID(), value.ManifestID(), value.QualityResultID(),
		value.Mode(), value.PolicyID(), value.PolicyDigest(), value.EvaluationStart(), value.EvaluationEnd(), value.OpenLotCount(), execution.AttemptedOrders,
		execution.FilledOrders, execution.AttemptedQuantity, execution.FilledQuantity, len(value.Observations()), len(value.ClosedTrades()), len(value.Metrics()),
		value.Digest(), value.CanonicalBytes(), createdAt)
	if err != nil {
		return nil, evaluationWriteError("insert evaluation parent", err)
	}
	if err := repo.stage("evaluation_parent"); err != nil {
		return nil, err
	}
	for sequence, observation := range value.Observations() {
		_, err = tx.Exec(ctx, `INSERT INTO evaluation_observations(
			evaluation_id,sequence,observed_at,equity,benchmark_value,cash_return,gross_exposure,net_exposure,largest_position_weight,
			cumulative_ownership_cost,cumulative_turnover,cumulative_modeled_slippage,cumulative_observed_slippage,evidence_id,evidence_sha256
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15) ON CONFLICT(evaluation_id,sequence) DO NOTHING`,
			value.ID(), sequence, observation.ObservedAt, observation.Equity, observation.BenchmarkValue, observation.CashReturn,
			observation.GrossExposure, observation.NetExposure, observation.LargestPositionWeight, observation.CumulativeOwnershipCost,
			observation.CumulativeTurnover, observation.CumulativeModeledSlippage, observation.CumulativeObservedSlippage,
			observation.EvidenceID, observation.EvidenceSHA256)
		if err != nil {
			return nil, evaluationWriteError("insert evaluation observation", err)
		}
		if err := repo.stage("evaluation_observation"); err != nil {
			return nil, err
		}
	}
	for sequence, trade := range value.ClosedTrades() {
		_, err = tx.Exec(ctx, `INSERT INTO evaluation_closed_trades(
			evaluation_id,sequence,instrument_id,side,quantity,entry_fill_count,exit_fill_count,entry_at,exit_at,entry_price,exit_price,
			entry_fees,exit_fees,other_ownership_cost,gross_pnl,after_cost_pnl
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16) ON CONFLICT(evaluation_id,sequence) DO NOTHING`,
			value.ID(), sequence, trade.InstrumentID, trade.Side, trade.Quantity, len(trade.EntryFillIDs), len(trade.ExitFillIDs), trade.EntryAt,
			trade.ExitAt, trade.EntryPrice, trade.ExitPrice, trade.EntryFees, trade.ExitFees, trade.OtherOwnershipCost, trade.GrossPnL, trade.AfterCostPnL)
		if err != nil {
			return nil, evaluationWriteError("insert evaluation closed trade", err)
		}
		if err := repo.stage("evaluation_trade"); err != nil {
			return nil, err
		}
		for index, fillID := range trade.EntryFillIDs {
			if _, err = tx.Exec(ctx, `INSERT INTO evaluation_trade_fill_ids(evaluation_id,trade_sequence,kind,sequence,fill_id)
				VALUES($1,$2,'entry',$3,$4) ON CONFLICT(evaluation_id,trade_sequence,kind,sequence) DO NOTHING`, value.ID(), sequence, index, fillID); err != nil {
				return nil, evaluationWriteError("insert evaluation entry fill", err)
			}
			if err := repo.stage("evaluation_fill"); err != nil {
				return nil, err
			}
		}
		for index, fillID := range trade.ExitFillIDs {
			if _, err = tx.Exec(ctx, `INSERT INTO evaluation_trade_fill_ids(evaluation_id,trade_sequence,kind,sequence,fill_id)
				VALUES($1,$2,'exit',$3,$4) ON CONFLICT(evaluation_id,trade_sequence,kind,sequence) DO NOTHING`, value.ID(), sequence, index, fillID); err != nil {
				return nil, evaluationWriteError("insert evaluation exit fill", err)
			}
			if err := repo.stage("evaluation_fill"); err != nil {
				return nil, err
			}
		}
	}
	for sequence, metric := range value.Metrics() {
		_, err = tx.Exec(ctx, `INSERT INTO evaluation_metrics(evaluation_id,sequence,section,name,state,value,unit,reason,description)
			VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) ON CONFLICT(evaluation_id,sequence) DO NOTHING`, value.ID(), sequence, metric.Section,
			metric.Name, metric.State, metric.Value, metric.Unit, metric.Reason, metric.Description)
		if err != nil {
			return nil, evaluationWriteError("insert evaluation metric", err)
		}
		if err := repo.stage("evaluation_metric"); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, evaluationWriteError("commit evaluation", err)
	}
	persisted, err := repo.GetEvaluation(ctx, value.ID())
	if err != nil {
		return nil, err
	}
	if persisted.Digest() != value.Digest() || !bytes.Equal(persisted.CanonicalBytes(), value.CanonicalBytes()) {
		return nil, fmt.Errorf("postgres: evaluation identity reused with changed payload: %w", repository.ErrIdempotencyConflict)
	}
	return persisted, nil
}

func (repo *EvaluationRepo) GetEvaluation(ctx context.Context, id uuid.UUID) (*evaluation.Report, error) {
	if repo == nil || repo.pool == nil || id == uuid.Nil {
		return nil, fmt.Errorf("postgres: get evaluation: repository and identity are required")
	}
	var digest string
	var raw []byte
	var resultID, policyID uuid.UUID
	var start, end time.Time
	var openLots int
	var execution evaluation.ExecutionInput
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes,result_id,policy_id,evaluation_start,evaluation_end,open_lot_count,
		attempted_orders,filled_orders,attempted_quantity,filled_quantity FROM trade_portfolio_evaluations WHERE id=$1`, id).
		Scan(&digest, &raw, &resultID, &policyID, &start, &end, &openLots, &execution.AttemptedOrders, &execution.FilledOrders,
			&execution.AttemptedQuantity, &execution.FilledQuantity)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get evaluation %s: %w", id, err)
	}
	start, end = start.UTC(), end.UTC()
	result, err := NewExperimentRunRepo(repo.pool).GetResult(ctx, resultID)
	if err != nil {
		return nil, fmt.Errorf("postgres: load evaluation result: %w", err)
	}
	policy, err := repo.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, fmt.Errorf("postgres: load evaluation policy: %w", err)
	}
	observations, err := repo.loadObservations(ctx, id)
	if err != nil {
		return nil, err
	}
	trades, err := repo.loadTrades(ctx, id)
	if err != nil {
		return nil, err
	}
	reconstructed, err := evaluation.NewReport(evaluation.ReportInput{
		Result: result, Policy: policy, EvaluationStart: start,
		EvaluationEnd: end, OpenLotCount: openLots, Execution: execution, Observations: observations, ClosedTrades: trades,
	})
	if err != nil {
		return nil, fmt.Errorf("postgres: recalculate evaluation %s: %w", id, err)
	}
	metrics, err := repo.loadMetrics(ctx, id)
	if err != nil {
		return nil, err
	}
	if reconstructed.ID() != id || reconstructed.Digest() != digest || !bytes.Equal(reconstructed.CanonicalBytes(), raw) || !reflect.DeepEqual(reconstructed.Metrics(), metrics) {
		return nil, fmt.Errorf("postgres: normalized evaluation %s does not reconstruct", id)
	}
	return reconstructed, nil
}

func (repo *EvaluationRepo) ListResultEvaluations(ctx context.Context, resultID uuid.UUID, limit, offset int) ([]*evaluation.Report, error) {
	return repo.listEvaluations(ctx, `SELECT id FROM trade_portfolio_evaluations WHERE result_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, resultID, limit, offset)
}

func (repo *EvaluationRepo) ListExperimentEvaluations(ctx context.Context, experimentID uuid.UUID, limit, offset int) ([]*evaluation.Report, error) {
	return repo.listEvaluations(ctx, `SELECT id FROM trade_portfolio_evaluations WHERE experiment_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, experimentID, limit, offset)
}

func (repo *EvaluationRepo) listEvaluations(ctx context.Context, query string, parent uuid.UUID, limit, offset int) ([]*evaluation.Report, error) {
	if repo == nil || repo.pool == nil || parent == uuid.Nil || limit <= 0 || limit > 1000 || offset < 0 {
		return nil, fmt.Errorf("postgres: list evaluations: valid parent and pagination are required")
	}
	rows, err := repo.pool.Query(ctx, query, parent, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list evaluations: %w", err)
	}
	defer rows.Close()
	ids := make([]uuid.UUID, 0)
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	values := make([]*evaluation.Report, 0, len(ids))
	for _, id := range ids {
		value, err := repo.GetEvaluation(ctx, id)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, nil
}

func (repo *EvaluationRepo) loadObservations(ctx context.Context, id uuid.UUID) ([]evaluation.ObservationInput, error) {
	rows, err := repo.pool.Query(ctx, `SELECT observed_at,equity,benchmark_value,cash_return,gross_exposure,net_exposure,largest_position_weight,
		cumulative_ownership_cost,cumulative_turnover,cumulative_modeled_slippage,cumulative_observed_slippage,evidence_id,evidence_sha256
		FROM evaluation_observations WHERE evaluation_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]evaluation.ObservationInput, 0)
	for rows.Next() {
		var value evaluation.ObservationInput
		if err := rows.Scan(&value.ObservedAt, &value.Equity, &value.BenchmarkValue, &value.CashReturn, &value.GrossExposure,
			&value.NetExposure, &value.LargestPositionWeight, &value.CumulativeOwnershipCost, &value.CumulativeTurnover,
			&value.CumulativeModeledSlippage, &value.CumulativeObservedSlippage, &value.EvidenceID, &value.EvidenceSHA256); err != nil {
			return nil, err
		}
		value.ObservedAt = value.ObservedAt.UTC()
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *EvaluationRepo) loadTrades(ctx context.Context, id uuid.UUID) ([]evaluation.ClosedTradeInput, error) {
	rows, err := repo.pool.Query(ctx, `SELECT sequence,instrument_id,side,quantity,entry_at,exit_at,entry_price,exit_price,entry_fees,exit_fees,
		other_ownership_cost,gross_pnl,after_cost_pnl FROM evaluation_closed_trades WHERE evaluation_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]evaluation.ClosedTradeInput, 0)
	sequences := make([]int, 0)
	for rows.Next() {
		var sequence int
		var value evaluation.ClosedTradeInput
		if err := rows.Scan(&sequence, &value.InstrumentID, &value.Side, &value.Quantity, &value.EntryAt, &value.ExitAt,
			&value.EntryPrice, &value.ExitPrice, &value.EntryFees, &value.ExitFees, &value.OtherOwnershipCost, &value.GrossPnL, &value.AfterCostPnL); err != nil {
			return nil, err
		}
		value.EntryAt, value.ExitAt = value.EntryAt.UTC(), value.ExitAt.UTC()
		sequences = append(sequences, sequence)
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for index := range values {
		values[index].EntryFillIDs, err = repo.loadFillIDs(ctx, id, sequences[index], "entry")
		if err != nil {
			return nil, err
		}
		values[index].ExitFillIDs, err = repo.loadFillIDs(ctx, id, sequences[index], "exit")
		if err != nil {
			return nil, err
		}
	}
	return values, nil
}

func (repo *EvaluationRepo) loadFillIDs(ctx context.Context, id uuid.UUID, tradeSequence int, kind string) ([]uuid.UUID, error) {
	rows, err := repo.pool.Query(ctx, `SELECT fill_id FROM evaluation_trade_fill_ids WHERE evaluation_id=$1 AND trade_sequence=$2 AND kind=$3 ORDER BY sequence`, id, tradeSequence, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]uuid.UUID, 0)
	for rows.Next() {
		var value uuid.UUID
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *EvaluationRepo) loadMetrics(ctx context.Context, id uuid.UUID) ([]evaluation.Metric, error) {
	rows, err := repo.pool.Query(ctx, `SELECT section,name,state,value,unit,reason,description FROM evaluation_metrics WHERE evaluation_id=$1 ORDER BY sequence`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	values := make([]evaluation.Metric, 0)
	for rows.Next() {
		var value evaluation.Metric
		if err := rows.Scan(&value.Section, &value.Name, &value.State, &value.Value, &value.Unit, &value.Reason, &value.Description); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *EvaluationRepo) stage(name string) error {
	if repo.afterStage == nil {
		return nil
	}
	if err := repo.afterStage(name); err != nil {
		return fmt.Errorf("postgres: injected evaluation failure after %s: %w", name, err)
	}
	return nil
}

func evaluationWriteError(operation string, err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == "23505" || pgErr.Code == "23514" || pgErr.Code == "23503" || pgErr.Code == "P0001") {
		return fmt.Errorf("postgres: %s: %v: %w", operation, err, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
