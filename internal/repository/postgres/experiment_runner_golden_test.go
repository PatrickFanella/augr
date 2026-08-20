package postgres

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestExperimentRunnerGoldenPostgresReplayAndRestart(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	fixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperScored)
	repo := NewExperimentRunRepo(pool)
	runner, err := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, repo)
	if err != nil {
		t.Fatal(err)
	}
	first := runGoldenExperiment(t, runner, fixture, uuid.MustParse("30300000-0000-4000-8000-000000000301"), 0)
	metrics := first.Metrics()
	if metrics.StepCount != 1 || metrics.IntentCount != 1 || metrics.OrderCount != 1 || metrics.FillCount != 2 ||
		metrics.FilledQuantity != "10" || metrics.FeeTotal != "1.01" {
		t.Fatalf("golden metrics=%+v", metrics)
	}

	restartedRepo := NewExperimentRunRepo(pool)
	restartedRunner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, restartedRepo)
	second := runGoldenExperiment(t, restartedRunner, fixture, uuid.MustParse("30300000-0000-4000-8000-000000000302"), 1)
	assertExactResult(t, second, first)
	results, err := restartedRepo.ListExperimentResults(context.Background(), fixture.Graph.Experiment.ID(), 10, 0)
	if err != nil || len(results) != 1 {
		t.Fatalf("persisted result population=%d err=%v", len(results), err)
	}
	var attempts, rawEvents, intents, orders, fills int
	if err := pool.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM experiment_run_attempts WHERE experiment_id=$1),
		(SELECT count(*) FROM economic_source_events WHERE account_id=$2),
		(SELECT count(*) FROM execution_intents WHERE account_id=$2),
		(SELECT count(*) FROM execution_orders o JOIN execution_intents i ON i.id=o.intent_id WHERE i.account_id=$2),
		(SELECT count(*) FROM execution_fills WHERE account_id=$2)`, fixture.Graph.Experiment.ID(), fixture.Graph.Account.ID).
		Scan(&attempts, &rawEvents, &intents, &orders, &fills); err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || rawEvents != 2 || intents != 1 || orders != 1 || fills != 2 {
		t.Fatalf("restart convergence attempts/raw/intents/orders/fills=%d/%d/%d/%d/%d", attempts, rawEvents, intents, orders, fills)
	}
}

func TestExperimentRunnerRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("EXPERIMENT_RUN_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set EXPERIMENT_RUN_QUALIFICATION_DB_URL to a dedicated empty schema-78 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var schema string
	var version, existing int
	if err := pool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schema); err != nil || schema != "public" {
		t.Fatalf("qualification schema=%q err=%v", schema, err)
	}
	if err := pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 78 {
		t.Fatalf("qualification version=%d err=%v", version, err)
	}
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM experiment_programs)+(SELECT count(*) FROM experiment_replay_plans)+
		(SELECT count(*) FROM experiment_run_attempts)+(SELECT count(*) FROM experiment_run_results)`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("qualification database is not experiment-run-empty count=%d err=%v", existing, err)
	}
	fixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperScored)
	repo := NewExperimentRunRepo(pool)
	runner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, repo)
	first := runGoldenExperiment(t, runner, fixture, uuid.MustParse("30300000-0000-4000-8000-000000000341"), 0)
	second := runGoldenExperiment(t, runner, fixture, uuid.MustParse("30300000-0000-4000-8000-000000000342"), 1)
	assertExactResult(t, second, first)
	failing := &failCompletedResultStore{ExperimentRunRepo: repo, fail: true}
	failedRunner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, failing)
	if result, err := failedRunner.Run(ctx, experimentrun.RunRequest{
		ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: uuid.MustParse("30300000-0000-4000-8000-000000000343"),
		StartedAt: qualification.Start.Add(time.Second), FinishedAt: qualification.End.Add(2 * time.Second), Program: fixture.Program,
	}); err == nil || result != nil {
		t.Fatalf("retained injected failure result=%v err=%v", result, err)
	}
	var programs, plans, results, attempts, failed int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM experiment_programs),(SELECT count(*) FROM experiment_replay_plans),
		(SELECT count(*) FROM experiment_run_results),(SELECT count(*) FROM experiment_run_attempts),
		(SELECT count(*) FROM experiment_run_attempt_events WHERE type='failed')`).Scan(&programs, &plans, &results, &attempts, &failed); err != nil {
		t.Fatal(err)
	}
	if programs != 1 || plans != 1 || results != 1 || attempts != 3 || failed != 1 {
		t.Fatalf("retained graph programs/plans/results/attempts/failed=%d/%d/%d/%d/%d", programs, plans, results, attempts, failed)
	}
	t.Logf("VERIFIED_LOCAL experiment=%s program=%s plan=%s result=%s sha256=%s attempts=%d",
		fixture.Graph.Experiment.ID(), fixture.Program.Identity().ID(), first.PlanID(), first.ID(), first.Digest(), attempts)
}

func TestExperimentRunnerGoldenPartialFill(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	fixture, err := qualification.BuildPartial(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	persistExperimentRunnerFixture(t, pool, fixture)
	runner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
	result := runGoldenExperiment(t, runner, fixture, uuid.MustParse("30300000-0000-4000-8000-000000000309"), 0)
	metrics := result.Metrics()
	if metrics.FillCount != 2 || metrics.FilledQuantity != "20" || metrics.FeeTotal != "1.02" {
		t.Fatalf("partial-fill metrics=%+v", metrics)
	}
	lifecycleValue, err := NewExecutionLifecycleRepo(pool).GetExecutionLifecycle(context.Background(), fixture.Graph.Account.ID, result.Outcomes()[0].IntentID)
	if err != nil || lifecycleValue.State != "partially_filled" {
		t.Fatalf("partial-fill lifecycle state=%v err=%v", lifecycleValue, err)
	}
}

func TestExperimentRunnerGoldenExplicitNoopAndRejection(t *testing.T) {
	tests := []struct {
		name     string
		build    func(strategycatalog.ExperimentMode) (*qualification.Fixture, error)
		noops    int
		rejected int
	}{
		{name: "noop", build: qualification.BuildNoop, noops: 1},
		{name: "rejected", build: qualification.BuildRejected, rejected: 1},
	}
	for index, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			pool := newExperimentRunnerGoldenPool(t)
			fixture, err := testCase.build(strategycatalog.ExperimentPaperScored)
			if err != nil {
				t.Fatal(err)
			}
			persistExperimentRunnerFixture(t, pool, fixture)
			runner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
			attemptID := uuid.MustParse([]string{
				"30300000-0000-4000-8000-000000000310",
				"30300000-0000-4000-8000-000000000312",
			}[index])
			result := runGoldenExperiment(t, runner, fixture, attemptID, 0)
			metrics := result.Metrics()
			if metrics.NoopCount != testCase.noops || metrics.RejectedCount != testCase.rejected || metrics.IntentCount != 0 ||
				metrics.OrderCount != 0 || metrics.FillCount != 0 || metrics.FilledQuantity != "0" || metrics.FeeTotal != "0" {
				t.Fatalf("control metrics=%+v", metrics)
			}
			var economics int
			if err := pool.QueryRow(context.Background(), `SELECT
				(SELECT count(*) FROM execution_intents WHERE account_id=$1)+
				(SELECT count(*) FROM economic_source_events WHERE account_id=$1)`, fixture.Graph.Account.ID).Scan(&economics); err != nil || economics != 0 {
				t.Fatalf("control invented economics=%d err=%v", economics, err)
			}
		})
	}
}

func TestExperimentRunnerGoldenCleanDatabaseReproduction(t *testing.T) {
	firstPool := newExperimentRunnerGoldenPool(t)
	firstFixture := persistExperimentRunnerGolden(t, firstPool, strategycatalog.ExperimentPaperScored)
	firstRunner, _ := experimentrun.NewRunner(qualification.Loader{Graph: firstFixture.Graph}, NewExperimentRunRepo(firstPool))
	first := runGoldenExperiment(t, firstRunner, firstFixture, uuid.MustParse("30300000-0000-4000-8000-000000000303"), 0)

	cleanPool := newExperimentRunnerGoldenPool(t)
	cleanFixture := persistExperimentRunnerGolden(t, cleanPool, strategycatalog.ExperimentPaperScored)
	cleanRunner, _ := experimentrun.NewRunner(qualification.Loader{Graph: cleanFixture.Graph}, NewExperimentRunRepo(cleanPool))
	clean := runGoldenExperiment(t, cleanRunner, cleanFixture, uuid.MustParse("30300000-0000-4000-8000-000000000304"), 2)
	assertExactResult(t, clean, first)

	firstOutcome, cleanOutcome := first.Outcomes()[0], clean.Outcomes()[0]
	if firstOutcome.DecisionSHA256 != cleanOutcome.DecisionSHA256 || firstOutcome.IntentID != cleanOutcome.IntentID ||
		firstOutcome.OrderID != cleanOutcome.OrderID || !sameUUIDSlice(firstOutcome.TransitionIDs, cleanOutcome.TransitionIDs) ||
		!sameUUIDSlice(firstOutcome.FillIDs, cleanOutcome.FillIDs) || firstOutcome.AggregateSHA256 != cleanOutcome.AggregateSHA256 ||
		firstOutcome.OutcomeSHA256 != cleanOutcome.OutcomeSHA256 || first.Metrics() != clean.Metrics() {
		t.Fatal("clean database replay diverged below the result envelope")
	}
}

func TestExperimentRunnerGoldenScoredStressIsolation(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	scoredFixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperScored)
	stressFixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperStress)
	repo := NewExperimentRunRepo(pool)
	scoredRunner, _ := experimentrun.NewRunner(qualification.Loader{Graph: scoredFixture.Graph}, repo)
	stressRunner, _ := experimentrun.NewRunner(qualification.Loader{Graph: stressFixture.Graph}, repo)
	scored := runGoldenExperiment(t, scoredRunner, scoredFixture, uuid.MustParse("30300000-0000-4000-8000-000000000305"), 0)
	stress := runGoldenExperiment(t, stressRunner, stressFixture, uuid.MustParse("30300000-0000-4000-8000-000000000306"), 0)
	if scored.ID() == stress.ID() || scored.PlanID() == stress.PlanID() || scored.AccountID() == stress.AccountID() ||
		scored.Mode() != strategycatalog.ExperimentPaperScored || stress.Mode() != strategycatalog.ExperimentPaperStress {
		t.Fatalf("scored/stress identities were not isolated: %s/%s", scored.ID(), stress.ID())
	}
	var crossed int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM experiment_run_results r
		JOIN research_experiments e ON e.id=r.experiment_id WHERE r.id=$1 AND e.mode='paper_stress'`, scored.ID()).Scan(&crossed); err != nil || crossed != 0 {
		t.Fatalf("scored result crossed into stress population count=%d err=%v", crossed, err)
	}
}

func TestExperimentRunnerGoldenFailedCompletionThenRetry(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	fixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperScored)
	store := &failCompletedResultStore{ExperimentRunRepo: NewExperimentRunRepo(pool), fail: true}
	runner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, store)
	result, err := runner.Run(context.Background(), experimentrun.RunRequest{
		ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: uuid.MustParse("30300000-0000-4000-8000-000000000307"),
		StartedAt: qualification.Start.Add(-time.Second), FinishedAt: qualification.End, Program: fixture.Program,
	})
	if err == nil || result != nil {
		t.Fatalf("injected completion failure result=%v err=%v", result, err)
	}
	var completedResults int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM experiment_run_results`).Scan(&completedResults); err != nil || completedResults != 0 {
		t.Fatalf("failed completion retained result count=%d err=%v", completedResults, err)
	}
	store.fail = false
	completed := runGoldenExperiment(t, runner, fixture, uuid.MustParse("30300000-0000-4000-8000-000000000308"), 1)
	if completed == nil {
		t.Fatal("retry did not complete")
	}
	failedEvents, err := store.GetAttemptEvents(context.Background(), uuid.MustParse("30300000-0000-4000-8000-000000000307"))
	if err != nil || len(failedEvents) != 2 || failedEvents[1].Type() != experimentrun.AttemptFailed {
		t.Fatalf("failed attempt evidence=%v err=%v", failedEvents, err)
	}
}

func TestExperimentRunnerGoldenResultChildStageRollback(t *testing.T) {
	for index, stage := range []string{"result_parent", "result_outcome", "result_transition", "result_fill", "result_graph"} {
		t.Run(stage, func(t *testing.T) {
			pool := newExperimentRunnerGoldenPool(t)
			fixture := persistExperimentRunnerGolden(t, pool, strategycatalog.ExperimentPaperScored)
			repo := NewExperimentRunRepo(pool)
			repo.afterStage = func(current string) error {
				if current == stage {
					return errors.New("injected " + stage)
				}
				return nil
			}
			runner, _ := experimentrun.NewRunner(qualification.Loader{Graph: fixture.Graph}, repo)
			attemptIDs := []string{
				"30300000-0000-4000-8000-000000000331",
				"30300000-0000-4000-8000-000000000332",
				"30300000-0000-4000-8000-000000000333",
				"30300000-0000-4000-8000-000000000334",
				"30300000-0000-4000-8000-000000000335",
			}
			result, err := runner.Run(context.Background(), experimentrun.RunRequest{
				ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: uuid.MustParse(attemptIDs[index]),
				StartedAt: qualification.Start.Add(-time.Second), FinishedAt: qualification.End, Program: fixture.Program,
			})
			if err == nil || result != nil {
				t.Fatalf("stage failure result=%v err=%v", result, err)
			}
			var parents, outcomes, transitions, fills, completed int
			if err := pool.QueryRow(context.Background(), `SELECT
				(SELECT count(*) FROM experiment_run_results),
				(SELECT count(*) FROM experiment_run_step_outcomes),
				(SELECT count(*) FROM experiment_run_transition_ids),
				(SELECT count(*) FROM experiment_run_fill_ids),
				(SELECT count(*) FROM experiment_run_attempt_events WHERE type='completed')`).Scan(
				&parents, &outcomes, &transitions, &fills, &completed); err != nil {
				t.Fatal(err)
			}
			if parents != 0 || outcomes != 0 || transitions != 0 || fills != 0 || completed != 0 {
				t.Fatalf("stage rollback leaked graph=%d/%d/%d/%d completed=%d", parents, outcomes, transitions, fills, completed)
			}
			events, eventErr := repo.GetAttemptEvents(context.Background(), uuid.MustParse(attemptIDs[index]))
			if eventErr != nil || len(events) != 2 || events[1].Type() != experimentrun.AttemptFailed {
				t.Fatalf("stage failure attempt=%v err=%v", events, eventErr)
			}
		})
	}
}

type failCompletedResultStore struct {
	*ExperimentRunRepo
	fail bool
}

func (store *failCompletedResultStore) RecordCompletedResult(ctx context.Context, experimentID uuid.UUID, result *experimentrun.Result, event *experimentrun.AttemptEvent) (*experimentrun.Result, *experimentrun.AttemptEvent, error) {
	if store.fail {
		return nil, nil, errors.New("injected golden result persistence failure")
	}
	return store.ExperimentRunRepo.RecordCompletedResult(ctx, experimentID, result, event)
}

func newExperimentRunnerGoldenPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := newStrategyCatalogMigrationPool(t)
	if _, err := pool.Exec(context.Background(), repositoryMigrationSQL(t, "000078_reproducible_experiment_runs.up.sql")); err != nil {
		t.Fatal(err)
	}
	return pool
}

func persistExperimentRunnerGolden(t *testing.T, pool *pgxpool.Pool, mode strategycatalog.ExperimentMode) *qualification.Fixture {
	t.Helper()
	fixture, err := qualification.Build(mode)
	if err != nil {
		t.Fatal(err)
	}
	persistExperimentRunnerFixture(t, pool, fixture)
	return fixture
}

func persistExperimentRunnerFixture(t *testing.T, pool *pgxpool.Pool, fixture *qualification.Fixture) {
	t.Helper()
	ctx := context.Background()
	if err := NewAccountRepo(pool).Create(ctx, fixture.Graph.Account); err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err := instruments.CreateInstrument(ctx, fixture.Instrument); err != nil {
		t.Fatal(err)
	}
	if _, err := instruments.RegisterVenueContract(ctx, fixture.VenueContract); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, fixture.QuoteSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCapitalPolicyRepo(pool).RegisterCapitalPolicy(ctx, fixture.CapitalArtifact); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCapitalPolicyRepo(pool).BindCapitalPolicy(ctx, fixture.Graph.CapitalBinding); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSimulationPolicyRepo(pool).RegisterSimulationPolicy(ctx, fixture.SimulationArtifact); err != nil {
		t.Fatal(err)
	}
	datasets := NewDatasetRepo(pool)
	if _, err := datasets.RegisterDatasetPolicy(ctx, fixture.DatasetPolicy); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetManifest(ctx, fixture.Graph.Manifest, qualification.Start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetQualityResult(ctx, fixture.Graph.Quality, qualification.Start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	catalog := NewStrategyCatalogRepo(pool)
	if _, err := catalog.RegisterStrategyFamily(ctx, fixture.Family); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.RegisterStrategyVersion(ctx, fixture.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.DeclareResearchExperiment(ctx, fixture.Graph.Experiment); err != nil {
		t.Fatal(err)
	}
}

func runGoldenExperiment(t *testing.T, runner *experimentrun.Runner, fixture *qualification.Fixture, attemptID uuid.UUID, offset int) *experimentrun.Result {
	t.Helper()
	result, err := runner.Run(context.Background(), experimentrun.RunRequest{
		ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: attemptID,
		StartedAt:  qualification.Start.Add(time.Duration(offset-1) * time.Second),
		FinishedAt: qualification.End.Add(time.Duration(offset) * time.Second), Program: fixture.Program,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func assertExactResult(t *testing.T, got, want *experimentrun.Result) {
	t.Helper()
	if got.ID() != want.ID() || got.Digest() != want.Digest() || string(got.CanonicalBytes()) != string(want.CanonicalBytes()) {
		t.Fatalf("result diverged got=%s/%s want=%s/%s", got.ID(), got.Digest(), want.ID(), want.Digest())
	}
}
