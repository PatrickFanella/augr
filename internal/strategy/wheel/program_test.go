package wheel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestWheelProgramBuildsStableOVR303OpeningPlan(t *testing.T) {
	t.Parallel()
	program, input := wheelProgramFixture(t)
	first, err := program.Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := program.Plan(context.Background(), input)
	if err != nil || first.ID() != second.ID() || first.Digest() != second.Digest() {
		t.Fatalf("replay=%v/%v", second, err)
	}
	steps := first.Steps()
	if len(steps) != 2 || steps[0].Intent == nil || steps[0].Intent.Side != "sell" || steps[0].Intent.Quantity != "1" || steps[0].Intent.LimitPrice == nil || *steps[0].Intent.LimitPrice != "2" ||
		steps[1].Intent == nil || steps[1].Intent.Quantity != "1" || *steps[1].Intent.LimitPrice != "1.5" {
		t.Fatalf("steps=%+v", steps)
	}
	if program.Report().AfterCostTotalReturn() != "0.094800000000" {
		t.Fatalf("report=%s", program.Report().AfterCostTotalReturn())
	}
}

func TestWheelProgramRejectsEvidenceAndIdentitySubstitution(t *testing.T) {
	t.Parallel()
	program, input := wheelProgramFixture(t)
	mutated := input
	mutated.Evidence = append([]experimentrun.ObservationEvidence(nil), input.Evidence...)
	mutated.Evidence[0].ContentSHA256 = strings.Repeat("f", 64)
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("changed evidence admitted")
	}
	mutated = input
	mutated.EvaluationEnd = input.EvaluationStart
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("changed window admitted")
	}
	mutated = input
	mutated.Mode = strategycatalog.ExperimentPaperStress
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("changed mode admitted")
	}
	mutated = input
	mutated.Evidence = append([]experimentrun.ObservationEvidence(nil), input.Evidence...)
	mutated.Evidence[0], mutated.Evidence[1] = mutated.Evidence[1], mutated.Evidence[0]
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("reordered evidence admitted")
	}
	mutated = input
	mutated.Evidence = mutated.Evidence[:len(mutated.Evidence)-1]
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("partial evidence admitted")
	}
	policy, scenario := wheelFixture(t, false)
	identity := wheelIdentity(t, policy, scenario)
	wrong, _ := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: identity.VersionID(), VersionSHA256: identity.VersionSHA256(), CompilerKind: identity.CompilerKind(), CompilerVersion: identity.CompilerVersion(), SourceCommit: identity.SourceCommit(), SourceTreeSHA256: identity.SourceTreeSHA256(), DecisionContract: identity.DecisionContract(), AdapterKind: identity.AdapterKind(), AdapterVersion: identity.AdapterVersion(), AdapterSHA256: strings.Repeat("e", 64), RunnerContract: identity.RunnerContract()})
	if _, err := NewProgram(wrong, policy, scenario); err == nil {
		t.Fatal("changed adapter identity admitted")
	}
}

func wheelProgramFixture(t *testing.T) (*Program, experimentrun.ProgramInput) {
	t.Helper()
	policy, scenario := wheelFixture(t, true)
	identity := wheelIdentity(t, policy, scenario)
	program, err := NewProgram(identity, policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"schema":"wheel-capital-state-test-v1"}`)
	stateSHA := hash(state)
	evidence := []experimentrun.ObservationEvidence{}
	for _, event := range scenario.canonical.Events {
		for _, candidate := range event.Candidates {
			evidence = append(evidence, experimentrun.ObservationEvidence{PartitionContentSHA256: candidate.PartitionContentSHA256, SourceKey: candidate.SourceKey, ContentSHA256: candidate.EvidenceSHA256, AvailableAt: candidate.AvailableAt})
		}
	}
	return program, experimentrun.ProgramInput{
		ExperimentID: wheelID(900), AccountID: wheelID(901), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA,
		CapitalProjectionCheckpointID: wheelID(902), CapitalStateBytes: state, ManifestID: wheelID(903), ManifestSHA256: strings.Repeat("a", 64), EvaluationStart: formatTime(scenario.EvaluationStart()), EvaluationEnd: formatTime(scenario.EvaluationEnd()), Seed: 402, Mode: strategycatalog.ExperimentPaperScored, Evidence: evidence,
	}
}

func wheelIdentity(t *testing.T, policy *Policy, scenario *Scenario) *experimentrun.ProgramIdentity {
	t.Helper()
	value, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: wheelID(800), VersionSHA256: strings.Repeat("b", 64), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("c", 40), SourceTreeSHA256: strings.Repeat("d", 64), DecisionContract: "quality-filtered-wheel-decision-v1", AdapterKind: AdapterKindV1, AdapterVersion: AdapterVersionV1, AdapterSHA256: AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

var _ experimentrun.Program = (*Program)(nil)
