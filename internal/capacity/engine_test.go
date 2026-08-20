package capacity

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/capital"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	definedriskqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type fakeEvaluation struct {
	id        uuid.UUID
	programID uuid.UUID
	digest    string
	raw       json.RawMessage
	value     string
}

func newFakeEvaluation(value string) *fakeEvaluation {
	raw := json.RawMessage(`{"schema":"trade-portfolio-evaluation-v1","state":"completed"}`)
	return &fakeEvaluation{id: uuid.NewSHA1(uuid.NameSpaceOID, []byte("capacity/evaluation/"+value)), programID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("capacity/program")), digest: hash(raw), raw: raw, value: value}
}
func (f *fakeEvaluation) ID() uuid.UUID        { return f.id }
func (f *fakeEvaluation) ProgramID() uuid.UUID { return f.programID }
func (f *fakeEvaluation) Digest() string       { return f.digest }
func (f *fakeEvaluation) CanonicalBytes() json.RawMessage {
	return append(json.RawMessage(nil), f.raw...)
}

func (f *fakeEvaluation) Mode() strategycatalog.ExperimentMode {
	return strategycatalog.ExperimentPaperScored
}

func (f *fakeEvaluation) EvaluationStart() time.Time {
	return time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
}

func (f *fakeEvaluation) EvaluationEnd() time.Time { return f.EvaluationStart().Add(24 * time.Hour) }

func (f *fakeEvaluation) Metrics() []evaluation.Metric {
	return []evaluation.Metric{{Section: "portfolio", Name: "after_cost_total_return", State: evaluation.MetricAvailable, Value: f.value}}
}

func TestComparisonRetainsSixTiersUnavailableFamiliesAndSaturation(t *testing.T) {
	policy, err := capital.NewPolicy(capital.ReviewedPolicyV1Input())
	if err != nil {
		t.Fatal(err)
	}
	families := []FamilyKind{FamilyPassive, FamilyWheel, FamilyMomentum, FamilyTrend}
	contracts := make([]*Contract, 0, 5)
	for _, family := range families {
		value, contractErr := newContract(newFakeEvaluation("0.01"), family, uuid.NewSHA1(uuid.NameSpaceOID, []byte(family)), strings.Repeat("a", 64), "0.01", false, "source_capacity_not_observed", "0", 0)
		if contractErr != nil {
			t.Fatal(contractErr)
		}
		contracts = append(contracts, value)
	}
	available, err := newContract(newFakeEvaluation("0.02"), FamilyDefinedRisk, uuid.NewSHA1(uuid.NameSpaceOID, []byte("defined")), strings.Repeat("b", 64), "0.02", true, "", "902", 10)
	if err != nil {
		t.Fatal(err)
	}
	contracts = append(contracts, available)
	comparison, err := NewComparison(policy, []*Contract{contracts[3], contracts[0], contracts[4], contracts[2], contracts[1]})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ComparisonFromCanonical(comparison.ID(), comparison.Digest(), comparison.CanonicalBytes(), policy, contracts)
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), comparison.CanonicalBytes()) {
		t.Fatalf("replay=%v", err)
	}
	for _, family := range comparison.Families() {
		if len(family.Tiers) != 6 {
			t.Fatalf("tiers=%+v", family)
		}
		if family.Family == FamilyDefinedRisk {
			if !family.MinimumViableAvailable || family.MinimumViableTier != "5000" || family.Tiers[0].Reason != "below_minimum_whole_unit" || !family.Tiers[2].Saturated || family.Tiers[2].Units != 10 {
				t.Fatalf("defined=%+v", family)
			}
		} else if family.MinimumViableAvailable || family.Tiers[0].Reason != "source_capacity_not_observed" {
			t.Fatalf("unavailable=%+v", family)
		}
	}
}

func TestDefinedRiskAdapterDerivesReservationAndTwoLegDepth(t *testing.T) {
	runnerFixture, err := definedriskqualification.BuildRunner(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	fixture := runnerFixture.Fixture
	var source struct {
		Return string `json:"after_cost_total_return"`
	}
	if json.Unmarshal(fixture.Report.CanonicalBytes(), &source) != nil {
		t.Fatal("report")
	}
	evaluationEvidence := newFakeEvaluation(source.Return)
	evaluationEvidence.programID = runnerFixture.Program.Identity().ID()
	contract, err := FromDefinedRisk(evaluationEvidence, fixture.Scenario, runnerFixture.Program)
	if err != nil {
		t.Fatal(err)
	}
	if !contract.canonical.CapacityAvailable || contract.canonical.MaximumUnits != 10 || contract.canonical.CapitalPerUnit != "122" {
		t.Fatalf("contract=%+v", contract.canonical)
	}
	evaluationEvidence.value = "0"
	if _, err = FromDefinedRisk(evaluationEvidence, fixture.Scenario, runnerFixture.Program); err == nil {
		t.Fatal("return mismatch accepted")
	}
}
