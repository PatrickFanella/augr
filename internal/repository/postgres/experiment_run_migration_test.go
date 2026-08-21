package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type experimentMigrationFixture struct {
	strategy strategyCatalogFixture
	program  *experimentrun.ProgramIdentity
	plan     *experimentrun.Plan
	result   *experimentrun.Result
	start    time.Time
}

func newExperimentRunMigrationFixture(t *testing.T) experimentMigrationFixture {
	t.Helper()
	ctx := context.Background()
	pool := newStrategyCatalogMigrationPool(t)
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000078_reproducible_experiment_runs.up.sql")); err != nil {
		t.Fatal(err)
	}
	strategy := newStrategyCatalogFixtureWithPool(t, ctx, pool)
	if _, err := strategy.repo.RegisterStrategyFamily(ctx, strategy.family); err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.repo.RegisterStrategyVersion(ctx, strategy.version); err != nil {
		t.Fatal(err)
	}
	partition, observation, available := experimentMigrationObservation(t, strategy.manifest)
	start := available.Add(-time.Minute)
	experiment, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: strategy.version.ID(), AccountID: strategy.account.ID, CapitalBindingID: strategy.binding.ID,
		ManifestID: strategy.manifest.ID(), QualityResultID: strategy.clean.ID(), SimulationPolicyVersion: strategy.simulation,
		CapitalPolicyVersion: strategy.capital, Mode: strategycatalog.ExperimentPaperScored,
		EvaluationStart: start, EvaluationEnd: start.Add(5 * time.Minute), Seed: 303, DatasetQuarantined: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := strategy.repo.DeclareResearchExperiment(ctx, experiment); err != nil {
		t.Fatal(err)
	}
	program, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{
		VersionID: strategy.version.ID(), VersionSHA256: strategy.version.Digest(), CompilerKind: strategy.version.CompilerKind(),
		CompilerVersion: strategy.version.CompilerVersion(), SourceCommit: strategy.version.SourceCommit(), SourceTreeSHA256: strategy.version.SourceTreeSHA256(),
		DecisionContract: strategy.version.DecisionContract(), AdapterKind: "migration-fixture", AdapterVersion: "v1",
		AdapterSHA256: strings.Repeat("c", 64), RunnerContract: experimentrun.RunnerContractV1,
	})
	if err != nil {
		t.Fatal(err)
	}
	capitalCheckpointID := uuid.MustParse("30300000-0000-4000-8000-000000000398")
	capitalStateBytes, _ := json.Marshal(struct {
		Schema                 string `json:"schema"`
		AccountID              string `json:"account_id"`
		BindingID              string `json:"binding_id"`
		PolicyVersion          string `json:"policy_version"`
		ProjectionCheckpointID string `json:"projection_checkpoint_id"`
		ProjectionChecksum     string `json:"projection_checksum"`
		AsOf                   string `json:"as_of"`
		Currency               string `json:"currency"`
		Cash                   string `json:"cash"`
		Equity                 string `json:"equity"`
		LongExposure           string `json:"long_exposure"`
		ShortExposure          string `json:"short_exposure"`
		GrossExposure          string `json:"gross_exposure"`
		MaintenanceRequirement string `json:"maintenance_requirement"`
	}{
		Schema: "capital-state-v1", AccountID: strategy.account.ID.String(), BindingID: strategy.binding.ID.String(),
		PolicyVersion: strategy.capital, ProjectionCheckpointID: capitalCheckpointID.String(), ProjectionChecksum: strings.Repeat("e", 64),
		AsOf: start.Format("2006-01-02T15:04:05.000000Z"), Currency: "USD", Cash: "100000", Equity: "100000",
		LongExposure: "0", ShortExposure: "0", GrossExposure: "0", MaintenanceRequirement: "0",
	})
	plan, err := experimentrun.NewPlan(experimentrun.PlanInput{
		ExperimentID: experiment.ID(), ProgramID: program.ID(), AccountID: strategy.account.ID, ManifestID: strategy.manifest.ID(),
		CapitalStateID: economicid.DeterministicUUID("capital-state", migrationSHA(capitalStateBytes)), CapitalStateSHA256: migrationSHA(capitalStateBytes),
		CapitalProjectionCheckpointID: capitalCheckpointID,
		CapitalStateBytes:             capitalStateBytes,
		ManifestSHA256:                strategy.manifest.Digest(), EvaluationStart: start, EvaluationEnd: start.Add(5 * time.Minute), Seed: 303,
		Mode: strategycatalog.ExperimentPaperScored, Steps: []experimentrun.StepInput{{
			PartitionContentSHA256: partition.ContentSHA256, ObservationSourceKey: observation.SourceKey,
			ObservationContentSHA256: observation.ContentSHA256, AvailableAt: available,
			Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := experimentrun.NewResult(experimentrun.ResultInput{
		Plan: plan, AccountID: strategy.account.ID, QualityResultID: strategy.clean.ID(),
		SimulationPolicyVersion: strategy.simulation, CapitalPolicyVersion: strategy.capital,
		Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	strategy.ctx = ctx
	strategy.pool = pool
	return experimentMigrationFixture{strategy: strategy, program: program, plan: plan, result: result, start: start}
}

func migrationSHA(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func TestExperimentRunMigrationConcurrentRegistrationConverges(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsFound <- insertExperimentProgramPlan(fixture.strategy.ctx, fixture.strategy.pool, fixture.program, fixture.plan, fixture.start)
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Error(err)
		}
	}
	var programs, plans, steps int
	if err := fixture.strategy.pool.QueryRow(fixture.strategy.ctx, `SELECT
		(SELECT count(*) FROM experiment_programs),(SELECT count(*) FROM experiment_replay_plans),
		(SELECT count(*) FROM experiment_replay_plan_steps)`).Scan(&programs, &plans, &steps); err != nil {
		t.Fatal(err)
	}
	if programs != 1 || plans != 1 || steps != 1 {
		t.Fatalf("registered graph=%d/%d/%d want 1/1/1", programs, plans, steps)
	}
}

func TestExperimentRunMigrationPersistsCompleteNoopResultAndRejectsMutation(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	if err := insertExperimentProgramPlan(fixture.strategy.ctx, fixture.strategy.pool, fixture.program, fixture.plan, fixture.start); err != nil {
		t.Fatal(err)
	}
	attemptID := uuid.New()
	started, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{AttemptID: attemptID, Sequence: 0, Type: experimentrun.AttemptStarted, OccurredAt: fixture.start})
	if err := insertExperimentAttemptStart(fixture.strategy.ctx, fixture.strategy.pool, fixture.plan.ExperimentID(), started, fixture.start); err != nil {
		t.Fatal(err)
	}
	completed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptCompleted, OccurredAt: fixture.start.Add(time.Second), ResultID: fixture.result.ID()})
	if err := insertExperimentNoopResult(fixture.strategy.ctx, fixture.strategy.pool, fixture.result, fixture.plan, completed, fixture.start.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var results, outcomes, events int
	if err := fixture.strategy.pool.QueryRow(fixture.strategy.ctx, `SELECT
		(SELECT count(*) FROM experiment_run_results),(SELECT count(*) FROM experiment_run_step_outcomes),
		(SELECT count(*) FROM experiment_run_attempt_events WHERE attempt_id=$1)`, attemptID).Scan(&results, &outcomes, &events); err != nil {
		t.Fatal(err)
	}
	if results != 1 || outcomes != 1 || events != 2 {
		t.Fatalf("completed graph=%d/%d/%d", results, outcomes, events)
	}
	if _, err := fixture.strategy.pool.Exec(fixture.strategy.ctx, `UPDATE experiment_run_results SET state=state WHERE id=$1`, fixture.result.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("result update error=%v, want append-only", err)
	}
	if _, err := fixture.strategy.pool.Exec(fixture.strategy.ctx, `DELETE FROM experiment_replay_plan_steps WHERE plan_id=$1`, fixture.plan.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("step delete error=%v, want append-only", err)
	}
}

func TestExperimentRunMigrationRejectsIncompleteAndForgedGraphs(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	ctx := fixture.strategy.ctx
	pool := fixture.strategy.pool
	if err := insertExperimentProgram(ctx, pool, fixture.program, fixture.start); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := insertExperimentPlanParent(ctx, tx, fixture.plan, fixture.start); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil || !strings.Contains(err.Error(), "plan graph does not reconstruct") {
		t.Fatalf("omitted plan step commit error=%v", err)
	}

	if err := insertExperimentProgramPlan(ctx, pool, fixture.program, fixture.plan, fixture.start); err != nil {
		t.Fatal(err)
	}
	attemptID := uuid.New()
	started, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{AttemptID: attemptID, Sequence: 0, Type: experimentrun.AttemptStarted, OccurredAt: fixture.start})
	if err := insertExperimentAttemptStart(ctx, pool, fixture.plan.ExperimentID(), started, fixture.start); err != nil {
		t.Fatal(err)
	}
	completed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptCompleted, OccurredAt: fixture.start.Add(time.Second), ResultID: fixture.result.ID()})
	if _, err := pool.Exec(ctx, `INSERT INTO experiment_run_attempt_events(
		id,schema_name,attempt_id,experiment_id,sequence,type,occurred_at,result_id,error_code,error_sha256,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-attempt-event-v1',$2,$3,1,'completed',$4,$5,'','',$6,$7,convert_from($7,'UTF8')::jsonb,$4)`,
		completed.ID(), attemptID, fixture.plan.ExperimentID(), fixture.start.Add(time.Second), fixture.result.ID(), completed.Digest(), completed.CanonicalBytes()); err == nil {
		t.Fatal("completed attempt accepted without an atomic result")
	}
	var terminals int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM experiment_run_attempt_events WHERE attempt_id=$1 AND sequence=1`, attemptID).Scan(&terminals); err != nil || terminals != 0 {
		t.Fatalf("forged terminal count=%d err=%v", terminals, err)
	}
}

func TestExperimentRunMigrationNonemptyRollbackRefuses(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	if err := insertExperimentProgramPlan(fixture.strategy.ctx, fixture.strategy.pool, fixture.program, fixture.plan, fixture.start); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.strategy.pool.Exec(fixture.strategy.ctx, repositoryMigrationSQL(t, "000078_reproducible_experiment_runs.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 78") {
		t.Fatalf("nonempty rollback error=%v", err)
	}
	var plans int
	if err := fixture.strategy.pool.QueryRow(fixture.strategy.ctx, `SELECT count(*) FROM experiment_replay_plans`).Scan(&plans); err != nil || plans != 1 {
		t.Fatalf("rollback refusal did not preserve plan count=%d err=%v", plans, err)
	}
}

func experimentMigrationObservation(t *testing.T, manifest *dataset.Manifest) (dataset.Partition, dataset.Observation, time.Time) {
	t.Helper()
	for _, partition := range manifest.Partitions() {
		if partition.Kind != dataset.KindQuotes {
			continue
		}
		observation := partition.Observations[0]
		available, err := time.Parse("2006-01-02T15:04:05.000000Z", observation.AvailableAt)
		if err != nil {
			t.Fatal(err)
		}
		return partition, observation, available
	}
	t.Fatal("quote observation not found")
	return dataset.Partition{}, dataset.Observation{}, time.Time{}
}

func insertExperimentProgramPlan(ctx context.Context, pool *pgxpool.Pool, program *experimentrun.ProgramIdentity, plan *experimentrun.Plan, createdAt time.Time) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertExperimentProgramTx(ctx, tx, program, createdAt); err != nil {
		return err
	}
	if err := insertExperimentPlanParent(ctx, tx, plan, createdAt); err != nil {
		return err
	}
	for sequence, step := range plan.Steps() {
		if _, err := tx.Exec(ctx, `INSERT INTO experiment_replay_plan_steps(
			plan_id,sequence,partition_content_sha256,observation_source_key,observation_content_sha256,available_at,
			decision_bytes,decision,decision_sha256,action,rejection_code
		) VALUES($1,$2,$3,$4,$5,$6,$7,convert_from($7,'UTF8')::jsonb,$8,$9,$10) ON CONFLICT(plan_id,sequence) DO NOTHING`,
			plan.ID(), sequence, step.PartitionContentSHA256, step.ObservationSourceKey, step.ObservationContentSHA256, step.AvailableAt,
			step.Decision, plan.DecisionSHA256(sequence), step.Action, step.RejectionCode); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertExperimentProgram(ctx context.Context, pool *pgxpool.Pool, program *experimentrun.ProgramIdentity, createdAt time.Time) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertExperimentProgramTx(ctx, tx, program, createdAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertExperimentProgramTx(ctx context.Context, tx pgx.Tx, program *experimentrun.ProgramIdentity, createdAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO experiment_programs(
		id,schema_name,version_id,version_sha256,compiler_kind,compiler_version,source_commit,source_tree_sha256,
		decision_contract,adapter_kind,adapter_version,adapter_sha256,runner_contract,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-program-v1',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,convert_from($14,'UTF8')::jsonb,$15) ON CONFLICT(id) DO NOTHING`,
		program.ID(), program.VersionID(), program.VersionSHA256(), program.CompilerKind(), program.CompilerVersion(), program.SourceCommit(),
		program.SourceTreeSHA256(), program.DecisionContract(), program.AdapterKind(), program.AdapterVersion(), program.AdapterSHA256(),
		program.RunnerContract(), program.Digest(), program.CanonicalBytes(), createdAt)
	return err
}

func insertExperimentPlanParent(ctx context.Context, tx pgx.Tx, plan *experimentrun.Plan, createdAt time.Time) error {
	_, err := tx.Exec(ctx, `INSERT INTO experiment_replay_plans(
		id,schema_name,experiment_id,program_id,account_id,capital_state_id,capital_state_sha256,capital_projection_checkpoint_id,
		capital_state_bytes,capital_state,manifest_id,manifest_sha256,evaluation_start,evaluation_end,seed,mode,
		step_count,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-replay-plan-v1',$2,$3,$4,$5,$6,$7,$8,convert_from($8,'UTF8')::jsonb,$9,$10,$11,$12,$13,$14,
		$15,$16,$17,convert_from($17,'UTF8')::jsonb,$18) ON CONFLICT(id) DO NOTHING`,
		plan.ID(), plan.ExperimentID(), plan.ProgramID(), plan.AccountID(), plan.CapitalStateID(), plan.CapitalStateSHA256(),
		plan.CapitalProjectionCheckpointID(), plan.CapitalStateBytes(), plan.ManifestID(), plan.ManifestSHA256(), plan.EvaluationStart(),
		plan.EvaluationEnd(), plan.Seed(), plan.Mode(), plan.StepCount(), plan.Digest(), plan.CanonicalBytes(), createdAt)
	return err
}

func insertExperimentAttemptStart(ctx context.Context, pool *pgxpool.Pool, experimentID uuid.UUID, event *experimentrun.AttemptEvent, createdAt time.Time) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO experiment_run_attempts(id,experiment_id,created_at) VALUES($1,$2,$3)`, event.AttemptID(), experimentID, createdAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO experiment_run_attempt_events(
		id,schema_name,attempt_id,experiment_id,sequence,type,occurred_at,result_id,error_code,error_sha256,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-attempt-event-v1',$2,$3,0,'started',$4,NULL,'','',$5,$6,convert_from($6,'UTF8')::jsonb,$4)`,
		event.ID(), event.AttemptID(), experimentID, createdAt, event.Digest(), event.CanonicalBytes()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func insertExperimentNoopResult(ctx context.Context, pool *pgxpool.Pool, result *experimentrun.Result, plan *experimentrun.Plan, completed *experimentrun.AttemptEvent, createdAt time.Time) error {
	metrics := result.Metrics()
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var qualityID, simulationVersion, capitalVersion string
	if err := tx.QueryRow(ctx, `SELECT quality_result_id::text,simulation_policy_version,capital_policy_version FROM research_experiments WHERE id=$1`, plan.ExperimentID()).Scan(&qualityID, &simulationVersion, &capitalVersion); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO experiment_run_results(
		id,schema_name,state,experiment_id,program_id,plan_id,account_id,manifest_id,quality_result_id,simulation_policy_version,
		capital_policy_version,mode,step_count,noop_count,rejected_count,intent_count,order_count,transition_count,fill_count,
		filled_quantity,fee_total,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-run-result-v1','completed',$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,convert_from($21,'UTF8')::jsonb,$22)`,
		result.ID(), plan.ExperimentID(), plan.ProgramID(), plan.ID(), plan.AccountID(), plan.ManifestID(), qualityID, simulationVersion, capitalVersion,
		plan.Mode(), metrics.StepCount, metrics.NoopCount, metrics.RejectedCount, metrics.IntentCount, metrics.OrderCount, metrics.TransitionCount,
		metrics.FillCount, metrics.FilledQuantity, metrics.FeeTotal, result.Digest(), result.CanonicalBytes(), createdAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO experiment_run_step_outcomes(
		result_id,sequence,action,decision_sha256,intent_id,order_id,transition_count,fill_count,filled_quantity,fee_total,aggregate_sha256,outcome_sha256
	) VALUES($1,0,'noop',$2,NULL,NULL,0,0,0,0,'','')`, result.ID(), plan.DecisionSHA256(0)); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO experiment_run_attempt_events(
		id,schema_name,attempt_id,experiment_id,sequence,type,occurred_at,result_id,error_code,error_sha256,sha256,canonical_bytes,canonical_json,created_at
	) VALUES($1,'experiment-attempt-event-v1',$2,$3,1,'completed',$4,$5,'','',$6,$7,convert_from($7,'UTF8')::jsonb,$4)`,
		completed.ID(), completed.AttemptID(), plan.ExperimentID(), createdAt, result.ID(), completed.Digest(), completed.CanonicalBytes()); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
