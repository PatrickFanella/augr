package robustness

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestAssessmentDeterministicRestoreAndExplicitGates(t *testing.T) {
	input, reports := validAssessmentInput(t)
	first, err := NewAssessment(input)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := NewAssessment(input)
	if err != nil || retry.ID() != first.ID() || retry.Digest() != first.Digest() {
		t.Fatalf("retry=%v err=%v", retry, err)
	}
	if first.ID() != uuid.MustParse("87d8f6b2-5a16-2d47-3b5c-a56cccc84eee") || first.Digest() != "8fc0a0586b7a9b40b17993a8852ae4f6e17ed90b92702c810673af44565283d0" {
		t.Fatalf("golden identity changed: %s/%s", first.ID(), first.Digest())
	}
	for _, candidate := range first.Candidates() {
		if gate(candidate, "overall_robustness").State != GatePass || gate(candidate, "multiple_testing_adjustment").State != GatePass {
			t.Fatalf("candidate gates=%+v", candidate.Gates)
		}
		if statistic(candidate, "holm_adjusted_nonpositive_mean_probability").Value == "" {
			t.Fatal("adjusted probability missing")
		}
	}
	restored, err := AssessmentFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes(), input.Family, input.Policy, reports)
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), first.CanonicalBytes()) {
		t.Fatalf("restored=%v err=%v", restored, err)
	}
	tampered := bytes.Replace(first.CanonicalBytes(), []byte(`"overall_robustness","state":"pass"`), []byte(`"overall_robustness","state":"fail"`), 1)
	if _, err := AssessmentFromCanonical(economicid.DeterministicUUID("statistical-robustness-assessment", AssessmentSchemaV1+"@sha256:"+hash(tampered)), hash(tampered), tampered, input.Family, input.Policy, reports); err == nil {
		t.Fatal("tampered gate restored")
	}
}

func TestPolicyFamilyRestoreOrderAndCloneSafety(t *testing.T) {
	input, _ := validAssessmentInput(t)
	policy, err := PolicyFromCanonical(input.Policy.ID(), input.Policy.Digest(), input.Policy.CanonicalBytes())
	if err != nil || policy.ID() != input.Policy.ID() {
		t.Fatalf("policy restore=%v err=%v", policy, err)
	}
	family, err := FamilyFromCanonical(input.Family.ID(), input.Family.Digest(), input.Family.CanonicalBytes())
	if err != nil || family.ID() != input.Family.ID() {
		t.Fatalf("family restore=%v err=%v", family, err)
	}
	reorderedFamily, err := NewFamily(FamilyInput{
		Name: input.Family.Name(), HypothesisSHA256: input.Family.HypothesisSHA256(),
		CandidateVersionIDs: []uuid.UUID{input.Family.CandidateVersionIDs()[1], input.Family.CandidateVersionIDs()[0]},
	})
	if err != nil || reorderedFamily.ID() != input.Family.ID() {
		t.Fatalf("family reorder=%v err=%v", reorderedFamily, err)
	}
	reordered := input
	reordered.Candidates = []CandidateInput{input.Candidates[1], input.Candidates[0]}
	first, _ := NewAssessment(input)
	second, err := NewAssessment(reordered)
	if err != nil || first.ID() != second.ID() {
		t.Fatalf("candidate reorder diverged err=%v", err)
	}
	view := first.Candidates()
	view[0].Folds[0].Perturbations[0].Kind = "changed"
	view[0].Statistics[0].Value = "changed"
	view[0].Gates[0].State = GateFail
	if !bytes.Equal(first.CanonicalBytes(), second.CanonicalBytes()) {
		t.Fatal("assessment accessor leaked mutable state")
	}
}

func TestAssessmentRejectsLeakageMissingPerturbationAndModeCross(t *testing.T) {
	for name, mutate := range map[string]func(*AssessmentInput){
		"purge": func(input *AssessmentInput) {
			input.Candidates[0].Folds[0].TrainEnd = input.Candidates[0].Folds[0].Baseline.EvaluationStart()
		},
		"embargo": func(input *AssessmentInput) {
			input.Candidates[0].Folds[1].TrainStart = input.Candidates[0].Folds[0].Baseline.EvaluationEnd()
		},
		"missing perturbation": func(input *AssessmentInput) { input.Candidates[0].Folds[0].Perturbations = nil },
		"mode cross":           func(input *AssessmentInput) { input.Mode = strategycatalog.ExperimentPaperStress },
		"nil baseline":         func(input *AssessmentInput) { input.Candidates[0].Folds[0].Baseline = nil },
	} {
		t.Run(name, func(t *testing.T) {
			input, _ := validAssessmentInput(t)
			mutate(&input)
			if _, err := NewAssessment(input); err == nil {
				t.Fatal("invalid assessment succeeded")
			}
		})
	}
}

func TestAssessmentFailsConcentratedNegativeAndDegradedEvidence(t *testing.T) {
	input, _ := validAssessmentInput(t)
	for foldIndex := range input.Candidates[0].Folds {
		start := input.Candidates[0].Folds[foldIndex].Baseline.EvaluationStart()
		input.Candidates[0].Folds[foldIndex].Baseline = evaluationReport(t, input.Candidates[0].VersionID, start, []string{"100", "90", "80", "70"}, strategycatalog.ExperimentPaperScored, "negative"+string(rune('a'+foldIndex)))
		for index := range input.Candidates[0].Folds[foldIndex].Perturbations {
			input.Candidates[0].Folds[foldIndex].Perturbations[index].Report = evaluationReport(t, input.Candidates[0].VersionID, start, []string{"100", "89", "78", "67"}, strategycatalog.ExperimentPaperScored, "degraded"+string(rune('a'+foldIndex)))
		}
	}
	assessment, err := NewAssessment(input)
	if err != nil {
		t.Fatal(err)
	}
	candidate := assessment.Candidates()[0]
	if gate(candidate, "bootstrap_positive_mean").State != GateFail || gate(candidate, "return_concentration").State != GateFail || gate(candidate, "perturbation_stability").State != GateFail || gate(candidate, "overall_robustness").State != GateFail {
		t.Fatalf("negative/degraded gates=%+v", candidate.Gates)
	}
}

func TestHolmFamilyExpansionInvalidatesUnadjustedWinners(t *testing.T) {
	policy, _ := robustnessPolicy()
	candidates := []CandidateEvidence{{VersionID: "10000000-0000-4000-8000-000000000001"}, {VersionID: "10000000-0000-4000-8000-000000000002"}}
	for index := range candidates {
		candidates[index].Gates = []Gate{{Name: "bootstrap_positive_mean", State: GatePass}, {Name: "return_concentration", State: GatePass}, {Name: "perturbation_stability", State: GatePass}}
	}
	calculations := []candidateCalculation{{rawProbability: decimal.RequireFromString("0.03")}, {rawProbability: decimal.RequireFromString("0.04")}}
	applyHolm(policy, candidates, calculations)
	for _, candidate := range candidates {
		if gate(candidate, "multiple_testing_adjustment").State != GateFail || gate(candidate, "overall_robustness").State != GateFail {
			t.Fatalf("unadjusted candidate passed expanded family: %+v", candidate.Gates)
		}
	}
}

func TestBootstrapAndConcentrationBoundaries(t *testing.T) {
	policy, _ := robustnessPolicy()
	lower, upper, raw := bootstrap(policy, uuid.MustParse("30500000-0000-4000-8000-000000000099"), []float64{0.01, 0.01, 0.01})
	if lower != 0.01 || upper != 0.01 || raw != float64(1)/float64(policy.BootstrapIterations()+1) {
		t.Fatalf("constant bootstrap=%v/%v/%v", lower, upper, raw)
	}
	if largest, top, available := concentration([]float64{0.1, 0, -0.01, -0.02}); !available || largest != 1 || top != 1 {
		t.Fatalf("clustered concentration=%v/%v available=%v", largest, top, available)
	}
	if _, _, available := concentration([]float64{0, -0.01}); available {
		t.Fatal("nonpositive sample reported available concentration")
	}
}

func validAssessmentInput(t *testing.T) (AssessmentInput, map[uuid.UUID]*evaluation.Report) {
	t.Helper()
	versionIDs := []uuid.UUID{uuid.MustParse("30500000-0000-4000-8000-000000000001"), uuid.MustParse("30500000-0000-4000-8000-000000000002")}
	policy, err := robustnessPolicy()
	if err != nil {
		t.Fatal(err)
	}
	family, err := NewFamily(FamilyInput{Name: "OVR-305 deterministic family", HypothesisSHA256: strings.Repeat("a", 64), CandidateVersionIDs: versionIDs})
	if err != nil {
		t.Fatal(err)
	}
	reports := map[uuid.UUID]*evaluation.Report{}
	candidates := make([]CandidateInput, len(versionIDs))
	for candidateIndex, versionID := range versionIDs {
		folds := make([]FoldInput, 2)
		for foldIndex := range folds {
			start := time.Date(2026, 7, 10+foldIndex*10, 15, 0, 0, 123456000, time.UTC)
			baseline := evaluationReport(t, versionID, start, []string{"100", "101", "102", "103"}, strategycatalog.ExperimentPaperScored, "baseline-"+string(rune('a'+candidateIndex))+string(rune('a'+foldIndex)))
			perturbed := evaluationReport(t, versionID, start, []string{"100", "100.8", "101.6", "102.4"}, strategycatalog.ExperimentPaperScored, "cost-"+string(rune('a'+candidateIndex))+string(rune('a'+foldIndex)))
			reports[baseline.ID()], reports[perturbed.ID()] = baseline, perturbed
			folds[foldIndex] = FoldInput{
				TrainStart: start.Add(-6 * 24 * time.Hour), TrainEnd: start.Add(-2 * 24 * time.Hour), Baseline: baseline,
				Perturbations: []ScenarioInput{{Kind: "cost_up", Severity: "double_declared_cost", Report: perturbed}},
			}
		}
		candidates[candidateIndex] = CandidateInput{VersionID: versionID, Folds: folds}
	}
	return AssessmentInput{Family: family, Policy: policy, Mode: strategycatalog.ExperimentPaperScored, Candidates: candidates}, reports
}

func robustnessPolicy() (*Policy, error) {
	return NewPolicy(PolicyInput{
		Version: "robustness-policy-v1@reviewed", FoldCount: 2, PurgeSeconds: 86400, EmbargoSeconds: 86400,
		BootstrapAlgorithm: "xorshift64star-iid-percentile-v1", BootstrapSeed: 305, BootstrapIterations: 1000,
		ConfidenceLevel: "0.95", FamilyWiseAlpha: "0.05", MaxLargestPositiveShare: "0.4", MaxTopDecilePositiveShare: "0.4",
		MaxPerturbationDegradation: "0.005", RequiredPerturbations: []string{"cost_up"}, DecimalScale: 12,
	})
}

func evaluationReport(t *testing.T, versionID uuid.UUID, start time.Time, equities []string, mode strategycatalog.ExperimentMode, salt string) *evaluation.Report {
	t.Helper()
	if len(equities) != 4 {
		t.Fatal("evaluation fixture needs four observations")
	}
	plan, result := evaluationParent(t, versionID, start, mode, salt)
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{
		Version: "evaluation-policy-v1@robustness", Frequency: "daily", PeriodsPerYear: 252,
		ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	observations := make([]evaluation.ObservationInput, len(equities))
	for index, equity := range equities {
		observations[index] = evaluation.ObservationInput{
			ObservedAt: start.Add(time.Duration(index) * 24 * time.Hour), Equity: equity, BenchmarkValue: equities[0],
			CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0",
			CumulativeModeledSlippage: "0", EvidenceID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(salt+string(rune(index)))), EvidenceSHA256: strings.Repeat("b", 64),
		}
	}
	report, err := evaluation.NewReport(evaluation.ReportInput{
		Result: result, Policy: policy, EvaluationStart: plan.EvaluationStart(), EvaluationEnd: plan.EvaluationEnd(),
		Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}, Observations: observations,
	})
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func evaluationParent(t *testing.T, versionID uuid.UUID, start time.Time, mode strategycatalog.ExperimentMode, salt string) (*experimentrun.Plan, *experimentrun.Result) {
	t.Helper()
	state := json.RawMessage(`{"schema":"capital-state-test-v1"}`)
	stateSHA := hash(state)
	plan, err := experimentrun.NewPlan(experimentrun.PlanInput{
		ExperimentID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("experiment-"+salt)),
		ProgramID:    uuid.NewSHA1(uuid.NameSpaceOID, append(versionID[:], []byte(salt)...)), AccountID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("account-"+string(mode))),
		CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA,
		CapitalProjectionCheckpointID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("checkpoint-"+salt)), CapitalStateBytes: state,
		ManifestID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("manifest-"+salt)), ManifestSHA256: strings.Repeat("c", 64), EvaluationStart: start,
		EvaluationEnd: start.Add(72 * time.Hour), Seed: 305, Mode: mode, Steps: []experimentrun.StepInput{{
			PartitionContentSHA256: strings.Repeat("d", 64),
			ObservationSourceKey:   "quote-" + salt, ObservationContentSHA256: strings.Repeat("e", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := experimentrun.NewResult(experimentrun.ResultInput{
		Plan: plan, AccountID: plan.AccountID(),
		QualityResultID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("quality-"+salt)), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("f", 64),
		CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("1", 64), Outcomes: []experimentrun.StepOutcomeInput{{
			Action:         experimentrun.ActionNoop,
			DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0",
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return plan, result
}

func gate(candidate CandidateEvidence, name string) Gate {
	for _, value := range candidate.Gates {
		if value.Name == name {
			return value
		}
	}
	return Gate{}
}

func statistic(candidate CandidateEvidence, name string) Statistic {
	for _, value := range candidate.Statistics {
		if value.Name == name {
			return value
		}
	}
	return Statistic{}
}
