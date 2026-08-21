package postgres

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// ExperimentRunRepo persists immutable program, replay-plan, attempt, and
// result evidence. Execution and economic writes are delegated to the common
// append-only repositories so experiment runs cannot create a parallel ledger.
type ExperimentRunRepo struct {
	pool       *pgxpool.Pool
	lifecycle  *ExecutionLifecycleRepo
	ledger     *LedgerRepo
	afterStage func(string) error
}

var _ repository.ExperimentRunRepository = (*ExperimentRunRepo)(nil)

func NewExperimentRunRepo(pool *pgxpool.Pool) *ExperimentRunRepo {
	return &ExperimentRunRepo{pool: pool, lifecycle: NewExecutionLifecycleRepo(pool), ledger: NewLedgerRepo(pool)}
}

func (repo *ExperimentRunRepo) RecordProgram(ctx context.Context, value *experimentrun.ProgramIdentity) (*experimentrun.ProgramIdentity, error) {
	if err := repo.ready("record experiment program"); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("postgres: record experiment program: program is required")
	}
	createdAt := databaseNow()
	_, err := repo.pool.Exec(ctx, `INSERT INTO experiment_programs(
		id,schema_name,version_id,version_sha256,compiler_kind,compiler_version,source_commit,source_tree_sha256,
		decision_contract,adapter_kind,adapter_version,adapter_sha256,runner_contract,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-program-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,convert_from($14,'UTF8')::jsonb,$15)
	ON CONFLICT(id) DO NOTHING`, value.ID(), value.VersionID(), value.VersionSHA256(), value.CompilerKind(), value.CompilerVersion(),
		value.SourceCommit(), value.SourceTreeSHA256(), value.DecisionContract(), value.AdapterKind(), value.AdapterVersion(),
		value.AdapterSHA256(), value.RunnerContract(), value.Digest(), value.CanonicalBytes(), createdAt)
	if err != nil {
		return nil, experimentRunWriteError("record experiment program", err)
	}
	persisted, err := repo.GetProgram(ctx, value.ID())
	if err != nil {
		return nil, fmt.Errorf("postgres: reload experiment program: %w", err)
	}
	if !sameProgramIdentity(persisted, value) {
		return nil, experimentRunConflict("experiment program identity reused with mismatched payload")
	}
	return persisted, nil
}

func (repo *ExperimentRunRepo) GetProgram(ctx context.Context, id uuid.UUID) (*experimentrun.ProgramIdentity, error) {
	if err := repo.ready("get experiment program"); err != nil {
		return nil, err
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM experiment_programs WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get experiment program %s: %w", id, err)
	}
	value, err := experimentrun.ProgramIdentityFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct experiment program %s: %w", id, err)
	}
	if err := repo.verifyProgramRow(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (repo *ExperimentRunRepo) RecordPlan(ctx context.Context, value *experimentrun.Plan) (*experimentrun.Plan, error) {
	if err := repo.ready("record experiment replay plan"); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, fmt.Errorf("postgres: record experiment replay plan: plan is required")
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin experiment replay plan: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	createdAt := databaseNow()
	_, err = tx.Exec(ctx, `INSERT INTO experiment_replay_plans(
		id,schema_name,experiment_id,program_id,account_id,capital_state_id,capital_state_sha256,capital_projection_checkpoint_id,
		capital_state_bytes,capital_state,manifest_id,manifest_sha256,evaluation_start,evaluation_end,seed,mode,
		step_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-replay-plan-v1',$2,$3,$4,$5,$6,$7,$8,convert_from($8,'UTF8')::jsonb,$9,$10,$11,$12,$13,$14,
		$15,$16,$17,convert_from($17,'UTF8')::jsonb,$18) ON CONFLICT(id) DO NOTHING`,
		value.ID(), value.ExperimentID(), value.ProgramID(), value.AccountID(), value.CapitalStateID(), value.CapitalStateSHA256(),
		value.CapitalProjectionCheckpointID(), value.CapitalStateBytes(), value.ManifestID(), value.ManifestSHA256(), value.EvaluationStart(),
		value.EvaluationEnd(), value.Seed(), value.Mode(), value.StepCount(), value.Digest(), value.CanonicalBytes(), createdAt)
	if err != nil {
		return nil, experimentRunWriteError("insert experiment replay plan", err)
	}
	if err := repo.stage("plan_parent"); err != nil {
		return nil, err
	}
	for sequence, step := range value.Steps() {
		var instrumentID, contractID any
		var side, orderType, tif, quantity, limitPrice, stopPrice any
		var decisionAt, routeAt, intentKey, orderKey, intentID, orderID any
		if step.Intent != nil {
			instrumentID, contractID = step.Intent.InstrumentID, step.Intent.VenueContractID
			side, orderType, tif, quantity = step.Intent.Side, step.Intent.OrderType, step.Intent.TimeInForce, step.Intent.Quantity
			limitPrice, stopPrice = step.Intent.LimitPrice, step.Intent.StopPrice
			decisionAt, routeAt = step.Intent.DecisionAt, step.Intent.RouteAt
			intentKey, orderKey = value.IntentIdempotencyKey(sequence), value.OrderIdempotencyKey(sequence)
			intentID, orderID = value.IntentID(sequence), value.OrderID(sequence)
		}
		_, err = tx.Exec(ctx, `INSERT INTO experiment_replay_plan_steps(
			plan_id,sequence,partition_content_sha256,observation_source_key,observation_content_sha256,available_at,
			decision_bytes,decision,decision_sha256,action,rejection_code,instrument_id,venue_contract_id,side,order_type,time_in_force,
			quantity,limit_price,stop_price,decision_at,route_at,intent_idempotency_key,order_idempotency_key,intent_id,order_id
		) VALUES($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::jsonb,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)
		ON CONFLICT(plan_id,sequence) DO NOTHING`, value.ID(), sequence, step.PartitionContentSHA256, step.ObservationSourceKey,
			step.ObservationContentSHA256, step.AvailableAt, step.Decision, value.DecisionSHA256(sequence), step.Action, step.RejectionCode,
			instrumentID, contractID, side, orderType, tif, quantity, limitPrice, stopPrice, decisionAt, routeAt, intentKey, orderKey, intentID, orderID)
		if err != nil {
			return nil, experimentRunWriteError("insert experiment replay plan step", err)
		}
		if err := repo.stage("plan_step"); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, experimentRunWriteError("commit experiment replay plan", err)
	}
	persisted, err := repo.GetPlan(ctx, value.ID())
	if err != nil {
		return nil, fmt.Errorf("postgres: reload experiment replay plan: %w", err)
	}
	if !samePlan(persisted, value) {
		return nil, experimentRunConflict("experiment replay plan identity reused with mismatched payload")
	}
	return persisted, nil
}

func (repo *ExperimentRunRepo) GetPlan(ctx context.Context, id uuid.UUID) (*experimentrun.Plan, error) {
	if err := repo.ready("get experiment replay plan"); err != nil {
		return nil, err
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM experiment_replay_plans WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get experiment replay plan %s: %w", id, err)
	}
	value, err := experimentrun.PlanFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct experiment replay plan %s: %w", id, err)
	}
	if err := repo.verifyPlanRows(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func (repo *ExperimentRunRepo) RecordAttemptEvent(ctx context.Context, experimentID, _ uuid.UUID, value *experimentrun.AttemptEvent) (*experimentrun.AttemptEvent, error) {
	if err := repo.ready("record experiment attempt event"); err != nil {
		return nil, err
	}
	if value == nil || experimentID == uuid.Nil {
		return nil, fmt.Errorf("postgres: record experiment attempt event: experiment and event are required")
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("postgres: begin experiment attempt event: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if value.Type() == experimentrun.AttemptStarted {
		_, err = tx.Exec(ctx, `INSERT INTO experiment_run_attempts(id,experiment_id,created_at) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING`,
			value.AttemptID(), experimentID, value.OccurredAt())
		if err != nil {
			return nil, experimentRunWriteError("insert experiment attempt", err)
		}
	}
	var storedExperimentID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT experiment_id FROM experiment_run_attempts WHERE id=$1 FOR UPDATE`, value.AttemptID()).Scan(&storedExperimentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, repository.ErrNotFound
		}
		return nil, fmt.Errorf("postgres: lock experiment attempt: %w", err)
	}
	if storedExperimentID != experimentID {
		return nil, experimentRunConflict("experiment attempt identity reused by another experiment")
	}
	if value.Sequence() == 1 {
		existing, loadErr := getAttemptEventTx(ctx, tx, value.AttemptID(), 1)
		if loadErr == nil {
			if !sameAttemptEvent(existing, value) {
				return nil, experimentRunConflict("experiment attempt terminal event conflicts with accepted evidence")
			}
			return existing, nil
		}
		if !errors.Is(loadErr, repository.ErrNotFound) {
			return nil, loadErr
		}
	}
	if err = insertAttemptEvent(ctx, tx, experimentID, value); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, experimentRunWriteError("commit experiment attempt event", err)
	}
	persisted, err := repo.getAttemptEvent(ctx, value.AttemptID(), value.Sequence())
	if err != nil {
		return nil, err
	}
	if !sameAttemptEvent(persisted, value) {
		return nil, experimentRunConflict("experiment attempt event identity reused with mismatched payload")
	}
	return persisted, nil
}

func (repo *ExperimentRunRepo) RecordCompletedResult(ctx context.Context, experimentID uuid.UUID, value *experimentrun.Result, event *experimentrun.AttemptEvent) (*experimentrun.Result, *experimentrun.AttemptEvent, error) {
	if err := repo.ready("record completed experiment result"); err != nil {
		return nil, nil, err
	}
	if value == nil || event == nil || experimentID == uuid.Nil || value.ExperimentID() != experimentID ||
		event.Type() != experimentrun.AttemptCompleted || event.ResultID() != value.ID() {
		return nil, nil, fmt.Errorf("postgres: record completed experiment result: exact completed graph is required")
	}
	tx, err := repo.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, nil, fmt.Errorf("postgres: begin completed experiment result: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var storedExperimentID uuid.UUID
	if err = tx.QueryRow(ctx, `SELECT experiment_id FROM experiment_run_attempts WHERE id=$1 FOR UPDATE`, event.AttemptID()).Scan(&storedExperimentID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, repository.ErrNotFound
		}
		return nil, nil, fmt.Errorf("postgres: lock completed experiment attempt: %w", err)
	}
	if storedExperimentID != experimentID {
		return nil, nil, experimentRunConflict("completed result experiment differs from attempt")
	}
	existingEvent, loadErr := getAttemptEventTx(ctx, tx, event.AttemptID(), 1)
	if loadErr == nil {
		if !sameAttemptEvent(existingEvent, event) {
			return nil, nil, experimentRunConflict("experiment attempt already has different terminal evidence")
		}
		existingResult, resultErr := repo.GetResult(ctx, value.ID())
		if resultErr != nil {
			return nil, nil, resultErr
		}
		if !sameResult(existingResult, value) {
			return nil, nil, experimentRunConflict("completed experiment result differs from accepted evidence")
		}
		return existingResult, existingEvent, nil
	}
	if !errors.Is(loadErr, repository.ErrNotFound) {
		return nil, nil, loadErr
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1::text,0))`, value.ID()); err != nil {
		return nil, nil, fmt.Errorf("postgres: lock completed experiment result identity: %w", err)
	}
	existingResult, resultLoadErr := getResultTx(ctx, tx, value.ID())
	switch {
	case resultLoadErr == nil:
		if !sameResult(existingResult, value) {
			return nil, nil, experimentRunConflict("completed experiment result identity differs from accepted evidence")
		}
	case errors.Is(resultLoadErr, repository.ErrNotFound):
		if err = repo.insertResultGraph(ctx, tx, value, event.OccurredAt()); err != nil {
			return nil, nil, err
		}
	default:
		return nil, nil, resultLoadErr
	}
	if err := repo.stage("result_graph"); err != nil {
		return nil, nil, err
	}
	if err = insertAttemptEvent(ctx, tx, experimentID, event); err != nil {
		return nil, nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, nil, experimentRunWriteError("commit completed experiment result", err)
	}
	persistedResult, err := repo.GetResult(ctx, value.ID())
	if err != nil {
		return nil, nil, err
	}
	persistedEvent, err := repo.getAttemptEvent(ctx, event.AttemptID(), 1)
	if err != nil {
		return nil, nil, err
	}
	if !sameResult(persistedResult, value) || !sameAttemptEvent(persistedEvent, event) {
		return nil, nil, experimentRunConflict("completed experiment graph differs after reload")
	}
	return persistedResult, persistedEvent, nil
}

func (repo *ExperimentRunRepo) insertResultGraph(ctx context.Context, tx pgx.Tx, value *experimentrun.Result, createdAt time.Time) error {
	metrics := value.Metrics()
	_, err := tx.Exec(ctx, `INSERT INTO experiment_run_results(
		id,schema_name,state,experiment_id,program_id,plan_id,account_id,manifest_id,quality_result_id,simulation_policy_version,
		capital_policy_version,mode,step_count,noop_count,rejected_count,intent_count,order_count,transition_count,fill_count,
		filled_quantity,fee_total,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-run-result-v1','completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,
		convert_from($21,'UTF8')::jsonb,$22)`, value.ID(), value.ExperimentID(), value.ProgramID(), value.PlanID(), value.AccountID(),
		value.ManifestID(), value.QualityResultID(), value.SimulationPolicyVersion(), value.CapitalPolicyVersion(), value.Mode(), metrics.StepCount,
		metrics.NoopCount, metrics.RejectedCount, metrics.IntentCount, metrics.OrderCount, metrics.TransitionCount, metrics.FillCount,
		metrics.FilledQuantity, metrics.FeeTotal, value.Digest(), value.CanonicalBytes(), createdAt)
	if err != nil {
		return experimentRunWriteError("insert completed experiment result", err)
	}
	if err := repo.stage("result_parent"); err != nil {
		return err
	}
	for sequence, outcome := range value.Outcomes() {
		var intentID, orderID any
		if outcome.IntentID != uuid.Nil {
			intentID = outcome.IntentID
		}
		if outcome.OrderID != uuid.Nil {
			orderID = outcome.OrderID
		}
		_, err = tx.Exec(ctx, `INSERT INTO experiment_run_step_outcomes(
			result_id,sequence,action,decision_sha256,intent_id,order_id,transition_count,fill_count,filled_quantity,fee_total,aggregate_sha256,outcome_sha256
		) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, value.ID(), sequence, outcome.Action, outcome.DecisionSHA256,
			intentID, orderID, len(outcome.TransitionIDs), len(outcome.FillIDs), outcome.FilledQuantity, outcome.FeeTotal,
			outcome.AggregateSHA256, outcome.OutcomeSHA256)
		if err != nil {
			return experimentRunWriteError("insert experiment result outcome", err)
		}
		if err := repo.stage("result_outcome"); err != nil {
			return err
		}
		for index, transitionID := range outcome.TransitionIDs {
			if _, err = tx.Exec(ctx, `INSERT INTO experiment_run_transition_ids(result_id,step_sequence,sequence,transition_id) VALUES($1,$2,$3,$4)`,
				value.ID(), sequence, index, transitionID); err != nil {
				return experimentRunWriteError("insert experiment result transition", err)
			}
			if err := repo.stage("result_transition"); err != nil {
				return err
			}
		}
		for index, fillID := range outcome.FillIDs {
			if _, err = tx.Exec(ctx, `INSERT INTO experiment_run_fill_ids(result_id,step_sequence,sequence,fill_id) VALUES($1,$2,$3,$4)`,
				value.ID(), sequence, index, fillID); err != nil {
				return experimentRunWriteError("insert experiment result fill", err)
			}
			if err := repo.stage("result_fill"); err != nil {
				return err
			}
		}
	}
	return nil
}

func insertAttemptEvent(ctx context.Context, tx pgx.Tx, experimentID uuid.UUID, value *experimentrun.AttemptEvent) error {
	var resultID any
	if value.ResultID() != uuid.Nil {
		resultID = value.ResultID()
	}
	command, err := tx.Exec(ctx, `INSERT INTO experiment_run_attempt_events(
		id,schema_name,attempt_id,experiment_id,sequence,type,occurred_at,result_id,error_code,error_sha256,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-attempt-event-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,convert_from($11,'UTF8')::jsonb,$6)
	ON CONFLICT(attempt_id,sequence) DO NOTHING`, value.ID(), value.AttemptID(), experimentID, value.Sequence(), value.Type(),
		value.OccurredAt(), resultID, value.ErrorCode(), value.ErrorSHA256(), value.Digest(), value.CanonicalBytes())
	if err != nil {
		return experimentRunWriteError("insert experiment attempt event", err)
	}
	if command.RowsAffected() == 0 {
		existing, loadErr := getAttemptEventTx(ctx, tx, value.AttemptID(), value.Sequence())
		if loadErr != nil {
			return loadErr
		}
		if !sameAttemptEvent(existing, value) {
			return experimentRunConflict("experiment attempt event conflicts with accepted evidence")
		}
	}
	return nil
}

func (repo *ExperimentRunRepo) GetAttemptEvents(ctx context.Context, attemptID uuid.UUID) ([]*experimentrun.AttemptEvent, error) {
	if err := repo.ready("list experiment attempt events"); err != nil {
		return nil, err
	}
	rows, err := repo.pool.Query(ctx, `SELECT id,sha256,canonical_bytes FROM experiment_run_attempt_events WHERE attempt_id=$1 ORDER BY sequence`, attemptID)
	if err != nil {
		return nil, fmt.Errorf("postgres: list experiment attempt events: %w", err)
	}
	defer rows.Close()
	values := make([]*experimentrun.AttemptEvent, 0, 2)
	for rows.Next() {
		var id uuid.UUID
		var digest string
		var raw []byte
		if err := rows.Scan(&id, &digest, &raw); err != nil {
			return nil, err
		}
		value, err := experimentrun.AttemptEventFromCanonical(id, digest, raw)
		if err != nil {
			return nil, fmt.Errorf("postgres: reconstruct experiment attempt event %s: %w", id, err)
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, repository.ErrNotFound
	}
	return values, nil
}

func (repo *ExperimentRunRepo) getAttemptEvent(ctx context.Context, attemptID uuid.UUID, sequence int) (*experimentrun.AttemptEvent, error) {
	var id uuid.UUID
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM experiment_run_attempt_events WHERE attempt_id=$1 AND sequence=$2`, attemptID, sequence).Scan(&id, &digest, &raw)
	return reconstructAttemptEvent(id, digest, raw, err)
}

func getAttemptEventTx(ctx context.Context, tx pgx.Tx, attemptID uuid.UUID, sequence int) (*experimentrun.AttemptEvent, error) {
	var id uuid.UUID
	var digest string
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT id,sha256,canonical_bytes FROM experiment_run_attempt_events WHERE attempt_id=$1 AND sequence=$2`, attemptID, sequence).Scan(&id, &digest, &raw)
	return reconstructAttemptEvent(id, digest, raw, err)
}

func reconstructAttemptEvent(id uuid.UUID, digest string, raw []byte, err error) (*experimentrun.AttemptEvent, error) {
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get experiment attempt event: %w", err)
	}
	value, err := experimentrun.AttemptEventFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct experiment attempt event %s: %w", id, err)
	}
	return value, nil
}

func (repo *ExperimentRunRepo) GetResult(ctx context.Context, id uuid.UUID) (*experimentrun.Result, error) {
	if err := repo.ready("get experiment result"); err != nil {
		return nil, err
	}
	var digest string
	var raw []byte
	err := repo.pool.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM experiment_run_results WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get experiment result %s: %w", id, err)
	}
	value, err := experimentrun.ResultFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct experiment result %s: %w", id, err)
	}
	if err := repo.verifyResultRows(ctx, value); err != nil {
		return nil, err
	}
	return value, nil
}

func getResultTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) (*experimentrun.Result, error) {
	var digest string
	var raw []byte
	err := tx.QueryRow(ctx, `SELECT sha256,canonical_bytes FROM experiment_run_results WHERE id=$1`, id).Scan(&digest, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, repository.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("postgres: get experiment result in transaction: %w", err)
	}
	value, err := experimentrun.ResultFromCanonical(id, digest, raw)
	if err != nil {
		return nil, fmt.Errorf("postgres: reconstruct experiment result in transaction: %w", err)
	}
	return value, nil
}

func (repo *ExperimentRunRepo) ListExperimentResults(ctx context.Context, experimentID uuid.UUID, limit, offset int) ([]*experimentrun.Result, error) {
	if err := repo.ready("list experiment results"); err != nil {
		return nil, err
	}
	if experimentID == uuid.Nil || limit <= 0 || limit > 1000 || offset < 0 {
		return nil, fmt.Errorf("postgres: list experiment results: invalid pagination")
	}
	rows, err := repo.pool.Query(ctx, `SELECT id,sha256,canonical_bytes FROM experiment_run_results WHERE experiment_id=$1 ORDER BY created_at,id LIMIT $2 OFFSET $3`, experimentID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("postgres: list experiment results: %w", err)
	}
	defer rows.Close()
	values := make([]*experimentrun.Result, 0)
	for rows.Next() {
		var id uuid.UUID
		var digest string
		var raw []byte
		if err := rows.Scan(&id, &digest, &raw); err != nil {
			return nil, err
		}
		value, err := experimentrun.ResultFromCanonical(id, digest, raw)
		if err != nil {
			return nil, fmt.Errorf("postgres: reconstruct experiment result %s: %w", id, err)
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

func (repo *ExperimentRunRepo) ProposeExecutionIntent(ctx context.Context, value *lifecycle.Aggregate) (*lifecycle.Aggregate, error) {
	return repo.lifecycle.ProposeExecutionIntent(ctx, value)
}

func (repo *ExperimentRunRepo) RecordEconomicSourceEvent(ctx context.Context, value *ledger.EconomicSourceEvent) (*ledger.EconomicSourceEvent, error) {
	return repo.ledger.RecordEconomicSourceEvent(ctx, value)
}

func (repo *ExperimentRunRepo) ApplyExecutionFill(ctx context.Context, accountID uuid.UUID, value *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	return repo.lifecycle.ApplyExecutionFill(ctx, accountID, value)
}

func (repo *ExperimentRunRepo) ApplyExecutionTransition(ctx context.Context, accountID uuid.UUID, value *lifecycle.Transition) (*lifecycle.Aggregate, error) {
	return repo.lifecycle.ApplyExecutionTransition(ctx, accountID, value)
}

func (repo *ExperimentRunRepo) GetExecutionLifecycle(ctx context.Context, accountID, intentID uuid.UUID) (*lifecycle.Aggregate, error) {
	return repo.lifecycle.GetExecutionLifecycle(ctx, accountID, intentID)
}

func (repo *ExperimentRunRepo) verifyProgramRow(ctx context.Context, value *experimentrun.ProgramIdentity) error {
	var versionID uuid.UUID
	var versionSHA, compilerKind, compilerVersion, sourceCommit, sourceTreeSHA, decisionContract string
	var adapterKind, adapterVersion, adapterSHA, runnerContract string
	err := repo.pool.QueryRow(ctx, `SELECT version_id,version_sha256,compiler_kind,compiler_version,source_commit,source_tree_sha256,
		decision_contract,adapter_kind,adapter_version,adapter_sha256,runner_contract FROM experiment_programs WHERE id=$1`, value.ID()).Scan(
		&versionID, &versionSHA, &compilerKind, &compilerVersion, &sourceCommit, &sourceTreeSHA, &decisionContract,
		&adapterKind, &adapterVersion, &adapterSHA, &runnerContract,
	)
	if err != nil {
		return fmt.Errorf("postgres: load normalized experiment program %s: %w", value.ID(), err)
	}
	if versionID != value.VersionID() || versionSHA != value.VersionSHA256() || compilerKind != value.CompilerKind() ||
		compilerVersion != value.CompilerVersion() || sourceCommit != value.SourceCommit() || sourceTreeSHA != value.SourceTreeSHA256() ||
		decisionContract != value.DecisionContract() || adapterKind != value.AdapterKind() || adapterVersion != value.AdapterVersion() ||
		adapterSHA != value.AdapterSHA256() || runnerContract != value.RunnerContract() {
		return experimentRunConflict("normalized experiment program differs from canonical evidence")
	}
	return nil
}

func (repo *ExperimentRunRepo) verifyPlanRows(ctx context.Context, value *experimentrun.Plan) error {
	var experimentID, programID, accountID, stateID, checkpointID, manifestID uuid.UUID
	var stateSHA, manifestSHA, mode string
	var stateBytes []byte
	var start, end time.Time
	var seed int64
	var stepCount int
	err := repo.pool.QueryRow(ctx, `SELECT experiment_id,program_id,account_id,capital_state_id,capital_state_sha256,
		capital_projection_checkpoint_id,capital_state_bytes,manifest_id,manifest_sha256,evaluation_start,evaluation_end,seed,mode,step_count
		FROM experiment_replay_plans WHERE id=$1`, value.ID()).Scan(&experimentID, &programID, &accountID, &stateID, &stateSHA,
		&checkpointID, &stateBytes, &manifestID, &manifestSHA, &start, &end, &seed, &mode, &stepCount)
	if err != nil {
		return fmt.Errorf("postgres: load normalized experiment replay plan %s: %w", value.ID(), err)
	}
	if experimentID != value.ExperimentID() || programID != value.ProgramID() || accountID != value.AccountID() ||
		stateID != value.CapitalStateID() || stateSHA != value.CapitalStateSHA256() || checkpointID != value.CapitalProjectionCheckpointID() ||
		!bytes.Equal(stateBytes, value.CapitalStateBytes()) || manifestID != value.ManifestID() || manifestSHA != value.ManifestSHA256() ||
		!start.Equal(value.EvaluationStart()) || !end.Equal(value.EvaluationEnd()) || seed != value.Seed() || mode != string(value.Mode()) || stepCount != value.StepCount() {
		return experimentRunConflict("normalized experiment replay plan parent differs from canonical evidence")
	}
	rows, err := repo.pool.Query(ctx, `SELECT sequence,partition_content_sha256,observation_source_key,observation_content_sha256,available_at,
		decision_bytes,decision_sha256,action,rejection_code,instrument_id,venue_contract_id,side,order_type,time_in_force,
		CASE WHEN quantity IS NULL THEN NULL ELSE trim_scale(quantity)::text END,
		CASE WHEN limit_price IS NULL THEN NULL ELSE trim_scale(limit_price)::text END,
		CASE WHEN stop_price IS NULL THEN NULL ELSE trim_scale(stop_price)::text END,
		decision_at,route_at,intent_idempotency_key,order_idempotency_key,intent_id,order_id
		FROM experiment_replay_plan_steps WHERE plan_id=$1 ORDER BY sequence`, value.ID())
	if err != nil {
		return fmt.Errorf("postgres: load normalized experiment replay plan steps: %w", err)
	}
	defer rows.Close()
	steps := value.Steps()
	sequenceSeen := 0
	for rows.Next() {
		var sequence int
		var partitionSHA, sourceKey, observationSHA, decisionSHA, action, rejection string
		var available time.Time
		var decision []byte
		var instrumentID, contractID, intentID, orderID *uuid.UUID
		var side, orderType, tif, quantity, limitPrice, stopPrice, intentKey, orderKey *string
		var decisionAt, routeAt *time.Time
		if err := rows.Scan(&sequence, &partitionSHA, &sourceKey, &observationSHA, &available, &decision, &decisionSHA, &action, &rejection,
			&instrumentID, &contractID, &side, &orderType, &tif, &quantity, &limitPrice, &stopPrice, &decisionAt, &routeAt,
			&intentKey, &orderKey, &intentID, &orderID); err != nil {
			return fmt.Errorf("postgres: scan normalized experiment replay plan step: %w", err)
		}
		if sequence != sequenceSeen || sequence >= len(steps) {
			return experimentRunConflict("normalized experiment replay plan step order differs from canonical evidence")
		}
		expected := steps[sequence]
		if partitionSHA != expected.PartitionContentSHA256 || sourceKey != expected.ObservationSourceKey ||
			observationSHA != expected.ObservationContentSHA256 || !available.Equal(expected.AvailableAt) || !bytes.Equal(decision, expected.Decision) ||
			decisionSHA != value.DecisionSHA256(sequence) || action != string(expected.Action) || rejection != expected.RejectionCode ||
			!samePlanIntentColumns(expected.Intent, instrumentID, contractID, side, orderType, tif, quantity, limitPrice, stopPrice, decisionAt, routeAt) ||
			!sameOptionalString(intentKey, value.IntentIdempotencyKey(sequence)) || !sameOptionalString(orderKey, value.OrderIdempotencyKey(sequence)) ||
			!sameExperimentOptionalUUID(intentID, value.IntentID(sequence)) || !sameExperimentOptionalUUID(orderID, value.OrderID(sequence)) {
			return experimentRunConflict("normalized experiment replay plan step differs from canonical evidence")
		}
		sequenceSeen++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if sequenceSeen != len(steps) {
		return experimentRunConflict("normalized experiment replay plan omits canonical steps")
	}
	return nil
}

func samePlanIntentColumns(expected *experimentrun.IntentSpecInput, instrumentID, contractID *uuid.UUID, side, orderType, tif, quantity, limitPrice, stopPrice *string, decisionAt, routeAt *time.Time) bool {
	if expected == nil {
		return instrumentID == nil && contractID == nil && side == nil && orderType == nil && tif == nil && quantity == nil &&
			limitPrice == nil && stopPrice == nil && decisionAt == nil && routeAt == nil
	}
	return instrumentID != nil && *instrumentID == expected.InstrumentID && contractID != nil && *contractID == expected.VenueContractID &&
		side != nil && *side == expected.Side && orderType != nil && *orderType == expected.OrderType && tif != nil && *tif == expected.TimeInForce &&
		quantity != nil && *quantity == expected.Quantity && sameStringPointer(limitPrice, expected.LimitPrice) && sameStringPointer(stopPrice, expected.StopPrice) &&
		decisionAt != nil && decisionAt.Equal(expected.DecisionAt) && routeAt != nil && routeAt.Equal(expected.RouteAt)
}

func sameStringPointer(left, right *string) bool {
	return left == nil && right == nil || left != nil && right != nil && *left == *right
}

func sameOptionalString(value *string, expected string) bool {
	return expected == "" && value == nil || value != nil && *value == expected
}

func sameExperimentOptionalUUID(value *uuid.UUID, expected uuid.UUID) bool {
	return expected == uuid.Nil && value == nil || value != nil && *value == expected
}

func (repo *ExperimentRunRepo) verifyResultRows(ctx context.Context, value *experimentrun.Result) error {
	var experimentID, programID, planID, accountID, manifestID, qualityID uuid.UUID
	var simulationVersion, capitalVersion, mode, quantity, fees string
	var metrics experimentrun.Metrics
	err := repo.pool.QueryRow(ctx, `SELECT experiment_id,program_id,plan_id,account_id,manifest_id,quality_result_id,
		simulation_policy_version,capital_policy_version,mode,step_count,noop_count,rejected_count,intent_count,order_count,
		transition_count,fill_count,trim_scale(filled_quantity)::text,trim_scale(fee_total)::text FROM experiment_run_results WHERE id=$1`, value.ID()).Scan(
		&experimentID, &programID, &planID, &accountID, &manifestID, &qualityID, &simulationVersion, &capitalVersion, &mode,
		&metrics.StepCount, &metrics.NoopCount, &metrics.RejectedCount, &metrics.IntentCount, &metrics.OrderCount,
		&metrics.TransitionCount, &metrics.FillCount, &quantity, &fees,
	)
	if err != nil {
		return fmt.Errorf("postgres: load normalized experiment result %s: %w", value.ID(), err)
	}
	metrics.FilledQuantity, metrics.FeeTotal = quantity, fees
	if experimentID != value.ExperimentID() || programID != value.ProgramID() || planID != value.PlanID() || accountID != value.AccountID() ||
		manifestID != value.ManifestID() || qualityID != value.QualityResultID() || simulationVersion != value.SimulationPolicyVersion() ||
		capitalVersion != value.CapitalPolicyVersion() || mode != string(value.Mode()) || metrics != value.Metrics() {
		return experimentRunConflict("normalized experiment result parent differs from canonical evidence")
	}
	plan, err := repo.GetPlan(ctx, planID)
	if err != nil {
		return fmt.Errorf("postgres: reload result replay plan: %w", err)
	}
	if plan.ExperimentID() != value.ExperimentID() || plan.ProgramID() != value.ProgramID() || plan.AccountID() != value.AccountID() ||
		plan.ManifestID() != value.ManifestID() || plan.Mode() != value.Mode() {
		return experimentRunConflict("experiment result cross-aggregate pins differ from replay plan")
	}
	rows, err := repo.pool.Query(ctx, `SELECT o.sequence,o.action,o.decision_sha256,o.intent_id,o.order_id,
		trim_scale(o.filled_quantity)::text,trim_scale(o.fee_total)::text,o.aggregate_sha256,o.outcome_sha256,
		ARRAY(SELECT t.transition_id FROM experiment_run_transition_ids t WHERE t.result_id=o.result_id AND t.step_sequence=o.sequence ORDER BY t.sequence),
		ARRAY(SELECT f.fill_id FROM experiment_run_fill_ids f WHERE f.result_id=o.result_id AND f.step_sequence=o.sequence ORDER BY f.sequence)
		FROM experiment_run_step_outcomes o WHERE o.result_id=$1 ORDER BY o.sequence`, value.ID())
	if err != nil {
		return fmt.Errorf("postgres: load normalized experiment result outcomes: %w", err)
	}
	defer rows.Close()
	outcomes := value.Outcomes()
	sequenceSeen := 0
	for rows.Next() {
		var sequence int
		var action, decisionSHA, filledQuantity, feeTotal, aggregateSHA, outcomeSHA string
		var intentID, orderID *uuid.UUID
		var transitionIDs, fillIDs []uuid.UUID
		if err := rows.Scan(&sequence, &action, &decisionSHA, &intentID, &orderID, &filledQuantity, &feeTotal,
			&aggregateSHA, &outcomeSHA, &transitionIDs, &fillIDs); err != nil {
			return fmt.Errorf("postgres: scan normalized experiment result outcome: %w", err)
		}
		if sequence != sequenceSeen || sequence >= len(outcomes) {
			return experimentRunConflict("normalized experiment result outcome order differs from canonical evidence")
		}
		expected := outcomes[sequence]
		if action != string(expected.Action) || decisionSHA != expected.DecisionSHA256 || !sameExperimentOptionalUUID(intentID, expected.IntentID) ||
			!sameExperimentOptionalUUID(orderID, expected.OrderID) || filledQuantity != expected.FilledQuantity || feeTotal != expected.FeeTotal ||
			aggregateSHA != expected.AggregateSHA256 || outcomeSHA != expected.OutcomeSHA256 ||
			!sameUUIDSlice(transitionIDs, expected.TransitionIDs) || !sameUUIDSlice(fillIDs, expected.FillIDs) {
			return experimentRunConflict("normalized experiment result outcome differs from canonical evidence")
		}
		sequenceSeen++
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if sequenceSeen != len(outcomes) {
		return experimentRunConflict("normalized experiment result omits canonical outcomes")
	}
	return nil
}

func sameUUIDSlice(left, right []uuid.UUID) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func (repo *ExperimentRunRepo) ready(operation string) error {
	if repo == nil || repo.pool == nil {
		return fmt.Errorf("postgres: %s: repository pool is required", operation)
	}
	return nil
}

func (repo *ExperimentRunRepo) stage(name string) error {
	if repo.afterStage == nil {
		return nil
	}
	if err := repo.afterStage(name); err != nil {
		return fmt.Errorf("postgres: injected experiment run failure after %s: %w", name, err)
	}
	return nil
}

func databaseNow() time.Time { return time.Now().UTC().Truncate(time.Microsecond) }

func sameProgramIdentity(left, right *experimentrun.ProgramIdentity) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && bytes.Equal(left.CanonicalBytes(), right.CanonicalBytes())
}

func samePlan(left, right *experimentrun.Plan) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && bytes.Equal(left.CanonicalBytes(), right.CanonicalBytes())
}

func sameResult(left, right *experimentrun.Result) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && bytes.Equal(left.CanonicalBytes(), right.CanonicalBytes())
}

func sameAttemptEvent(left, right *experimentrun.AttemptEvent) bool {
	return left != nil && right != nil && left.ID() == right.ID() && left.Digest() == right.Digest() && bytes.Equal(left.CanonicalBytes(), right.CanonicalBytes())
}

func experimentRunConflict(message string) error {
	return fmt.Errorf("postgres: %s: %w", message, repository.ErrIdempotencyConflict)
}

func experimentRunWriteError(operation string, err error) error {
	var postgresError *pgconn.PgError
	if errors.As(err, &postgresError) && postgresError.Code == "23505" {
		return fmt.Errorf("postgres: %s: %v: %w", operation, err, repository.ErrIdempotencyConflict)
	}
	return fmt.Errorf("postgres: %s: %w", operation, err)
}
