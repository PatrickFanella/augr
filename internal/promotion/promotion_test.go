package promotion

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestDecisionApprovesHoldsAndRetiresDeterministically(t *testing.T) {
	deployment, assessment := promotionParents(t, true)
	policy, err := NewPolicy(PolicyInput{Version: "promotion-policy-v1@reviewed", RequiredGates: []string{"overall_robustness", "multiple_testing_adjustment"}, FailureAction: ActionHold})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := NewDecision(DecisionInput{Deployment: deployment, Assessment: assessment, Policy: policy})
	if err != nil || approved.Outcome() != OutcomeApproved || approved.PriorState() != strategycatalog.DeploymentProposed || approved.NextState() != StateShadow {
		t.Fatalf("approved=%v err=%v", approved, err)
	}
	retry, _ := NewDecision(DecisionInput{Deployment: deployment, Assessment: assessment, Policy: policy})
	if retry.ID() != approved.ID() || retry.Digest() != approved.Digest() {
		t.Fatal("identical decision diverged")
	}
	held, err := NewDecision(DecisionInput{Deployment: deployment, Assessment: assessment, Policy: policy, PriorDecision: approved})
	if err != nil || held.Outcome() != OutcomeHeld || held.PriorState() != StateShadow || held.NextState() != StateShadow {
		t.Fatalf("held=%v err=%v", held, err)
	}

	failedDeployment, failedAssessment := promotionParents(t, false)
	retirePolicy, _ := NewPolicy(PolicyInput{Version: "promotion-policy-v1@retire-on-failure", RequiredGates: []string{"overall_robustness"}, FailureAction: ActionRetire})
	retired, err := NewDecision(DecisionInput{Deployment: failedDeployment, Assessment: failedAssessment, Policy: retirePolicy})
	if err != nil || retired.Outcome() != OutcomeRetired || retired.NextState() != StateRetired {
		t.Fatalf("retired=%v err=%v", retired, err)
	}
}

func TestDecisionRestoreCloneAndTamperSafety(t *testing.T) {
	deployment, assessment := promotionParents(t, true)
	policy, _ := NewPolicy(PolicyInput{Version: "promotion-policy-v1@restore", RequiredGates: []string{"overall_robustness"}, FailureAction: ActionHold})
	decision, _ := NewDecision(DecisionInput{Deployment: deployment, Assessment: assessment, Policy: policy})
	restored, err := DecisionFromCanonical(decision.ID(), decision.Digest(), decision.CanonicalBytes(), deployment, assessment, policy, nil)
	if err != nil || !bytes.Equal(restored.CanonicalBytes(), decision.CanonicalBytes()) {
		t.Fatalf("restored=%v err=%v", restored, err)
	}
	view := decision.CanonicalBytes()
	view[0] = 'x'
	if bytes.Equal(view, decision.CanonicalBytes()) {
		t.Fatal("canonical accessor leaked mutable bytes")
	}
	tampered := bytes.Replace(decision.CanonicalBytes(), []byte(`"outcome":"approved"`), []byte(`"outcome":"held"`), 1)
	if _, err := DecisionFromCanonical(economicid.DeterministicUUID("promotion-retirement-decision", DecisionSchemaV1+"@sha256:"+hash(tampered)), hash(tampered), tampered, deployment, assessment, policy, nil); err == nil {
		t.Fatal("tampered outcome restored")
	}
}

func TestDecisionRejectsMissingGateVersionModeAndPriorCross(t *testing.T) {
	deployment, assessment := promotionParents(t, true)
	for name, mutate := range map[string]func(*strategycatalog.Deployment, *robustness.Assessment, *Policy) DecisionInput{
		"missing gate": func(d *strategycatalog.Deployment, a *robustness.Assessment, _ *Policy) DecisionInput {
			p, _ := NewPolicy(PolicyInput{Version: "missing", RequiredGates: []string{"overall_robustness", "not_present"}, FailureAction: ActionHold})
			return DecisionInput{Deployment: d, Assessment: a, Policy: p}
		},
		"version": func(_ *strategycatalog.Deployment, a *robustness.Assessment, p *Policy) DecisionInput {
			input := validDeploymentInput(uuid.New(), strategycatalog.ExperimentPaperScored)
			d, _ := strategycatalog.NewDeployment(input)
			return DecisionInput{Deployment: d, Assessment: a, Policy: p}
		},
		"mode": func(_ *strategycatalog.Deployment, a *robustness.Assessment, p *Policy) DecisionInput {
			input := validDeploymentInput(uuid.MustParse(a.Candidates()[0].VersionID), strategycatalog.ExperimentPaperStress)
			d, _ := strategycatalog.NewDeployment(input)
			return DecisionInput{Deployment: d, Assessment: a, Policy: p}
		},
	} {
		t.Run(name, func(t *testing.T) {
			policy, _ := NewPolicy(PolicyInput{Version: "reviewed", RequiredGates: []string{"overall_robustness"}, FailureAction: ActionHold})
			if _, err := NewDecision(mutate(deployment, assessment, policy)); err == nil {
				t.Fatal("invalid decision succeeded")
			}
		})
	}
}

func promotionParents(t *testing.T, pass bool) (*strategycatalog.Deployment, *robustness.Assessment) {
	t.Helper()
	versionID := uuid.MustParse("30600000-0000-4000-8000-000000000001")
	family, _ := robustness.NewFamily(robustness.FamilyInput{Name: "promotion family", HypothesisSHA256: strings.Repeat("a", 64), CandidateVersionIDs: []uuid.UUID{versionID}})
	policy, _ := robustness.NewPolicy(robustness.PolicyInput{Version: "robustness@promotion", FoldCount: 2, PurgeSeconds: 86400, EmbargoSeconds: 86400, BootstrapAlgorithm: "xorshift64star-iid-percentile-v1", BootstrapSeed: 306, BootstrapIterations: 1000, ConfidenceLevel: "0.95", FamilyWiseAlpha: "0.05", MaxLargestPositiveShare: "0.6", MaxTopDecilePositiveShare: "0.6", MaxPerturbationDegradation: "0.005", RequiredPerturbations: []string{"cost_up"}, DecimalScale: 12})
	folds := make([]robustness.FoldInput, 2)
	for index := range folds {
		start := time.Date(2026, 7, 1+index*10, 0, 0, 0, 123456000, time.UTC)
		equities := []string{"100", "101", "102", "103"}
		perturbed := []string{"100", "100.8", "101.6", "102.4"}
		if !pass {
			equities, perturbed = []string{"100", "90", "80", "70"}, []string{"100", "89", "78", "67"}
		}
		baseline := promotionReport(t, versionID, start, equities, "baseline"+string(rune(index)))
		cost := promotionReport(t, versionID, start, perturbed, "cost"+string(rune(index)))
		folds[index] = robustness.FoldInput{TrainStart: start.Add(-6 * 24 * time.Hour), TrainEnd: start.Add(-2 * 24 * time.Hour), Baseline: baseline, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_cost", Report: cost}}}
	}
	assessment, err := robustness.NewAssessment(robustness.AssessmentInput{Family: family, Policy: policy, Mode: strategycatalog.ExperimentPaperScored, Candidates: []robustness.CandidateInput{{VersionID: versionID, Folds: folds}}})
	if err != nil {
		t.Fatal(err)
	}
	deployment, err := strategycatalog.NewDeployment(validDeploymentInput(versionID, strategycatalog.ExperimentPaperScored))
	if err != nil {
		t.Fatal(err)
	}
	return deployment, assessment
}

func validDeploymentInput(versionID uuid.UUID, mode strategycatalog.ExperimentMode) strategycatalog.DeploymentInput {
	return strategycatalog.DeploymentInput{VersionID: versionID, AccountID: uuid.MustParse("30600000-0000-4000-8000-000000000002"), CapitalBindingID: uuid.MustParse("30600000-0000-4000-8000-000000000003"), Budget: "10000", ScheduleCron: "0 14 * * 1-5", Timezone: "America/Chicago", RiskPolicyVersion: "risk-v1", Mode: mode}
}

func promotionReport(t *testing.T, versionID uuid.UUID, start time.Time, equities []string, salt string) *evaluation.Report {
	t.Helper()
	state := json.RawMessage(`{"schema":"capital-state-test-v1"}`)
	stateSHA := hash(state)
	plan, err := experimentrun.NewPlan(experimentrun.PlanInput{ExperimentID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("experiment"+salt)), ProgramID: uuid.NewSHA1(uuid.NameSpaceOID, append(versionID[:], []byte(salt)...)), AccountID: uuid.MustParse("30600000-0000-4000-8000-000000000002"), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("checkpoint"+salt)), CapitalStateBytes: state, ManifestID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("manifest"+salt)), ManifestSHA256: strings.Repeat("a", 64), EvaluationStart: start, EvaluationEnd: start.Add(3 * 24 * time.Hour), Seed: 306, Mode: strategycatalog.ExperimentPaperScored, Steps: []experimentrun.StepInput{{PartitionContentSHA256: strings.Repeat("b", 64), ObservationSourceKey: "source", ObservationContentSHA256: strings.Repeat("c", 64), AvailableAt: start, Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop}}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := experimentrun.NewResult(experimentrun.ResultInput{Plan: plan, AccountID: plan.AccountID(), QualityResultID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("quality"+salt)), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("e", 64), CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("f", 64), Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}}})
	if err != nil {
		t.Fatal(err)
	}
	policy, _ := evaluation.NewPolicy(evaluation.PolicyInput{Version: "evaluation@promotion", Frequency: "daily", PeriodsPerYear: 252, ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12})
	observations := make([]evaluation.ObservationInput, len(equities))
	for index, equity := range equities {
		observations[index] = evaluation.ObservationInput{ObservedAt: start.Add(time.Duration(index) * 24 * time.Hour), Equity: equity, BenchmarkValue: "100", CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(salt+string(rune(index)))), EvidenceSHA256: strings.Repeat("d", 64)}
	}
	report, err := evaluation.NewReport(evaluation.ReportInput{Result: result, Policy: policy, EvaluationStart: start, EvaluationEnd: start.Add(3 * 24 * time.Hour), Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}, Observations: observations})
	if err != nil {
		t.Fatal(err)
	}
	return report
}
