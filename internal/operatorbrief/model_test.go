package operatorbrief

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/costattribution"
	"github.com/PatrickFanella/get-rich-quick/internal/dailysupervisor"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	reviewqualification "github.com/PatrickFanella/get-rich-quick/internal/evidencereview/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
	researchqualification "github.com/PatrickFanella/get-rich-quick/internal/researchworkflow/qualification"
)

func briefInput(t *testing.T, attention bool) Input {
	failure := dailysupervisor.CheckName("")
	if attention {
		failure = dailysupervisor.CheckMarketData
	}
	return briefInputWithFailure(t, failure, attention, true)
}

func briefInputWithFailure(t *testing.T, failure dailysupervisor.CheckName, infrastructureUnknown, reconciliationClean bool) Input {
	t.Helper()
	evaluated := time.Date(2026, 11, 1, 7, 30, 0, 0, time.UTC)
	occurrence, err := financialscheduler.NewOccurrence(financialscheduler.OccurrenceInput{JobKey: "daily_supervisor", ScheduleRevision: "daily-supervisor-v1", Trigger: financialscheduler.TriggerScheduled, DueAt: evaluated})
	if err != nil {
		t.Fatal(err)
	}
	effect, err := financialscheduler.NewEffect(financialscheduler.EffectInput{OccurrenceID: occurrence.ID, Kind: financialscheduler.EffectSupervisor, BusinessKey: "operating-day/2026-11-01", PayloadSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	checkNames := []dailysupervisor.CheckName{dailysupervisor.CheckDatabase, dailysupervisor.CheckSchema, dailysupervisor.CheckLedgerProjection, dailysupervisor.CheckMarketData, dailysupervisor.CheckRiskBrake, dailysupervisor.CheckReconciliation, dailysupervisor.CheckExposureScheduler, dailysupervisor.CheckExitWorker, dailysupervisor.CheckSettlementWorker, dailysupervisor.CheckReconciliationWorker}
	checks := make([]dailysupervisor.CheckInput, 0, len(checkNames))
	for index, name := range checkNames {
		state := dailysupervisor.StatePass
		reason := ""
		if name == failure {
			state = dailysupervisor.StateFail
			reason = "required dependency failed"
		}
		checks = append(checks, dailysupervisor.CheckInput{Name: name, State: state, EvidenceID: economicid.DeterministicUUID("brief-check", string(name)), EvidenceSHA256: strings.Repeat(string("123456789abcdef0"[index]), 64), ObservedAt: evaluated.Add(-time.Minute), FreshThrough: evaluated.Add(time.Hour), Reason: reason})
	}
	incidentCount := 0
	if !reconciliationClean {
		incidentCount = 1
	}
	supervisor, err := dailysupervisor.NewAssessment(dailysupervisor.Input{OperatingDay: "2026-11-01", Timezone: "America/New_York", EvaluatedAt: evaluated, PolicyVersion: dailysupervisor.PolicyVersionPrefix + strings.Repeat("b", 64), Reconciliation: dailysupervisor.ReconciliationReference{ID: economicid.DeterministicUUID("brief-reconciliation", fmt.Sprint(reconciliationClean)), SHA256: strings.Repeat("c", 64), Clean: reconciliationClean, IncidentCount: incidentCount}, SchedulerOccurrence: occurrence, SchedulerEffect: effect, Checks: checks})
	if err != nil {
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
	costLines := []costattribution.LineInput{{Key: "model", Category: costattribution.CategoryModel, Status: costattribution.StatusActual, Amount: research.Hypothesis.ProvenanceCost(), EvidenceKind: "research_hypothesis", EvidenceID: research.Hypothesis.ID(), EvidenceSHA256: research.Hypothesis.Digest(), Explanation: "Actual model cost."}, {Key: "data", Category: costattribution.CategoryData, Status: costattribution.StatusEstimated, Amount: "1", EvidenceKind: "dataset_manifest", EvidenceID: research.Parents.Manifest.ID(), EvidenceSHA256: research.Parents.Manifest.Digest(), Method: "partition_rate_v1", MethodSHA256: strings.Repeat("d", 64), Explanation: "Estimated data cost."}, {Key: "fee", Category: costattribution.CategoryFee, Status: costattribution.StatusEstimated, Amount: "0.2", EvidenceKind: "external_artifact", EvidenceID: uuid.MustParse("60700000-0000-4000-8000-000000000001"), EvidenceSHA256: strings.Repeat("e", 64), Method: "fee_estimate_v1", MethodSHA256: strings.Repeat("f", 64), Explanation: "Estimated fee."}, {Key: "rebate", Category: costattribution.CategoryRebate, Status: costattribution.StatusEstimated, Amount: "0.1", EvidenceKind: "external_artifact", EvidenceID: uuid.MustParse("60700000-0000-4000-8000-000000000002"), EvidenceSHA256: strings.Repeat("1", 64), Method: "rebate_estimate_v1", MethodSHA256: strings.Repeat("2", 64), Explanation: "Estimated rebate."}}
	if infrastructureUnknown {
		costLines = append(costLines, costattribution.LineInput{Key: "infrastructure", Category: costattribution.CategoryInfrastructure, Status: costattribution.StatusUnknown, Explanation: "Infrastructure allocation unavailable."})
	} else {
		costLines = append(costLines, costattribution.LineInput{Key: "infrastructure", Category: costattribution.CategoryInfrastructure, Status: costattribution.StatusEstimated, Amount: "0.5", EvidenceKind: "external_artifact", EvidenceID: uuid.MustParse("60700000-0000-4000-8000-000000000003"), EvidenceSHA256: strings.Repeat("3", 64), Method: "cpu_allocation_v1", MethodSHA256: strings.Repeat("4", 64), Explanation: "Estimated infrastructure."})
	}
	costs, err := costattribution.NewReport(costattribution.Input{Case: review.Case, Summary: review.Summary, Hypothesis: research.Hypothesis, Manifest: research.Parents.Manifest, AccountID: uuid.MustParse("60700000-0000-4000-8000-000000000004"), WindowStart: evaluated.Add(-24 * time.Hour), WindowEnd: evaluated, StatementAt: evaluated, Currency: "USD", Lines: costLines})
	if err != nil {
		t.Fatal(err)
	}
	return Input{OperatingDay: "2026-11-01", Timezone: "America/New_York", GeneratedAt: evaluated.Add(10 * time.Minute), Supervisor: supervisor, Costs: costs, Performance: PerformanceInput{EvaluationID: uuid.MustParse("60700000-0000-4000-8000-000000000005"), EvaluationSHA256: strings.Repeat("5", 64), Status: PerformancePositive, Headline: "After-cost performance remained positive.", Explanation: "The exact scored evaluation retained positive after-cost evidence.", Facts: []FactInput{{Key: "net_return", Value: "0.03"}, {Key: "trade_count", Value: "12"}}}}
}

func TestHealthyBriefHasFiveSectionsAndNoIncidents(t *testing.T) {
	brief, err := NewBrief(briefInput(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.Sections()) != 5 || len(brief.Incidents()) != 0 {
		t.Fatalf("sections/incidents=%d/%v", len(brief.Sections()), brief.Incidents())
	}
}
func TestAttentionBriefDerivesOpenIncidentsAndPreservesSafeWork(t *testing.T) {
	brief, err := NewBrief(briefInput(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.Incidents()) != 3 {
		t.Fatalf("incidents=%v", brief.Incidents())
	}
	for _, incident := range brief.Incidents() {
		if incident.State != "open" {
			t.Fatalf("incident=%+v", incident)
		}
	}
	risk := brief.Sections()[3]
	facts := map[string]string{}
	for _, fact := range risk.Facts {
		facts[fact.Key] = fact.Value
	}
	if facts["new_exposure"] != "halted" || facts["protective_exit"] != "eligible" || facts["settlement"] != "eligible" || facts["reconciliation"] != "eligible" {
		t.Fatalf("risk facts=%v", facts)
	}
}
func TestReconciliationDriftAndRiskFailureRemainVisible(t *testing.T) {
	drift, err := NewBrief(briefInputWithFailure(t, dailysupervisor.CheckReconciliation, false, false))
	if err != nil {
		t.Fatal(err)
	}
	if drift.Sections()[2].Status != "attention" || drift.Incidents()[0].Key != "supervisor_check:reconciliation" {
		t.Fatalf("drift sections/incidents=%+v/%+v", drift.Sections()[2], drift.Incidents())
	}
	risk, err := NewBrief(briefInputWithFailure(t, dailysupervisor.CheckRiskBrake, false, true))
	if err != nil {
		t.Fatal(err)
	}
	if risk.Sections()[3].Status != "restricted" {
		t.Fatalf("risk=%+v", risk.Sections()[3])
	}
	foundCritical := false
	for _, incident := range risk.Incidents() {
		foundCritical = foundCritical || incident.Key == "supervisor_check:risk_brake" && incident.Severity == "critical"
	}
	if !foundCritical {
		t.Fatalf("incidents=%+v", risk.Incidents())
	}
}
func TestUnavailablePerformanceBecomesIncidentWithoutFakeEvidence(t *testing.T) {
	in := briefInput(t, false)
	in.Performance = PerformanceInput{Status: PerformanceUnavailable, Headline: "Performance unavailable.", Explanation: "No completed scored evaluation was retained.", Facts: []FactInput{{Key: "reason", Value: "missing_evaluation"}}}
	brief, err := NewBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(brief.Incidents()) != 1 || brief.Incidents()[0].Key != "performance:unavailable" {
		t.Fatalf("incidents=%v", brief.Incidents())
	}
	in.Performance.EvaluationSHA256 = strings.Repeat("a", 64)
	if _, err := NewBrief(in); err == nil {
		t.Fatal("unavailable performance evidence accepted")
	}
}
func TestFactPermutationConvergesAndSemanticChangeDoesNot(t *testing.T) {
	in := briefInput(t, false)
	first, err := NewBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Performance.Facts[0], in.Performance.Facts[1] = in.Performance.Facts[1], in.Performance.Facts[0]
	second, err := NewBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != second.ID() {
		t.Fatal("fact permutation diverged")
	}
	in.Performance.Explanation += " Changed."
	third, err := NewBrief(in)
	if err != nil {
		t.Fatal(err)
	}
	if third.ID() == first.ID() {
		t.Fatal("semantic change reused identity")
	}
}
func TestTimezoneAndDSTDayFailClosed(t *testing.T) {
	in := briefInput(t, false)
	in.Timezone = "UTC"
	if _, err := NewBrief(in); err == nil {
		t.Fatal("supervisor timezone mismatch accepted")
	}
	in = briefInput(t, false)
	in.OperatingDay = "2026-10-31"
	if _, err := NewBrief(in); err == nil {
		t.Fatal("wrong DST day accepted")
	}
}
func TestBriefHasNoNotificationOrMutationAuthoritySurface(t *testing.T) {
	brief, err := NewBrief(briefInput(t, true))
	if err != nil {
		t.Fatal(err)
	}
	if brief.ID() == uuid.Nil || len(brief.CanonicalBytes()) == 0 {
		t.Fatal("brief evidence missing")
	}
}
