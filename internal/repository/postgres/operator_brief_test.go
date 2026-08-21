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

	"github.com/PatrickFanella/get-rich-quick/internal/costattribution"
	"github.com/PatrickFanella/get-rich-quick/internal/dailysupervisor"
	reviewqualification "github.com/PatrickFanella/get-rich-quick/internal/evidencereview/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/operatorbrief"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestOperatorBriefRetainedQualification(t *testing.T) {
	url := os.Getenv("OPERATOR_BRIEF_QUALIFICATION_DB_URL")
	if url == "" {
		t.Skip("set OPERATOR_BRIEF_QUALIFICATION_DB_URL to the dedicated composed schema-101 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_operator_briefs`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("existing=%d/%v", count, err)
	}
	var reconciliationID uuid.UUID
	var reconciliationSHA string
	if err := pool.QueryRow(ctx, `SELECT id,sha256 FROM venue_reconciliation_runs WHERE clean ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&reconciliationID, &reconciliationSHA); err != nil {
		t.Fatal(err)
	}
	allPass := retainedSupervisorAssessment(t, time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC), "all-pass", reconciliationID, reconciliationSHA, false)
	failure := retainedSupervisorAssessment(t, time.Date(2026, 8, 21, 14, 0, 0, 0, time.UTC), "provider-failure", reconciliationID, reconciliationSHA, true)
	if allPass.ID() != uuid.MustParse("49af1a6a-3b27-cac1-e18a-f8a58277cbc3") || failure.ID() != uuid.MustParse("4cb14b93-bcc8-26df-8c65-0430032256eb") {
		t.Fatalf("supervisor identities=%s/%s", allPass.ID(), failure.ID())
	}
	var accountID, feeID, rebateID uuid.UUID
	var feeSHA, rebateSHA string
	if err := pool.QueryRow(ctx, `SELECT id FROM accounts ORDER BY id LIMIT 1`).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id,full_cost_ledger_evidence_sha256(id) FROM ledger_transactions WHERE event_type='cost.fee' AND origin_type='ovr606_qualification' LIMIT 1`).Scan(&feeID, &feeSHA); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id,full_cost_ledger_evidence_sha256(id) FROM ledger_transactions WHERE event_type='cost.rebate' AND origin_type='ovr606_qualification' LIMIT 1`).Scan(&rebateID, &rebateSHA); err != nil {
		t.Fatal(err)
	}
	review, err := reviewqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	research, err := researchqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	completeInput := qualificationCostInput(review.HeldCase, review.HeldSummary, research, accountID, start, end, feeID, feeSHA, rebateID, rebateSHA, false)
	completeCosts, err := costattribution.NewReport(completeInput)
	if err != nil {
		t.Fatal(err)
	}
	incompleteInput := qualificationCostInput(review.Case, review.Summary, research, accountID, start, end, feeID, feeSHA, rebateID, rebateSHA, true)
	incompleteCosts, err := costattribution.NewReport(incompleteInput)
	if err != nil {
		t.Fatal(err)
	}
	baselineInput := retainedBriefInput(allPass, completeCosts)
	baseline, err := operatorbrief.NewBrief(baselineInput)
	if err != nil {
		t.Fatal(err)
	}
	for _, failedStage := range []string{"brief", "sections", "facts", "incidents"} {
		repo := NewOperatorBriefRepo(pool)
		repo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := repo.RegisterBrief(ctx, baseline); err == nil {
			t.Fatalf("stage %s accepted", failedStage)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM daily_operator_briefs WHERE id=$1`, baseline.ID()).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failedStage, count, err)
		}
	}
	type result struct {
		persisted *PersistedOperatorBrief
		err       error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			p, e := NewOperatorBriefRepo(pool).RegisterBrief(ctx, baseline)
			results <- result{p, e}
		}()
	}
	wait.Wait()
	close(results)
	for result := range results {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		if result.persisted.ID != baseline.ID() || result.persisted.SHA256 != baseline.Digest() {
			t.Fatalf("result=%+v", result.persisted)
		}
	}
	changedInput := baselineInput
	changedInput.Performance.Explanation = "A changed unavailable-performance explanation."
	changed, err := operatorbrief.NewBrief(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOperatorBriefRepo(pool).RegisterBrief(ctx, changed); !errors.Is(err, ErrOperatorBriefConflict) {
		t.Fatalf("changed=%v", err)
	}
	restarted, err := NewOperatorBriefRepo(pool).GetBrief(ctx, baseline.ID())
	if err != nil || restarted.SHA256 != baseline.Digest() || string(restarted.CanonicalBytes) != string(baseline.CanonicalBytes()) {
		t.Fatalf("restart=%+v/%v", restarted, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE daily_operator_briefs SET timezone=timezone WHERE id=$1`, baseline.ID()); err == nil {
		t.Fatal("update accepted")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM daily_operator_briefs WHERE id=$1`, baseline.ID()); err == nil {
		t.Fatal("delete accepted")
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO daily_operator_brief_incidents(brief_id,sequence,incident_key,severity,incident_state,source_kind,source_sha256,summary,required_action,canonical_row) VALUES($1,1,'forged','high','open','forged','','forged','forged','{}')`, baseline.ID()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("direct SQL incident forgery accepted")
	}
	attentionInput := retainedBriefInput(failure, incompleteCosts)
	attention, err := operatorbrief.NewBrief(attentionInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewOperatorBriefRepo(pool).RegisterBrief(ctx, attention); err != nil {
		t.Fatal(err)
	}
	if len(attention.Incidents()) != 4 {
		t.Fatalf("attention incidents=%v", attention.Incidents())
	}
	risk := attention.Sections()[3]
	facts := map[string]string{}
	for _, fact := range risk.Facts {
		facts[fact.Key] = fact.Value
	}
	if facts["new_exposure"] != "halted" || facts["protective_exit"] != "eligible" || facts["settlement"] != "eligible" || facts["reconciliation"] != "eligible" {
		t.Fatalf("risk=%v", facts)
	}
	t.Logf("baseline_id=%s sha256=%s incidents=%d", baseline.ID(), baseline.Digest(), len(baseline.Incidents()))
	t.Logf("attention_id=%s sha256=%s incidents=%d", attention.ID(), attention.Digest(), len(attention.Incidents()))
}

func retainedSupervisorAssessment(t *testing.T, evaluated time.Time, key string, reconciliationID uuid.UUID, reconciliationSHA string, failure bool) *dailysupervisor.Assessment {
	t.Helper()
	occurrence, err := financialscheduler.NewOccurrence(financialscheduler.OccurrenceInput{JobKey: "daily_supervisor", ScheduleRevision: "daily-supervisor-v1@sha256:" + strings.Repeat("d", 64), Trigger: financialscheduler.TriggerScheduled, DueAt: evaluated})
	if err != nil {
		t.Fatal(err)
	}
	payload := fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
	effect, err := financialscheduler.NewEffect(financialscheduler.EffectInput{OccurrenceID: occurrence.ID, Kind: financialscheduler.EffectSupervisor, BusinessKey: "operating-day/" + evaluated.Format("2006-01-02"), PayloadSHA256: payload})
	if err != nil {
		t.Fatal(err)
	}
	input := retainedSupervisorInput(evaluated, reconciliationID, reconciliationSHA, occurrence, effect)
	if failure {
		for index := range input.Checks {
			if input.Checks[index].Name == dailysupervisor.CheckMarketData {
				input.Checks[index].State = dailysupervisor.StateFail
				input.Checks[index].Reason = "provider unavailable"
			}
		}
	}
	assessment, err := dailysupervisor.NewAssessment(input)
	if err != nil {
		t.Fatal(err)
	}
	return assessment
}

func retainedBriefInput(supervisor *dailysupervisor.Assessment, costs *costattribution.Report) operatorbrief.Input {
	record := supervisor.Record()
	generated := record.EvaluatedAt.Add(10 * time.Minute)
	return operatorbrief.Input{OperatingDay: record.OperatingDay, Timezone: record.Timezone, GeneratedAt: generated, Supervisor: supervisor, Costs: costs, Performance: operatorbrief.PerformanceInput{Status: operatorbrief.PerformanceUnavailable, Headline: "Performance evaluation unavailable.", Explanation: "No completed scored OVR-304 evaluation was retained in the composed qualification graph.", Facts: []operatorbrief.FactInput{{Key: "reason", Value: "missing_evaluation"}}}}
}
