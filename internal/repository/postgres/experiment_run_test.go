package postgres

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestExperimentRunRepoPersistsReloadsAndListsCompleteGraph(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	repo := NewExperimentRunRepo(fixture.strategy.pool)
	ctx := fixture.strategy.ctx

	program, err := repo.RecordProgram(ctx, fixture.program)
	if err != nil || !sameProgramIdentity(program, fixture.program) {
		t.Fatalf("RecordProgram()=%v err=%v", program, err)
	}
	plan, err := repo.RecordPlan(ctx, fixture.plan)
	if err != nil || !samePlan(plan, fixture.plan) {
		t.Fatalf("RecordPlan()=%v err=%v", plan, err)
	}

	attemptID := uuid.New()
	started, err := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 0, Type: experimentrun.AttemptStarted, OccurredAt: fixture.start,
	})
	if err != nil {
		t.Fatal(err)
	}
	if persisted, err := repo.RecordAttemptEvent(ctx, fixture.plan.ExperimentID(), uuid.Nil, started); err != nil || !sameAttemptEvent(persisted, started) {
		t.Fatalf("RecordAttemptEvent(started)=%v err=%v", persisted, err)
	}
	completed, err := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptCompleted,
		OccurredAt: fixture.start.Add(time.Second), ResultID: fixture.result.ID(),
	})
	if err != nil {
		t.Fatal(err)
	}
	result, terminal, err := repo.RecordCompletedResult(ctx, fixture.plan.ExperimentID(), fixture.result, completed)
	if err != nil || !sameResult(result, fixture.result) || !sameAttemptEvent(terminal, completed) {
		t.Fatalf("RecordCompletedResult() result=%v terminal=%v err=%v", result, terminal, err)
	}

	restarted := NewExperimentRunRepo(fixture.strategy.pool)
	loadedProgram, err := restarted.GetProgram(ctx, fixture.program.ID())
	if err != nil || !sameProgramIdentity(loadedProgram, fixture.program) {
		t.Fatalf("GetProgram after restart=%v err=%v", loadedProgram, err)
	}
	loadedPlan, err := restarted.GetPlan(ctx, fixture.plan.ID())
	if err != nil || !samePlan(loadedPlan, fixture.plan) {
		t.Fatalf("GetPlan after restart=%v err=%v", loadedPlan, err)
	}
	events, err := restarted.GetAttemptEvents(ctx, attemptID)
	if err != nil || len(events) != 2 || !sameAttemptEvent(events[0], started) || !sameAttemptEvent(events[1], completed) {
		t.Fatalf("GetAttemptEvents after restart=%v err=%v", events, err)
	}
	results, err := restarted.ListExperimentResults(ctx, fixture.plan.ExperimentID(), 10, 0)
	if err != nil || len(results) != 1 || !sameResult(results[0], fixture.result) {
		t.Fatalf("ListExperimentResults after restart=%v err=%v", results, err)
	}
}

func TestExperimentRunRepoConcurrentExactWritersConverge(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	repo := NewExperimentRunRepo(fixture.strategy.pool)
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			if _, err := repo.RecordProgram(fixture.strategy.ctx, fixture.program); err != nil {
				errorsFound <- err
				return
			}
			_, err := repo.RecordPlan(fixture.strategy.ctx, fixture.plan)
			errorsFound <- err
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
		t.Fatalf("persisted graph=%d/%d/%d want 1/1/1", programs, plans, steps)
	}
}

func TestExperimentRunRepoInterruptionCanFailAndRetryConverges(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	repo := NewExperimentRunRepo(fixture.strategy.pool)
	ctx := fixture.strategy.ctx
	if _, err := repo.RecordProgram(ctx, fixture.program); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordPlan(ctx, fixture.plan); err != nil {
		t.Fatal(err)
	}
	attemptID := uuid.New()
	started, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 0, Type: experimentrun.AttemptStarted, OccurredAt: fixture.start,
	})
	if _, err := repo.RecordAttemptEvent(ctx, fixture.plan.ExperimentID(), uuid.Nil, started); err != nil {
		t.Fatal(err)
	}
	failed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptFailed, OccurredAt: fixture.start.Add(time.Second),
		ErrorCode: "interrupted", ErrorSHA256: migrationSHA([]byte("injected interruption")),
	})
	for range 2 {
		persisted, err := repo.RecordAttemptEvent(ctx, fixture.plan.ExperimentID(), uuid.Nil, failed)
		if err != nil || !sameAttemptEvent(persisted, failed) {
			t.Fatalf("RecordAttemptEvent(failed)=%v err=%v", persisted, err)
		}
	}
	completed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptCompleted,
		OccurredAt: fixture.start.Add(2 * time.Second), ResultID: fixture.result.ID(),
	})
	if _, _, err := repo.RecordCompletedResult(ctx, fixture.plan.ExperimentID(), fixture.result, completed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("completion after failure error=%v, want ErrIdempotencyConflict", err)
	}
	var resultCount int
	if err := fixture.strategy.pool.QueryRow(ctx, `SELECT count(*) FROM experiment_run_results`).Scan(&resultCount); err != nil || resultCount != 0 {
		t.Fatalf("completion conflict left result count=%d err=%v", resultCount, err)
	}
}

func TestExperimentRunRepoRejectsTerminalPayloadChange(t *testing.T) {
	fixture := newExperimentRunMigrationFixture(t)
	repo := NewExperimentRunRepo(fixture.strategy.pool)
	attemptID := uuid.New()
	started, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 0, Type: experimentrun.AttemptStarted, OccurredAt: fixture.start,
	})
	if _, err := repo.RecordAttemptEvent(fixture.strategy.ctx, fixture.plan.ExperimentID(), uuid.Nil, started); err != nil {
		t.Fatal(err)
	}
	failed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptFailed, OccurredAt: fixture.start.Add(time.Second),
		ErrorCode: "first_failure", ErrorSHA256: migrationSHA([]byte("first")),
	})
	if _, err := repo.RecordAttemptEvent(fixture.strategy.ctx, fixture.plan.ExperimentID(), uuid.Nil, failed); err != nil {
		t.Fatal(err)
	}
	changed, _ := experimentrun.NewAttemptEvent(experimentrun.AttemptEventInput{
		AttemptID: attemptID, Sequence: 1, Type: experimentrun.AttemptFailed, OccurredAt: fixture.start.Add(time.Second),
		ErrorCode: "changed_failure", ErrorSHA256: migrationSHA([]byte("changed")),
	})
	if _, err := repo.RecordAttemptEvent(fixture.strategy.ctx, fixture.plan.ExperimentID(), uuid.Nil, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed terminal error=%v, want ErrIdempotencyConflict", err)
	}
}
