package experimentrun

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestPlanReproducesStableIntentAndOrderIdentities(t *testing.T) {
	input := validPlanInput()
	first, err := NewPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewPlan(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.ID() != retry.ID() || first.IntentID(1) != retry.IntentID(1) || first.OrderID(1) != retry.OrderID(1) || first.IntentID(0) != uuid.Nil {
		t.Fatal("plan replay identities did not converge")
	}
	restored, err := PlanFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || restored.OrderID(1) != first.OrderID(1) {
		t.Fatalf("restore=%+v err=%v", restored, err)
	}
	changed := validPlanInput()
	changed.Steps[0], changed.Steps[1] = changed.Steps[1], changed.Steps[0]
	reordered, err := NewPlan(changed)
	if err != nil {
		t.Fatal(err)
	}
	if reordered.ID() == first.ID() {
		t.Fatal("semantic step reorder reused plan identity")
	}
}

func TestPlanRejectsDuplicateFutureAndNoncanonicalEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*PlanInput){
		"duplicate":              func(v *PlanInput) { v.Steps = append(v.Steps, v.Steps[0]) },
		"future":                 func(v *PlanInput) { v.Steps[0].AvailableAt = v.EvaluationEnd.Add(time.Microsecond) },
		"noncanonical decision":  func(v *PlanInput) { v.Steps[0].Decision = json.RawMessage(`{ "signal":"hold"}`) },
		"noop intent":            func(v *PlanInput) { v.Steps[0].Intent = v.Steps[1].Intent },
		"execute without intent": func(v *PlanInput) { v.Steps[1].Intent = nil },
	} {
		t.Run(name, func(t *testing.T) {
			input := validPlanInput()
			mutate(&input)
			if _, err := NewPlan(input); err == nil {
				t.Fatal("invalid plan succeeded")
			}
		})
	}
}

func TestPlanTamperingAndModeSeparation(t *testing.T) {
	plan, err := NewPlan(validPlanInput())
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(plan.CanonicalBytes(), []byte(`"buy"`), []byte(`"sell"`), 1)
	if _, err := PlanFromCanonical(plan.ID(), hashBytes(tampered), tampered); err == nil {
		t.Fatal("tampered plan restored")
	}
	stress := validPlanInput()
	stress.Mode = strategycatalog.ExperimentPaperStress
	stressPlan, err := NewPlan(stress)
	if err != nil {
		t.Fatal(err)
	}
	if stressPlan.ID() == plan.ID() {
		t.Fatal("scored and stress plans converged")
	}
}

func validPlanInput() PlanInput {
	start := time.Date(2026, 8, 20, 19, 0, 0, 123456000, time.UTC)
	limit := "10.25"
	return PlanInput{ExperimentID: uuid.MustParse("30300000-0000-4000-8000-000000000010"), ProgramID: uuid.MustParse("30300000-0000-4000-8000-000000000011"), ManifestID: uuid.MustParse("30300000-0000-4000-8000-000000000012"), ManifestSHA256: strings.Repeat("a", 64), EvaluationStart: start, EvaluationEnd: start.Add(2 * time.Hour), Seed: 303, Mode: strategycatalog.ExperimentPaperScored, Steps: []StepInput{
		{PartitionContentSHA256: strings.Repeat("b", 64), ObservationSourceKey: "quote-1", ObservationContentSHA256: strings.Repeat("c", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: ActionNoop},
		{PartitionContentSHA256: strings.Repeat("b", 64), ObservationSourceKey: "quote-2", ObservationContentSHA256: strings.Repeat("d", 64), AvailableAt: start.Add(2 * time.Minute), Decision: json.RawMessage(`{"signal":"buy"}`), Action: ActionExecute, Intent: &IntentSpecInput{InstrumentID: uuid.MustParse("30300000-0000-4000-8000-000000000013"), VenueContractID: uuid.MustParse("30300000-0000-4000-8000-000000000014"), Side: "buy", OrderType: "limit", TimeInForce: "day", Quantity: "10", LimitPrice: &limit, DecisionAt: start.Add(2 * time.Minute), RouteAt: start.Add(3 * time.Minute)}},
	}}
}
