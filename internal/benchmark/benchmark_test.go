package benchmark

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
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestDeclarationAndOpportunityCostAreDeterministic(t *testing.T) {
	t.Parallel()
	declaration, report := benchmarkFixture(t)
	if report.StrategyTotalReturn() != "0.050000000000" || report.BenchmarkTotalReturn() != "0.100000000000" ||
		report.CashTotalReturn() != "0.020100000000" || report.BenchmarkOpportunityCost() != "0.050000000000" ||
		report.CashOpportunityCost() != "-0.029900000000" || report.BenchmarkWealthDifference() != "50.000000000000" {
		t.Fatalf("returns = strategy %s benchmark %s cash %s costs %s/%s wealth %s", report.StrategyTotalReturn(), report.BenchmarkTotalReturn(),
			report.CashTotalReturn(), report.BenchmarkOpportunityCost(), report.CashOpportunityCost(), report.BenchmarkWealthDifference())
	}
	restoredDeclaration, err := DeclarationFromCanonical(declaration.ID(), declaration.Digest(), declaration.CanonicalBytes(), fixtureExperiment(t), fixtureManifest(t))
	if err != nil || restoredDeclaration.ID() != declaration.ID() {
		t.Fatalf("declaration restore = %+v/%v", restoredDeclaration, err)
	}
	_, evaluationReport := benchmarkParents(t)
	restoredReport, err := ReportFromCanonical(report.ID(), report.Digest(), report.CanonicalBytes(), declaration, evaluationReport)
	if err != nil || restoredReport.ID() != report.ID() {
		t.Fatalf("report restore = %+v/%v", restoredReport, err)
	}
	observations := declaration.Observations()
	observations[0].Value = "999"
	if declaration.Observations()[0].Value == "999" {
		t.Fatal("observation getter exposed declaration storage")
	}
}

func TestDeclarationRejectsPartialMisalignedAndTamperedEvidence(t *testing.T) {
	t.Parallel()
	experiment, manifest := fixtureExperiment(t), fixtureManifest(t)
	valid := fixtureDeclarationInput(experiment, manifest)
	tests := map[string]func(*DeclarationInput){
		"missing observation": func(input *DeclarationInput) { input.Observations = input.Observations[:2] },
		"wrong frequency":     func(input *DeclarationInput) { input.Frequency = "weekly" },
		"noncanonical value":  func(input *DeclarationInput) { input.Observations[1].Value = "105.0" },
		"cash loss below one": func(input *DeclarationInput) { input.Observations[1].CashReturn = "-1" },
		"bad evidence hash":   func(input *DeclarationInput) { input.Observations[1].EvidenceSHA256 = "bad" },
		"wrong manifest":      func(input *DeclarationInput) { input.Manifest = fixtureOtherManifest(t) },
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			input := valid
			input.Observations = append([]ObservationInput(nil), valid.Observations...)
			mutate(&input)
			if _, err := NewDeclaration(input); err == nil {
				t.Fatal("invalid declaration accepted")
			}
		})
	}
	declaration, report := benchmarkFixture(t)
	raw := declaration.CanonicalBytes()
	raw = bytes.Replace(raw, []byte(`"value":"105"`), []byte(`"value":"106"`), 1)
	if _, err := DeclarationFromCanonical(declaration.ID(), declaration.Digest(), raw, experiment, manifest); err == nil {
		t.Fatal("tampered declaration restored")
	}
	reportRaw := report.CanonicalBytes()
	reportRaw = bytes.Replace(reportRaw, []byte(`"strategy_total_return":"0.050000000000"`), []byte(`"strategy_total_return":"0.060000000000"`), 1)
	_, evaluationReport := benchmarkParents(t)
	if _, err := ReportFromCanonical(report.ID(), report.Digest(), reportRaw, declaration, evaluationReport); err == nil {
		t.Fatal("tampered report restored")
	}
}

func TestReportRejectsCurveOrParentSubstitution(t *testing.T) {
	t.Parallel()
	declaration, _ := benchmarkFixture(t)
	_, evaluationReport := benchmarkParents(t)
	input := evaluationReport.Observations()
	input[1].BenchmarkValue = "106"
	mutated := rebuildEvaluation(t, evaluationReport, input)
	if _, err := NewReport(declaration, mutated); err == nil || !strings.Contains(err.Error(), "observation 1") {
		t.Fatalf("curve substitution error = %v", err)
	}
	input = evaluationReport.Observations()
	input[1].EvidenceSHA256 = strings.Repeat("f", 64)
	mutated = rebuildEvaluation(t, evaluationReport, input)
	if _, err := NewReport(declaration, mutated); err == nil {
		t.Fatal("source substitution accepted")
	}
}

func benchmarkFixture(t *testing.T) (*Declaration, *Report) {
	t.Helper()
	experiment, evaluationReport := benchmarkParents(t)
	manifest := fixtureManifest(t)
	declaration, err := NewDeclaration(fixtureDeclarationInput(experiment, manifest))
	if err != nil {
		t.Fatal(err)
	}
	report, err := NewReport(declaration, evaluationReport)
	if err != nil {
		t.Fatal(err)
	}
	return declaration, report
}

func fixtureDeclarationInput(experiment *strategycatalog.Experiment, manifest *dataset.Manifest) DeclarationInput {
	start := experiment.EvaluationStart()
	return DeclarationInput{
		Experiment: experiment, Manifest: manifest, BenchmarkInstrumentID: fixtureInstrument(), BenchmarkKind: "total_return_index",
		Weighting: "single_asset", DistributionTreatment: "reinvested", CashConvention: "explicit_per_period", Frequency: "daily", InitialNotional: "1000", DecimalScale: 12,
		Observations: []ObservationInput{
			{ObservedAt: start, Value: "100", CashReturn: "0", EvidenceID: fixtureEvidence(1), EvidenceSHA256: strings.Repeat("1", 64)},
			{ObservedAt: start.Add(24 * time.Hour), Value: "105", CashReturn: "0.01", EvidenceID: fixtureEvidence(2), EvidenceSHA256: strings.Repeat("2", 64)},
			{ObservedAt: start.Add(48 * time.Hour), Value: "110", CashReturn: "0.01", EvidenceID: fixtureEvidence(3), EvidenceSHA256: strings.Repeat("3", 64)},
		},
	}
}

func benchmarkParents(t *testing.T) (*strategycatalog.Experiment, *evaluation.Report) {
	t.Helper()
	experiment := fixtureExperiment(t)
	start := experiment.EvaluationStart()
	state := json.RawMessage(`{"schema":"benchmark-test-capital-state-v1"}`)
	stateSHA := hash(state)
	plan, err := experimentrun.NewPlan(experimentrun.PlanInput{
		ExperimentID: experiment.ID(), ProgramID: fixtureID(20), AccountID: experiment.AccountID(),
		CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: fixtureID(21), CapitalStateBytes: state,
		ManifestID: experiment.ManifestID(), ManifestSHA256: fixtureManifest(t).Digest(), EvaluationStart: start, EvaluationEnd: start.Add(48 * time.Hour), Seed: experiment.Seed(), Mode: experiment.Mode(),
		Steps: []experimentrun.StepInput{{PartitionContentSHA256: strings.Repeat("a", 64), ObservationSourceKey: "benchmark-test", ObservationContentSHA256: strings.Repeat("b", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := experimentrun.NewResult(experimentrun.ResultInput{
		Plan: plan, AccountID: experiment.AccountID(), QualityResultID: experiment.QualityResultID(),
		SimulationPolicyVersion: experiment.SimulationPolicyVersion(), CapitalPolicyVersion: experiment.CapitalPolicyVersion(),
		Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := evaluation.NewPolicy(evaluation.PolicyInput{
		Version: "benchmark-test-v1", Frequency: "daily", PeriodsPerYear: 252, ReturnKind: "simple",
		CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := evaluation.NewReport(evaluation.ReportInput{
		Result: result, Policy: policy, EvaluationStart: start, EvaluationEnd: start.Add(48 * time.Hour),
		Execution: evaluation.ExecutionInput{AttemptedOrders: "0", FilledOrders: "0", AttemptedQuantity: "0", FilledQuantity: "0"},
		Observations: []evaluation.ObservationInput{
			{ObservedAt: start, Equity: "100", BenchmarkValue: "100", CashReturn: "0", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: fixtureEvidence(1), EvidenceSHA256: strings.Repeat("1", 64)},
			{ObservedAt: start.Add(24 * time.Hour), Equity: "102", BenchmarkValue: "105", CashReturn: "0.01", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: fixtureEvidence(2), EvidenceSHA256: strings.Repeat("2", 64)},
			{ObservedAt: start.Add(48 * time.Hour), Equity: "105", BenchmarkValue: "110", CashReturn: "0.01", GrossExposure: "0", NetExposure: "0", LargestPositionWeight: "0", CumulativeOwnershipCost: "0", CumulativeTurnover: "0", CumulativeModeledSlippage: "0", EvidenceID: fixtureEvidence(3), EvidenceSHA256: strings.Repeat("3", 64)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return experiment, report
}

func rebuildEvaluation(t *testing.T, original *evaluation.Report, observations []evaluation.ObservationInput) *evaluation.Report {
	t.Helper()
	_, base := benchmarkParents(t)
	_ = original
	// Reuse the exact result/policy through canonical parent construction by
	// rebuilding the fixture and changing only observation evidence.
	experiment := fixtureExperiment(t)
	start := experiment.EvaluationStart()
	state := json.RawMessage(`{"schema":"benchmark-test-capital-state-v1"}`)
	stateSHA := hash(state)
	plan, _ := experimentrun.NewPlan(experimentrun.PlanInput{ExperimentID: experiment.ID(), ProgramID: fixtureID(20), AccountID: experiment.AccountID(), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: fixtureID(21), CapitalStateBytes: state, ManifestID: experiment.ManifestID(), ManifestSHA256: fixtureManifest(t).Digest(), EvaluationStart: start, EvaluationEnd: start.Add(48 * time.Hour), Seed: experiment.Seed(), Mode: experiment.Mode(), Steps: []experimentrun.StepInput{{PartitionContentSHA256: strings.Repeat("a", 64), ObservationSourceKey: "benchmark-test", ObservationContentSHA256: strings.Repeat("b", 64), AvailableAt: start.Add(time.Minute), Decision: json.RawMessage(`{"signal":"hold"}`), Action: experimentrun.ActionNoop}}})
	result, _ := experimentrun.NewResult(experimentrun.ResultInput{Plan: plan, AccountID: experiment.AccountID(), QualityResultID: experiment.QualityResultID(), SimulationPolicyVersion: experiment.SimulationPolicyVersion(), CapitalPolicyVersion: experiment.CapitalPolicyVersion(), Outcomes: []experimentrun.StepOutcomeInput{{Action: experimentrun.ActionNoop, DecisionSHA256: plan.DecisionSHA256(0), FilledQuantity: "0", FeeTotal: "0"}}})
	policy, _ := evaluation.NewPolicy(evaluation.PolicyInput{Version: "benchmark-test-v1", Frequency: "daily", PeriodsPerYear: 252, ReturnKind: "simple", CashConvention: "explicit_per_period", LotMethod: "fifo", RecoveryDefinition: "first_equity_at_or_above_prior_peak", DecimalScale: 12})
	value, err := evaluation.NewReport(evaluation.ReportInput{Result: result, Policy: policy, EvaluationStart: start, EvaluationEnd: start.Add(48 * time.Hour), Execution: base.Execution(), Observations: observations})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureExperiment(t *testing.T) *strategycatalog.Experiment {
	t.Helper()
	start := time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)
	value, err := strategycatalog.NewExperiment(strategycatalog.ExperimentInput{
		VersionID: fixtureID(10), AccountID: fixtureID(11), CapitalBindingID: fixtureID(12),
		ManifestID: fixtureManifest(t).ID(), QualityResultID: fixtureID(13), SimulationPolicyVersion: "simulation-policy-v1@sha256:" + strings.Repeat("c", 64),
		CapitalPolicyVersion: "capital-margin-policy-v1@sha256:" + strings.Repeat("d", 64), Mode: strategycatalog.ExperimentPaperScored,
		EvaluationStart: start, EvaluationEnd: start.Add(48 * time.Hour), Seed: 401,
	})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureManifest(t *testing.T) *dataset.Manifest      { return newFixtureManifest(t, "benchmark") }
func fixtureOtherManifest(t *testing.T) *dataset.Manifest { return newFixtureManifest(t, "other") }
func newFixtureManifest(t *testing.T, label string) *dataset.Manifest {
	t.Helper()
	cutoff := time.Date(2026, 8, 20, 14, 0, 0, 0, time.UTC)
	value, err := dataset.NewManifest(dataset.ManifestInput{DecisionCutoff: cutoff, Partitions: []dataset.PartitionInput{{
		Kind:     dataset.KindBenchmarkMembership,
		Provider: "fixture", Source: label, Namespace: "benchmark/" + label, RequestSHA256: hash([]byte("request-" + label)), MediaType: "application/json",
		SymbologyVersion: "instrument-v1", AdjustmentPolicy: "total-return", Timezone: "UTC", Calendar: "XNYS-v1", Revision: "v1", License: "test-only", RetentionPolicy: "retain-test-evidence",
		Observations: []dataset.ObservationInput{{SourceKey: label + "-membership", InstrumentID: fixtureInstrument(), EffectiveAt: cutoff.Add(-time.Hour), ObservedAt: cutoff.Add(-30 * time.Minute), AvailableAt: cutoff.Add(-time.Minute), Revision: "1", ContentSHA256: hash([]byte(label))}},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func fixtureID(value int) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte{byte(value)}) }
func fixtureEvidence(value int) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte{byte(value)})
}
func fixtureInstrument() uuid.UUID { return uuid.MustParse("40100000-0000-4000-8000-000000000001") }
