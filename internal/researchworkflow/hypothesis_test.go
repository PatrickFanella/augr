package researchworkflow

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/generativestrategy"
	gsqualification "github.com/PatrickFanella/get-rich-quick/internal/generativestrategy/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestHypothesisDeterministicPermutationRestoreAndEdit(t *testing.T) {
	input := validHypothesisInput(t)
	first, err := NewHypothesis(input)
	if err != nil {
		t.Fatal(err)
	}
	permuted := input
	permuted.Sources = []SourceInput{input.Sources[1], input.Sources[0]}
	permuted.Searches = []SearchInput{input.Searches[1], input.Searches[0]}
	permuted.Searches[0].Results = []SearchResultInput{input.Searches[1].Results[1], input.Searches[1].Results[0]}
	permuted.Tests = append([]TestInput(nil), input.Tests...)
	for left, right := 0, len(permuted.Tests)-1; left < right; left, right = left+1, right-1 {
		permuted.Tests[left], permuted.Tests[right] = permuted.Tests[right], permuted.Tests[left]
	}
	second, err := NewHypothesis(permuted)
	if err != nil || second.ID() != first.ID() || !bytes.Equal(second.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("permutation diverged: %v", err)
	}
	restored, err := HypothesisFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes(), input.Parents)
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("restore failed: %v", err)
	}
	changed := input
	changed.Claim = "A materially changed and still falsifiable claim."
	third, err := NewHypothesis(changed)
	if err != nil || third.ID() == first.ID() {
		t.Fatalf("semantic edit retained identity: %v", err)
	}
	tampered := bytes.Replace(first.CanonicalBytes(), []byte(`"state":"authored"`), []byte(`"state":"approved"`), 1)
	if _, err := HypothesisFromCanonical(first.ID(), hash(tampered), tampered, input.Parents); err == nil {
		t.Fatal("tampered artifact restored")
	}
}

func TestHypothesisRejectsIncompleteOrFutureEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*HypothesisInput){
		"future source": func(input *HypothesisInput) {
			input.Sources[0].AvailableAt = input.Parents.Manifest.DecisionCutoff().Add(time.Second)
		},
		"hash mismatch":           func(input *HypothesisInput) { input.Sources[0].ContentSHA256 = strings.Repeat("9", 64) },
		"missing selected source": func(input *HypothesisInput) { input.Searches[0].Results[0].Selected = false },
		"selected omitted":        func(input *HypothesisInput) { input.Searches[0].Results[0].SourceKey = "omitted_source" },
		"missing leakage test": func(input *HypothesisInput) {
			for index, value := range input.Tests {
				if value.Type == "leakage" {
					input.Tests = append(input.Tests[:index], input.Tests[index+1:]...)
					return
				}
			}
		},
		"missing generated test": func(input *HypothesisInput) { input.Tests = input.Tests[1:] },
	} {
		t.Run(name, func(t *testing.T) {
			input := validHypothesisInput(t)
			mutate(&input)
			if _, err := NewHypothesis(input); err == nil {
				t.Fatal("invalid hypothesis succeeded")
			}
		})
	}
}

func validHypothesisInput(t *testing.T) HypothesisInput {
	t.Helper()
	fixture, err := gsqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := generativestrategy.NewSpec(fixture.Input)
	if err != nil {
		t.Fatal(err)
	}
	version, receipt, err := generativestrategy.Compile(spec, strings.Repeat("1", 40), strings.Repeat("2", 64))
	if err != nil {
		t.Fatal(err)
	}
	policy, family, assessment := robustnessParents(t, version.ID())
	cutoff := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	availableA := cutoff.Add(-4 * time.Hour)
	availableB := cutoff.Add(-3 * time.Hour)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: cutoff, Partitions: []dataset.PartitionInput{{
		Kind: dataset.KindExternalObject, Provider: "licensed_provider", Source: "research_archive", Namespace: "research", RequestSHA256: strings.Repeat("3", 64), MediaType: "application/pdf", SymbologyVersion: "none", AdjustmentPolicy: "immutable", Timezone: "UTC", Calendar: "continuous", Revision: "v1", License: "licensed", RetentionPolicy: "permanent",
		Observations: []dataset.ObservationInput{
			{SourceKey: "source_a", InstrumentID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), EffectiveAt: cutoff.Add(-48 * time.Hour), ObservedAt: availableA, AvailableAt: availableA, Revision: "v1", ContentSHA256: strings.Repeat("4", 64)},
			{SourceKey: "source_b", InstrumentID: uuid.MustParse("20000000-0000-4000-8000-000000000002"), EffectiveAt: cutoff.Add(-36 * time.Hour), ObservedAt: availableB, AvailableAt: availableB, Revision: "v1", ContentSHA256: strings.Repeat("5", 64)},
		},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	tests := []TestInput{}
	var generated struct {
		PropertyTests []string `json:"property_tests"`
		ExampleTests  []struct {
			Key string `json:"key"`
		} `json:"example_tests"`
	}
	if err := json.Unmarshal(spec.CanonicalBytes(), &generated); err != nil {
		t.Fatal(err)
	}
	for _, key := range generated.PropertyTests {
		tests = append(tests, TestInput{Key: "property_" + key, Type: "spec_property", ExpectedOutcome: "The invariant holds.", AcceptanceRule: "The generated property test passes.", SpecTestKey: key})
	}
	for _, value := range generated.ExampleTests {
		tests = append(tests, TestInput{Key: "example_" + value.Key, Type: "spec_example", ExpectedOutcome: "The example matches its declared decision.", AcceptanceRule: "The generated example test passes.", SpecTestKey: value.Key})
	}
	for _, kind := range []string{"leakage", "cost", "baseline", "refutation"} {
		tests = append(tests, TestInput{Key: kind + "_gate", Type: kind, ExpectedOutcome: "The declared evidence satisfies the gate.", AcceptanceRule: "The named gate returns pass."})
	}
	return HypothesisInput{
		Parents: Parents{manifest, policy, family, assessment, spec, version, receipt}, WorkflowKey: "research_momentum_v1", Claim: "Momentum predicts positive net returns after declared costs.", Mechanism: "Delayed information diffusion creates persistent cross-sectional price movement.", PredictedObservation: "The candidate exceeds its baseline on held-out point-in-time observations.", NullHypothesis: "Net performance is no better than the declared baseline.", RefutationThreshold: "Reject when the held-out net mean is nonpositive.", EvaluationHorizon: "Two embargoed walk-forward folds of thirty calendar days.", AbstentionCondition: "Abstain when any required source is stale or unavailable.",
		Sources:    []SourceInput{{"paper_a", "https://example.com/a", "Publisher A", "Paper A", cutoff.Add(-72 * time.Hour), availableA, strings.Repeat("4", 64), "licensed", []string{"source_a"}}, {"paper_b", "https://example.com/b", "Publisher B", "Paper B", cutoff.Add(-60 * time.Hour), availableB, strings.Repeat("5", 64), "licensed", []string{"source_b"}}},
		Searches:   []SearchInput{{"search_a", "catalog", strings.Repeat("6", 64), cutoff.Add(-2 * time.Hour), []SearchResultInput{{"paper_a", 1, true}, {"discarded_a", 2, false}}}, {"search_b", "catalog", strings.Repeat("7", 64), cutoff.Add(-time.Hour), []SearchResultInput{{"paper_b", 1, true}, {"discarded_b", 2, false}}}},
		Provenance: ProvenanceInput{"openai", "gpt-5.6", strings.Repeat("8", 64), strings.Repeat("9", 64), strings.Repeat("a", 64), 2000, 800, "USD", "0.4"}, Tests: tests,
	}
}

func robustnessParents(t *testing.T, versionID uuid.UUID) (*robustness.Policy, *robustness.Family, *robustness.Assessment) {
	t.Helper()
	policy, err := robustness.NewPolicy(robustness.PolicyInput{Version: "robustness-policy-v1@research", FoldCount: 2, PurgeSeconds: 86400, EmbargoSeconds: 86400, BootstrapAlgorithm: "xorshift64star-iid-percentile-v1", BootstrapSeed: 602, BootstrapIterations: 1000, ConfidenceLevel: "0.95", FamilyWiseAlpha: "0.05", MaxLargestPositiveShare: "0.4", MaxTopDecilePositiveShare: "0.4", MaxPerturbationDegradation: "0.005", RequiredPerturbations: []string{"cost_up"}, DecimalScale: 12})
	if err != nil {
		t.Fatal(err)
	}
	family, err := robustness.NewFamily(robustness.FamilyInput{Name: "OVR-602 research family", HypothesisSHA256: strings.Repeat("b", 64), CandidateVersionIDs: []uuid.UUID{versionID}})
	if err != nil {
		t.Fatal(err)
	}
	folds := make([]robustness.FoldInput, 2)
	for index := range folds {
		start := time.Date(2026, 7, 1+index*10, 12, 0, 0, 0, time.UTC)
		baseline := researchEvaluationReport(t, versionID, start, []string{"100", "101", "102", "103"}, "base"+string(rune('a'+index)))
		perturbed := researchEvaluationReport(t, versionID, start, []string{"100", "100.8", "101.6", "102.4"}, "cost"+string(rune('a'+index)))
		folds[index] = robustness.FoldInput{TrainStart: start.Add(-6 * 24 * time.Hour), TrainEnd: start.Add(-2 * 24 * time.Hour), Baseline: baseline, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_declared_cost", Report: perturbed}}}
	}
	assessment, err := robustness.NewAssessment(robustness.AssessmentInput{Family: family, Policy: policy, Mode: strategycatalog.ExperimentPaperScored, Candidates: []robustness.CandidateInput{{VersionID: versionID, Folds: folds}}})
	if err != nil {
		t.Fatal(err)
	}
	return policy, family, assessment
}

func researchEvaluationReport(t *testing.T, versionID uuid.UUID, start time.Time, equities []string, salt string) *evaluation.Report {
	t.Helper()
	state := json.RawMessage(`{"schema":"capital-state-test-v1"}`)
	stateSHA := hash(state)
	plan, err := experimentrun.NewPlan(experimentrun.PlanInput{ExperimentID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("experiment-"+salt)), ProgramID: uuid.NewSHA1(uuid.NameSpaceOID, append(versionID[:], []byte(salt)...)), AccountID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("account-"+salt)), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("checkpoint-"+salt)), CapitalStateBytes: state, ManifestID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("manifest-"+salt)), ManifestSHA256: strings.Repeat("c", 64), EvaluationStart: start, EvaluationEnd: start.Add(72 * time.Hour), Seed: 602, Mode: strategycatalog.ExperimentPaperScored, Steps: []experimentrun.StepInput{{PartitionContentSHA256: strings.Repeat("d", 64), ObservationSourceKey: "quote_" + salt, ObservationContentSHA256: strings.Repeat("e", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := experimentrun.NewResult(experimentrun.ResultInput{Plan: plan, AccountID: plan.AccountID(), QualityResultID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("quality-"+salt)), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("f", 64), CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("1", 64), Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}}})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{Version: "evaluation-policy-v1@research", Frequency: "daily", PeriodsPerYear: 252, ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12})
	if err != nil {
		t.Fatal(err)
	}
	observations := make([]evaluation.ObservationInput, len(equities))
	for index, equity := range equities {
		observations[index] = evaluation.ObservationInput{ObservedAt: start.Add(time.Duration(index) * 24 * time.Hour), Equity: equity, BenchmarkValue: equities[0], CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(salt+string(rune(index)))), EvidenceSHA256: strings.Repeat("2", 64)}
	}
	report, err := evaluation.NewReport(evaluation.ReportInput{Result: result, Policy: policy, EvaluationStart: plan.EvaluationStart(), EvaluationEnd: plan.EvaluationEnd(), Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}, Observations: observations})
	if err != nil {
		t.Fatal(err)
	}
	return report
}
