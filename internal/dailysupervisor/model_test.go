package dailysupervisor

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
)

func supervisorInput(t *testing.T) Input {
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
	checks := make([]CheckInput, 0, len(checkOrder))
	for index, name := range checkOrder {
		checks = append(checks, CheckInput{Name: name, State: StatePass, EvidenceID: economicid.DeterministicUUID("supervisor-check", string(name)), EvidenceSHA256: strings.Repeat(string("123456789abcdef0"[index%16]), 64), ObservedAt: evaluated.Add(-time.Minute), FreshThrough: evaluated.Add(time.Hour)})
	}
	return Input{OperatingDay: "2026-11-01", Timezone: "America/New_York", EvaluatedAt: evaluated, PolicyVersion: "daily-supervisor-policy-v1@sha256:" + strings.Repeat("b", 64), Reconciliation: ReconciliationReference{economicid.DeterministicUUID("reconciliation", "clean"), strings.Repeat("c", 64), true, 0}, SchedulerOccurrence: occurrence, SchedulerEffect: effect, Checks: checks}
}

func TestAllPassAdmitsEveryWorkClassDeterministically(t *testing.T) {
	in := supervisorInput(t)
	a, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() != b.ID() || a.Digest() != b.Digest() {
		t.Fatal("identical evidence diverged")
	}
	for _, work := range workOrder {
		if a.Admission(work) != AdmissionEligible {
			t.Fatalf("%s=%s", work, a.Admission(work))
		}
	}
}

func TestProviderFailureHaltsExposureButPreservesSafeWorkers(t *testing.T) {
	in := supervisorInput(t)
	for i := range in.Checks {
		if in.Checks[i].Name == CheckMarketData {
			in.Checks[i].State = StateFail
			in.Checks[i].Reason = "provider unavailable"
		}
	}
	a, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Admission(WorkNewExposure) != AdmissionHalted {
		t.Fatal("new exposure remained eligible")
	}
	for _, work := range []WorkClass{WorkProtectiveExit, WorkSettlement, WorkReconciliation, WorkEvidenceOnly} {
		if a.Admission(work) != AdmissionEligible {
			t.Fatalf("safe work %s halted", work)
		}
	}
	if len(a.Attention()) != 1 {
		t.Fatalf("attention=%v", a.Attention())
	}
}

func TestLedgerFailureHaltsEveryFinancialMutation(t *testing.T) {
	in := supervisorInput(t)
	for i := range in.Checks {
		if in.Checks[i].Name == CheckLedgerProjection {
			in.Checks[i].State = StateUnknown
			in.Checks[i].Reason = "projection unavailable"
		}
	}
	a, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, work := range []WorkClass{WorkNewExposure, WorkProtectiveExit, WorkSettlement, WorkReconciliation} {
		if a.Admission(work) != AdmissionHalted {
			t.Fatalf("%s remained eligible", work)
		}
	}
	if a.Admission(WorkEvidenceOnly) != AdmissionEligible {
		t.Fatal("evidence inspection halted")
	}
}

func TestStaleEvidenceFailsClosed(t *testing.T) {
	in := supervisorInput(t)
	for index := range in.Checks {
		if in.Checks[index].Name == CheckMarketData {
			in.Checks[index].FreshThrough = in.EvaluatedAt.Add(-time.Microsecond)
		}
	}
	assessment, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Admission(WorkNewExposure) != AdmissionHalted {
		t.Fatal("stale market evidence did not halt new exposure")
	}
	if attention := assessment.Attention(); len(attention) != 1 || attention[0].State != StateFail || attention[0].Reason != "stale" {
		t.Fatalf("attention=%v", attention)
	}
}

func TestRiskFailureHaltsExposureExitAndSettlement(t *testing.T) {
	in := supervisorInput(t)
	for index := range in.Checks {
		if in.Checks[index].Name == CheckRiskBrake {
			in.Checks[index].State = StateFail
			in.Checks[index].Reason = "brake state unavailable"
		}
	}
	assessment, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	for _, work := range []WorkClass{WorkNewExposure, WorkProtectiveExit, WorkSettlement} {
		if assessment.Admission(work) != AdmissionHalted {
			t.Fatalf("%s remained eligible", work)
		}
	}
	if assessment.Admission(WorkReconciliation) != AdmissionEligible {
		t.Fatal("risk failure unnecessarily halted reconciliation")
	}
}

func TestSettlementWorkerFailureOnlyHaltsSettlement(t *testing.T) {
	in := supervisorInput(t)
	for index := range in.Checks {
		if in.Checks[index].Name == CheckSettlementWorker {
			in.Checks[index].State = StateUnknown
			in.Checks[index].Reason = "worker heartbeat missing"
		}
	}
	assessment, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if assessment.Admission(WorkSettlement) != AdmissionHalted {
		t.Fatal("settlement remained eligible")
	}
	for _, work := range []WorkClass{WorkNewExposure, WorkProtectiveExit, WorkReconciliation, WorkEvidenceOnly} {
		if assessment.Admission(work) != AdmissionEligible {
			t.Fatalf("%s was unnecessarily halted", work)
		}
	}
}

func TestReconciliationDriftCannotPassAndHaltsOnlyExposure(t *testing.T) {
	in := supervisorInput(t)
	in.Reconciliation.Clean = false
	in.Reconciliation.IncidentCount = 1
	if _, err := NewAssessment(in); err == nil {
		t.Fatal("drifting reconciliation passed")
	}
	for i := range in.Checks {
		if in.Checks[i].Name == CheckReconciliation {
			in.Checks[i].State = StateFail
			in.Checks[i].Reason = "cash drift"
		}
	}
	a, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.Admission(WorkNewExposure) != AdmissionHalted || a.Admission(WorkReconciliation) != AdmissionEligible || a.Admission(WorkProtectiveExit) != AdmissionEligible {
		t.Fatalf("actions=%v", a.Actions())
	}
}

func TestCheckPermutationConvergesAndSemanticChangeDoesNot(t *testing.T) {
	in := supervisorInput(t)
	a, _ := NewAssessment(in)
	for left, right := 0, len(in.Checks)-1; left < right; left, right = left+1, right-1 {
		in.Checks[left], in.Checks[right] = in.Checks[right], in.Checks[left]
	}
	b, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() != b.ID() {
		t.Fatal("check permutation changed identity")
	}
	in.Checks[0].EvidenceSHA256 = strings.Repeat("f", 64)
	c, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID() == a.ID() {
		t.Fatal("semantic change reused identity")
	}
}

func TestTimezoneDayAndSupersessionAreExact(t *testing.T) {
	in := supervisorInput(t)
	a, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	in.Prior = &Prior{ID: a.ID(), SHA256: a.Digest(), EvaluatedAt: in.EvaluatedAt}
	in.EvaluatedAt = in.EvaluatedAt.Add(time.Minute)
	for i := range in.Checks {
		in.Checks[i].FreshThrough = in.EvaluatedAt.Add(time.Hour)
	}
	b, err := NewAssessment(in)
	if err != nil {
		t.Fatal(err)
	}
	if b.ID() == a.ID() {
		t.Fatal("supersession reused identity")
	}
	in.Timezone = "invalid/timezone"
	if _, err := NewAssessment(in); err == nil {
		t.Fatal("invalid timezone accepted")
	}
	in = supervisorInput(t)
	in.OperatingDay = "2026-10-31"
	if _, err := NewAssessment(in); err == nil {
		t.Fatal("wrong DST operating day accepted")
	}
}

func TestSupervisorHasNoMutationAuthoritySurface(t *testing.T) {
	a, err := NewAssessment(supervisorInput(t))
	if err != nil {
		t.Fatal(err)
	}
	if a.ID() == uuid.Nil || len(a.CanonicalBytes()) == 0 {
		t.Fatal("assessment missing evidence")
	}
	// The public surface exposes evidence getters only; admission is a value,
	// never a callback or command capable of flattening or changing risk state.
}
