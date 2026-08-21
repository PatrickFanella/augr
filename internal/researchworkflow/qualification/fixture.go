package qualification

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/evaluation"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/generativestrategy"
	generatedqualification "github.com/PatrickFanella/get-rich-quick/internal/generativestrategy/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/researchworkflow"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

type Fixture struct {
	Parents        researchworkflow.Parents
	StrategyFamily *strategycatalog.Family
	Hypothesis     *researchworkflow.Hypothesis
	ReadyCritic    *researchworkflow.Critic
	RejectCritic   *researchworkflow.Critic
	ConflictCritic *researchworkflow.Critic
}

func Build() (Fixture, error) {
	generated, err := generatedqualification.Build()
	if err != nil {
		return Fixture{}, err
	}
	spec, err := generativestrategy.NewSpec(generated.Input)
	if err != nil {
		return Fixture{}, err
	}
	version, receipt, err := generativestrategy.Compile(spec, strings.Repeat("1", 40), strings.Repeat("2", 64))
	if err != nil {
		return Fixture{}, err
	}
	policy, family, assessment, err := robustnessParents(version.ID())
	if err != nil {
		return Fixture{}, err
	}
	cutoff := time.Date(2026, 8, 20, 12, 0, 0, 0, time.UTC)
	availableA := cutoff.Add(-4 * time.Hour)
	availableB := cutoff.Add(-3 * time.Hour)
	manifest, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: cutoff, Partitions: []dataset.PartitionInput{{Kind: dataset.KindExternalObject, Provider: "licensed_provider", Source: "research_archive", Namespace: "research", RequestSHA256: strings.Repeat("3", 64), MediaType: "application/pdf", SymbologyVersion: "none", AdjustmentPolicy: "immutable", Timezone: "UTC", Calendar: "continuous", Revision: "v1", License: "licensed", RetentionPolicy: "permanent", Observations: []dataset.ObservationInput{{SourceKey: "source_a", InstrumentID: uuid.MustParse("10000000-0000-4000-8000-000000000001"), EffectiveAt: cutoff.Add(-48 * time.Hour), ObservedAt: availableA, AvailableAt: availableA, Revision: "v1", ContentSHA256: strings.Repeat("4", 64)}, {SourceKey: "source_b", InstrumentID: uuid.MustParse("20000000-0000-4000-8000-000000000002"), EffectiveAt: cutoff.Add(-36 * time.Hour), ObservedAt: availableB, AvailableAt: availableB, Revision: "v1", ContentSHA256: strings.Repeat("5", 64)}}}}})
	if err != nil {
		return Fixture{}, err
	}
	parents := researchworkflow.Parents{Manifest: manifest, RobustnessPolicy: policy, RobustnessFamily: family, Assessment: assessment, Spec: spec, Version: version, Receipt: receipt}
	tests := []researchworkflow.TestInput{}
	var generatedTests struct {
		PropertyTests []string `json:"property_tests"`
		ExampleTests  []struct {
			Key string `json:"key"`
		} `json:"example_tests"`
	}
	if err = json.Unmarshal(spec.CanonicalBytes(), &generatedTests); err != nil {
		return Fixture{}, err
	}
	for _, key := range generatedTests.PropertyTests {
		tests = append(tests, researchworkflow.TestInput{Key: "property_" + key, Type: "spec_property", ExpectedOutcome: "The invariant holds.", AcceptanceRule: "The generated property test passes.", SpecTestKey: key})
	}
	for _, value := range generatedTests.ExampleTests {
		tests = append(tests, researchworkflow.TestInput{Key: "example_" + value.Key, Type: "spec_example", ExpectedOutcome: "The example matches its declared decision.", AcceptanceRule: "The generated example test passes.", SpecTestKey: value.Key})
	}
	for _, kind := range []string{"leakage", "cost", "baseline", "refutation"} {
		tests = append(tests, researchworkflow.TestInput{Key: kind + "_gate", Type: kind, ExpectedOutcome: "The declared evidence satisfies the gate.", AcceptanceRule: "The named gate returns pass."})
	}
	hypothesis, err := researchworkflow.NewHypothesis(researchworkflow.HypothesisInput{Parents: parents, WorkflowKey: "research_momentum_v1", Claim: "Momentum predicts positive net returns after declared costs.", Mechanism: "Delayed information diffusion creates persistent cross-sectional price movement.", PredictedObservation: "The candidate exceeds its baseline on held-out point-in-time observations.", NullHypothesis: "Net performance is no better than the declared baseline.", RefutationThreshold: "Reject when the held-out net mean is nonpositive.", EvaluationHorizon: "Two embargoed walk-forward folds of thirty calendar days.", AbstentionCondition: "Abstain when any required source is stale or unavailable.", Sources: []researchworkflow.SourceInput{{Key: "paper_a", URI: "https://example.com/a", Publisher: "Publisher A", Title: "Paper A", PublishedAt: cutoff.Add(-72 * time.Hour), AvailableAt: availableA, ContentSHA256: strings.Repeat("4", 64), License: "licensed", ManifestSourceKeys: []string{"source_a"}}, {Key: "paper_b", URI: "https://example.com/b", Publisher: "Publisher B", Title: "Paper B", PublishedAt: cutoff.Add(-60 * time.Hour), AvailableAt: availableB, ContentSHA256: strings.Repeat("5", 64), License: "licensed", ManifestSourceKeys: []string{"source_b"}}}, Searches: []researchworkflow.SearchInput{{Key: "search_a", Provider: "catalog", QuerySHA256: strings.Repeat("6", 64), ExecutedAt: cutoff.Add(-2 * time.Hour), Results: []researchworkflow.SearchResultInput{{SourceKey: "paper_a", Rank: 1, Selected: true}, {SourceKey: "discarded_a", Rank: 2, Selected: false}}}, {Key: "search_b", Provider: "catalog", QuerySHA256: strings.Repeat("7", 64), ExecutedAt: cutoff.Add(-time.Hour), Results: []researchworkflow.SearchResultInput{{SourceKey: "paper_b", Rank: 1, Selected: true}, {SourceKey: "discarded_b", Rank: 2, Selected: false}}}}, Provenance: researchworkflow.ProvenanceInput{Provider: "openai", Model: "gpt-5.6", SystemPromptSHA256: strings.Repeat("8", 64), DeveloperPromptSHA256: strings.Repeat("9", 64), UserPromptSHA256: strings.Repeat("a", 64), InputTokens: 2000, OutputTokens: 800, Currency: "USD", Cost: "0.4"}, Tests: tests})
	if err != nil {
		return Fixture{}, err
	}
	ready, err := critic(hypothesis, "independent_review_v1", false, assessment.Digest(), version.Digest())
	if err != nil {
		return Fixture{}, err
	}
	reject, err := critic(hypothesis, "independent_rejection_v1", true, assessment.Digest(), version.Digest())
	if err != nil {
		return Fixture{}, err
	}
	conflict, err := critic(hypothesis, "independent_review_v1", true, assessment.Digest(), version.Digest())
	if err != nil {
		return Fixture{}, err
	}
	return Fixture{parents, generated.Family, hypothesis, ready, reject, conflict}, nil
}

func critic(h *researchworkflow.Hypothesis, key string, reject bool, assessmentDigest, versionDigest string) (*researchworkflow.Critic, error) {
	references := map[string]string{"cost_capacity": "test:cost_gate", "leakage": "test:leakage_gate", "multiple_testing": "assessment:sha256:" + assessmentDigest, "reproducibility": "version:sha256:" + versionDigest, "source_coverage": "source:paper_a", "test_completeness": "test:refutation_gate"}
	checks := []researchworkflow.CheckInput{}
	for _, name := range []string{"cost_capacity", "leakage", "multiple_testing", "reproducibility", "source_coverage", "test_completeness"} {
		checks = append(checks, researchworkflow.CheckInput{Name: name, State: "pass", References: []string{references[name]}, Explanation: "Independent review found complete retained evidence for this check."})
	}
	findings := []researchworkflow.FindingInput{{Key: "license_note", Category: "source_coverage", Severity: "low", Status: "resolved", References: []string{"source:paper_a"}, Explanation: "The retained license was independently confirmed."}}
	if reject {
		findings = append(findings, researchworkflow.FindingInput{Key: "open_leakage", Category: "leakage", Severity: "critical", Status: "open", References: []string{"test:leakage_gate"}, Explanation: "Independent review found unresolved lookahead risk."})
	}
	return researchworkflow.NewCritic(researchworkflow.CriticInput{Hypothesis: h, ReviewKey: key, Provenance: researchworkflow.ProvenanceInput{Provider: "anthropic", Model: "critic-model", SystemPromptSHA256: strings.Repeat("b", 64), DeveloperPromptSHA256: strings.Repeat("c", 64), UserPromptSHA256: strings.Repeat("d", 64), InputTokens: 1500, OutputTokens: 600, Currency: "USD", Cost: "0.3"}, Findings: findings, Checks: checks})
}

func robustnessParents(versionID uuid.UUID) (*robustness.Policy, *robustness.Family, *robustness.Assessment, error) {
	policy, err := robustness.NewPolicy(robustness.PolicyInput{Version: "robustness-policy-v1@research", FoldCount: 2, PurgeSeconds: 86400, EmbargoSeconds: 86400, BootstrapAlgorithm: "xorshift64star-iid-percentile-v1", BootstrapSeed: 602, BootstrapIterations: 1000, ConfidenceLevel: "0.95", FamilyWiseAlpha: "0.05", MaxLargestPositiveShare: "0.4", MaxTopDecilePositiveShare: "0.4", MaxPerturbationDegradation: "0.005", RequiredPerturbations: []string{"cost_up"}, DecimalScale: 12})
	if err != nil {
		return nil, nil, nil, err
	}
	family, err := robustness.NewFamily(robustness.FamilyInput{Name: "OVR-602 research family", HypothesisSHA256: strings.Repeat("b", 64), CandidateVersionIDs: []uuid.UUID{versionID}})
	if err != nil {
		return nil, nil, nil, err
	}
	folds := make([]robustness.FoldInput, 2)
	for index := range folds {
		start := time.Date(2026, 7, 1+index*10, 12, 0, 0, 0, time.UTC)
		baseline, e := report(versionID, start, []string{"100", "101", "102", "103"}, fmt.Sprintf("base%d", index))
		if e != nil {
			return nil, nil, nil, e
		}
		perturbed, e := report(versionID, start, []string{"100", "100.8", "101.6", "102.4"}, fmt.Sprintf("cost%d", index))
		if e != nil {
			return nil, nil, nil, e
		}
		folds[index] = robustness.FoldInput{TrainStart: start.Add(-6 * 24 * time.Hour), TrainEnd: start.Add(-2 * 24 * time.Hour), Baseline: baseline, Perturbations: []robustness.ScenarioInput{{Kind: "cost_up", Severity: "double_declared_cost", Report: perturbed}}}
	}
	assessment, err := robustness.NewAssessment(robustness.AssessmentInput{Family: family, Policy: policy, Mode: strategycatalog.ExperimentPaperScored, Candidates: []robustness.CandidateInput{{VersionID: versionID, Folds: folds}}})
	return policy, family, assessment, err
}

func report(versionID uuid.UUID, start time.Time, equities []string, salt string) (*evaluation.Report, error) {
	state := json.RawMessage(`{"schema":"capital-state-test-v1"}`)
	stateSHA := sha(state)
	plan, err := experimentrun.NewPlan(experimentrun.PlanInput{ExperimentID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("experiment-"+salt)), ProgramID: uuid.NewSHA1(uuid.NameSpaceOID, append(versionID[:], []byte(salt)...)), AccountID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("account-"+salt)), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("checkpoint-"+salt)), CapitalStateBytes: state, ManifestID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("manifest-"+salt)), ManifestSHA256: strings.Repeat("c", 64), EvaluationStart: start, EvaluationEnd: start.Add(72 * time.Hour), Seed: 602, Mode: strategycatalog.ExperimentPaperScored, Steps: []experimentrun.StepInput{{PartitionContentSHA256: strings.Repeat("d", 64), ObservationSourceKey: "quote_" + salt, ObservationContentSHA256: strings.Repeat("e", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop}}})
	if err != nil {
		return nil, err
	}
	result, err := experimentrun.NewResult(experimentrun.ResultInput{Plan: plan, AccountID: plan.AccountID(), QualityResultID: uuid.NewSHA1(uuid.NameSpaceOID, []byte("quality-"+salt)), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("f", 64), CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("1", 64), Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}}})
	if err != nil {
		return nil, err
	}
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{Version: "evaluation-policy-v1@research", Frequency: "daily", PeriodsPerYear: 252, ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12})
	if err != nil {
		return nil, err
	}
	observations := make([]evaluation.ObservationInput, len(equities))
	for index, equity := range equities {
		observations[index] = evaluation.ObservationInput{ObservedAt: start.Add(time.Duration(index) * 24 * time.Hour), Equity: equity, BenchmarkValue: equities[0], CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("%s%d", salt, index))), EvidenceSHA256: strings.Repeat("2", 64)}
	}
	return evaluation.NewReport(evaluation.ReportInput{Result: result, Policy: policy, EvaluationStart: plan.EvaluationStart(), EvaluationEnd: plan.EvaluationEnd(), Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"}, Observations: observations})
}

func sha(value []byte) string { sum := sha256.Sum256(value); return fmt.Sprintf("%x", sum) }
