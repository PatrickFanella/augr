package experimentrun

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestResultDerivesMetricsAndReproducesExactly(t *testing.T) {
	plan, err := NewPlan(validPlanInput())
	if err != nil {
		t.Fatal(err)
	}
	input := validResultInput(plan)
	first, err := NewResult(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewResult(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != retry.ID() || first.Digest() != retry.Digest() {
		t.Fatal("identical result did not converge")
	}
	metrics := first.Metrics()
	if metrics.StepCount != 2 || metrics.NoopCount != 1 || metrics.IntentCount != 1 || metrics.OrderCount != 1 || metrics.TransitionCount != 2 || metrics.FillCount != 1 || metrics.FilledQuantity != "10" || metrics.FeeTotal != "1.25" {
		t.Fatalf("metrics=%+v", metrics)
	}
	restored, err := ResultFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	input.Outcomes[1].FeeTotal = "1.5"
	changed, err := NewResult(input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() == first.ID() {
		t.Fatal("changed execution metrics reused result identity")
	}
}

func TestResultRejectsPlanMismatchAndTamperedDerivedMetrics(t *testing.T) {
	plan, err := NewPlan(validPlanInput())
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*ResultInput){
		"decision":      func(v *ResultInput) { v.Outcomes[1].DecisionSHA256 = strings.Repeat("f", 64) },
		"intent":        func(v *ResultInput) { v.Outcomes[1].IntentID = uuid.New() },
		"noop quantity": func(v *ResultInput) { v.Outcomes[0].FilledQuantity = "1" },
		"nil fill":      func(v *ResultInput) { v.Outcomes[1].FillIDs = []uuid.UUID{uuid.Nil} },
	} {
		t.Run(name, func(t *testing.T) {
			input := validResultInput(plan)
			mutate(&input)
			if _, err := NewResult(input); err == nil {
				t.Fatal("invalid result succeeded")
			}
		})
	}
	result, err := NewResult(validResultInput(plan))
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(result.CanonicalBytes(), []byte(`"fill_count":1`), []byte(`"fill_count":2`), 1)
	if _, err := ResultFromCanonical(economicResultID(t, tampered), hashBytes(tampered), tampered); err == nil {
		t.Fatal("tampered derived metrics restored")
	}
}

func TestAttemptEventsRequireOneStartedAndOneTerminalEvent(t *testing.T) {
	attemptID := uuid.New()
	startedAt := time.Date(2026, 8, 20, 20, 0, 0, 123456000, time.UTC)
	started, err := NewAttemptEvent(AttemptEventInput{AttemptID: attemptID, Sequence: 0, Type: AttemptStarted, OccurredAt: startedAt})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := NewAttemptEvent(AttemptEventInput{AttemptID: attemptID, Sequence: 1, Type: AttemptCompleted, OccurredAt: startedAt.Add(time.Second), ResultID: uuid.New()})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttempt([]*AttemptEvent{started, completed}); err != nil {
		t.Fatal(err)
	}
	restored, err := AttemptEventFromCanonical(completed.ID(), completed.Digest(), completed.CanonicalBytes())
	if err != nil || restored.ResultID() != completed.ResultID() {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	failed, err := NewAttemptEvent(AttemptEventInput{AttemptID: attemptID, Sequence: 1, Type: AttemptFailed, OccurredAt: startedAt.Add(time.Second), ErrorCode: "injected_failure", ErrorSHA256: strings.Repeat("a", 64)})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttempt([]*AttemptEvent{started, failed}); err != nil {
		t.Fatal(err)
	}
	if err := ValidateAttempt([]*AttemptEvent{started}); err == nil {
		t.Fatal("incomplete attempt succeeded")
	}
	if _, err := NewAttemptEvent(AttemptEventInput{AttemptID: attemptID, Sequence: 1, Type: AttemptCompleted, OccurredAt: startedAt}); err == nil {
		t.Fatal("completion without result succeeded")
	}
}

func validResultInput(plan *Plan) ResultInput {
	return ResultInput{Plan: plan, AccountID: uuid.MustParse("30300000-0000-4000-8000-000000000020"), QualityResultID: uuid.MustParse("30300000-0000-4000-8000-000000000021"), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("a", 64), CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("b", 64), Outcomes: []StepOutcomeInput{
		{Action: ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"},
		{Action: ActionExecute, DecisionSHA256: plan.DecisionSHA256(1), IntentID: plan.IntentID(1), OrderID: plan.OrderID(1), TransitionIDs: []uuid.UUID{uuid.MustParse("30300000-0000-4000-8000-000000000022"), uuid.MustParse("30300000-0000-4000-8000-000000000023")}, FillIDs: []uuid.UUID{uuid.MustParse("30300000-0000-4000-8000-000000000024")}, FilledQuantity: "10", FeeTotal: "1.25", AggregateSHA256: strings.Repeat("c", 64), OutcomeSHA256: strings.Repeat("d", 64)},
	}}
}

func economicResultID(t *testing.T, raw []byte) uuid.UUID {
	t.Helper()
	return economicid.DeterministicUUID(resultDomain, ResultSchemaV1+"@sha256:"+hashBytes(raw))
}
