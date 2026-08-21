package postgres

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/dailysupervisor"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
)

func TestDailySupervisorRetainedQualification(t *testing.T) {
	url := os.Getenv("DAILY_SUPERVISOR_QUALIFICATION_DB_URL")
	if url == "" {
		t.Skip("set DAILY_SUPERVISOR_QUALIFICATION_DB_URL to a dedicated schema-99 database with OVR-207 evidence")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_supervisor_assessments`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("existing assessments=%d/%v", count, err)
	}
	var reconciliationID uuid.UUID
	var reconciliationSHA string
	if err := pool.QueryRow(ctx, `SELECT id,sha256 FROM venue_reconciliation_runs WHERE clean ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&reconciliationID, &reconciliationSHA); err != nil {
		t.Fatal(err)
	}
	scheduler := NewFinancialSchedulerRepo(pool)
	if err := scheduler.RegisterCatalog(ctx, financialscheduler.Catalog()); err != nil {
		t.Fatal(err)
	}

	evaluatedAt := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	occurrence, effect := claimSupervisorEffect(t, ctx, scheduler, evaluatedAt, "all-pass")
	input := retainedSupervisorInput(evaluatedAt, reconciliationID, reconciliationSHA, occurrence, effect)
	assessment, err := dailysupervisor.NewAssessment(input)
	if err != nil {
		t.Fatal(err)
	}

	for _, failedStage := range []string{"policy", "assessment", "checks", "actions", "attention"} {
		repo := NewDailySupervisorRepo(pool)
		repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := repo.RegisterAssessment(ctx, assessment); err == nil {
			t.Fatalf("stage %s accepted", failedStage)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_supervisor_assessments WHERE id=$1`, assessment.ID()).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failedStage, count, err)
		}
	}

	type result struct {
		persisted *PersistedDailySupervisorAssessment
		err       error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			persisted, registerErr := NewDailySupervisorRepo(pool).RegisterAssessment(ctx, assessment)
			results <- result{persisted, registerErr}
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		if result.persisted.ID != assessment.ID() || result.persisted.SHA256 != assessment.Digest() {
			t.Fatalf("concurrent result=%+v", result.persisted)
		}
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_supervisor_assessments WHERE id=$1`, assessment.ID()).Scan(&count); err != nil || count != 1 {
		t.Fatalf("converged count=%d/%v", count, err)
	}

	changed := input
	changed.Checks = append([]dailysupervisor.CheckInput(nil), input.Checks...)
	for index := range changed.Checks {
		if changed.Checks[index].Name == dailysupervisor.CheckMarketData {
			changed.Checks[index].State = dailysupervisor.StateFail
			changed.Checks[index].Reason = "changed retry"
		}
	}
	changedAssessment, err := dailysupervisor.NewAssessment(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDailySupervisorRepo(pool).RegisterAssessment(ctx, changedAssessment); !errors.Is(err, ErrDailySupervisorConflict) {
		t.Fatalf("changed retry=%v", err)
	}

	restarted, err := NewDailySupervisorRepo(pool).GetAssessment(ctx, assessment.ID())
	if err != nil || restarted.SHA256 != assessment.Digest() || string(restarted.CanonicalBytes) != string(assessment.CanonicalBytes()) {
		t.Fatalf("restart=%+v/%v", restarted, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE daily_supervisor_assessments SET timezone=timezone WHERE id=$1`, assessment.ID()); err == nil {
		t.Fatal("append-only update accepted")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM daily_supervisor_assessments WHERE id=$1`, assessment.ID()); err == nil {
		t.Fatal("append-only delete accepted")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO daily_supervisor_action_blockers(assessment_id,action_sequence,sequence,check_name) VALUES($1,5,1,'database')`, assessment.ID()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("direct SQL action forgery accepted")
	}

	successorAt := evaluatedAt.Add(time.Minute)
	successorOccurrence, successorEffect := claimSupervisorEffect(t, ctx, scheduler, successorAt, "successor")
	successorInput := retainedSupervisorInput(successorAt, reconciliationID, reconciliationSHA, successorOccurrence, successorEffect)
	successorInput.Prior = &dailysupervisor.Prior{ID: assessment.ID(), SHA256: assessment.Digest(), EvaluatedAt: evaluatedAt}
	successor, err := dailysupervisor.NewAssessment(successorInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDailySupervisorRepo(pool).RegisterAssessment(ctx, successor); err != nil {
		t.Fatal(err)
	}
	forkAt := evaluatedAt.Add(2 * time.Minute)
	forkOccurrence, forkEffect := claimSupervisorEffect(t, ctx, scheduler, forkAt, "fork")
	forkInput := retainedSupervisorInput(forkAt, reconciliationID, reconciliationSHA, forkOccurrence, forkEffect)
	forkInput.Prior = &dailysupervisor.Prior{ID: assessment.ID(), SHA256: assessment.Digest(), EvaluatedAt: evaluatedAt}
	fork, err := dailysupervisor.NewAssessment(forkInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDailySupervisorRepo(pool).RegisterAssessment(ctx, fork); !errors.Is(err, ErrDailySupervisorConflict) {
		t.Fatalf("fork retry=%v", err)
	}

	failureAt := evaluatedAt.Add(24 * time.Hour)
	failureOccurrence, failureEffect := claimSupervisorEffect(t, ctx, scheduler, failureAt, "provider-failure")
	failureInput := retainedSupervisorInput(failureAt, reconciliationID, reconciliationSHA, failureOccurrence, failureEffect)
	for index := range failureInput.Checks {
		if failureInput.Checks[index].Name == dailysupervisor.CheckMarketData {
			failureInput.Checks[index].State = dailysupervisor.StateFail
			failureInput.Checks[index].Reason = "provider unavailable"
		}
	}
	failureAssessment, err := dailysupervisor.NewAssessment(failureInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewDailySupervisorRepo(pool).RegisterAssessment(ctx, failureAssessment); err != nil {
		t.Fatal(err)
	}
	if failureAssessment.Admission(dailysupervisor.WorkNewExposure) != dailysupervisor.AdmissionHalted {
		t.Fatal("failure day admitted new exposure")
	}
	for _, work := range []dailysupervisor.WorkClass{dailysupervisor.WorkProtectiveExit, dailysupervisor.WorkSettlement, dailysupervisor.WorkReconciliation} {
		if failureAssessment.Admission(work) != dailysupervisor.AdmissionEligible {
			t.Fatalf("failure day halted safe work %s", work)
		}
	}
	t.Logf("all_pass_id=%s sha256=%s", assessment.ID(), assessment.Digest())
	t.Logf("dependency_failure_id=%s sha256=%s", failureAssessment.ID(), failureAssessment.Digest())
}

func claimSupervisorEffect(t *testing.T, ctx context.Context, repo *FinancialSchedulerRepo, dueAt time.Time, key string) (*financialscheduler.Occurrence, *financialscheduler.Effect) {
	t.Helper()
	occurrence, err := financialscheduler.NewOccurrence(financialscheduler.OccurrenceInput{JobKey: "daily_supervisor", ScheduleRevision: "daily-supervisor-v1@sha256:" + strings.Repeat("d", 64), Trigger: financialscheduler.TriggerScheduled, DueAt: dueAt})
	if err != nil {
		t.Fatal(err)
	}
	ownerID := economicid.DeterministicUUID("ovr605-owner", key)
	acquisition, err := repo.Acquire(ctx, occurrence, ownerID, time.Hour)
	if err != nil || !acquisition.Acquired {
		t.Fatalf("acquire=%+v/%v", acquisition, err)
	}
	payloadSHA := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	effect, err := financialscheduler.NewEffect(financialscheduler.EffectInput{OccurrenceID: occurrence.ID, Kind: financialscheduler.EffectSupervisor, BusinessKey: "operating-day/" + dueAt.Format("2006-01-02"), PayloadSHA256: payloadSHA})
	if err != nil {
		t.Fatal(err)
	}
	claimed, err := repo.ClaimEffect(ctx, acquisition.Lease, effect)
	if err != nil {
		t.Fatal(err)
	}
	return occurrence, claimed
}

func retainedSupervisorInput(evaluatedAt time.Time, reconciliationID uuid.UUID, reconciliationSHA string, occurrence *financialscheduler.Occurrence, effect *financialscheduler.Effect) dailysupervisor.Input {
	checkNames := []dailysupervisor.CheckName{
		dailysupervisor.CheckDatabase, dailysupervisor.CheckSchema, dailysupervisor.CheckLedgerProjection,
		dailysupervisor.CheckMarketData, dailysupervisor.CheckRiskBrake, dailysupervisor.CheckReconciliation,
		dailysupervisor.CheckExposureScheduler, dailysupervisor.CheckExitWorker, dailysupervisor.CheckSettlementWorker,
		dailysupervisor.CheckReconciliationWorker,
	}
	checks := make([]dailysupervisor.CheckInput, 0, len(checkNames))
	for index, name := range checkNames {
		checks = append(checks, dailysupervisor.CheckInput{Name: name, State: dailysupervisor.StatePass, EvidenceID: economicid.DeterministicUUID("ovr605-check", evaluatedAt.String(), string(name)), EvidenceSHA256: strings.Repeat(string("123456789abcdef0"[index]), 64), ObservedAt: evaluatedAt.Add(-time.Minute), FreshThrough: evaluatedAt.Add(time.Hour)})
	}
	return dailysupervisor.Input{
		OperatingDay: evaluatedAt.Format("2006-01-02"), Timezone: "UTC", EvaluatedAt: evaluatedAt,
		PolicyVersion:       dailysupervisor.PolicyVersionPrefix + strings.Repeat("b", 64),
		Reconciliation:      dailysupervisor.ReconciliationReference{ID: reconciliationID, SHA256: reconciliationSHA, Clean: true},
		SchedulerOccurrence: occurrence, SchedulerEffect: effect, Checks: checks,
	}
}
