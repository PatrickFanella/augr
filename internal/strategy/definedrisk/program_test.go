package definedrisk

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestProgramBuildsStableAtomicAndOrphanPlans(t *testing.T) {
	for _, mode := range []ExecutionMode{ExecutionAtomic, ExecutionSequential} {
		t.Run(string(mode), func(t *testing.T) {
			shortDepth := "10"
			if mode == ExecutionSequential {
				shortDepth = "0"
			}
			program, input := programFixture(t, mode, shortDepth)
			first, err := program.Plan(context.Background(), input)
			if err != nil {
				t.Fatal(err)
			}
			second, err := program.Plan(context.Background(), input)
			if err != nil || first.ID() != second.ID() || first.Digest() != second.Digest() {
				t.Fatalf("replay=%v", err)
			}
			steps := first.Steps()
			if len(steps) != 2 || steps[0].Intent.Side != "buy" || steps[1].Intent.Side != "sell" {
				t.Fatalf("steps=%+v", steps)
			}
			var decision map[string]any
			if json.Unmarshal(steps[0].Decision, &decision) != nil || decision["spread_reserved_capital"] == "" || decision["execution_mode"] != string(mode) {
				t.Fatalf("decision=%s", steps[0].Decision)
			}
		})
	}
}

func TestProgramRejectsEvidenceWindowModeAndIdentityDrift(t *testing.T) {
	program, input := programFixture(t, ExecutionAtomic, "10")
	mutated := input
	mutated.Evidence = append([]experimentrun.ObservationEvidence(nil), input.Evidence...)
	mutated.Evidence[0], mutated.Evidence[1] = mutated.Evidence[1], mutated.Evidence[0]
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("reordered evidence accepted")
	}
	mutated = input
	mutated.Evidence = mutated.Evidence[:len(mutated.Evidence)-1]
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("partial evidence accepted")
	}
	mutated = input
	mutated.Mode = strategycatalog.ExperimentPaperStress
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("mode drift accepted")
	}
	policy := testPolicy(t, ExecutionAtomic)
	scenario := testScenario(t, policy, BullCall, "call", "long", "short", "105", "10")
	identity := programIdentity(t, policy, scenario)
	wrong, _ := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: identity.VersionID(), VersionSHA256: identity.VersionSHA256(), CompilerKind: identity.CompilerKind(), CompilerVersion: identity.CompilerVersion(), SourceCommit: identity.SourceCommit(), SourceTreeSHA256: identity.SourceTreeSHA256(), DecisionContract: identity.DecisionContract(), AdapterKind: identity.AdapterKind(), AdapterVersion: identity.AdapterVersion(), AdapterSHA256: strings.Repeat("f", 64), RunnerContract: identity.RunnerContract()})
	if _, err := NewProgram(wrong, policy, scenario); err == nil {
		t.Fatal("identity drift accepted")
	}
}

func programFixture(t *testing.T, mode ExecutionMode, shortDepth string) (*Program, experimentrun.ProgramInput) {
	t.Helper()
	policy := testPolicy(t, mode)
	scenario := testScenario(t, policy, BullCall, "call", "long", "short", "105", shortDepth)
	program, err := NewProgram(programIdentity(t, policy, scenario), policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"schema":"defined-risk-capital-state-test-v1"}`)
	stateSHA := hash(state)
	return program, experimentrun.ProgramInput{ExperimentID: definedRiskID("experiment"), AccountID: definedRiskID("account"), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: definedRiskID("checkpoint"), CapitalStateBytes: state, ManifestID: definedRiskID("manifest"), ManifestSHA256: strings.Repeat("a", 64), EvaluationStart: scenario.canonical.DecisionAt, EvaluationEnd: scenario.canonical.ExpiryAt, Seed: 405, Mode: strategycatalog.ExperimentPaperScored, Evidence: program.expectedEvidence()}
}

func programIdentity(t *testing.T, policy *Policy, scenario *Scenario) *experimentrun.ProgramIdentity {
	t.Helper()
	value, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: definedRiskID("version"), VersionSHA256: strings.Repeat("b", 64), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("c", 40), SourceTreeSHA256: strings.Repeat("d", 64), DecisionContract: "defined-risk-options-decision-v1", AdapterKind: AdapterKindV1, AdapterVersion: AdapterVersionV1, AdapterSHA256: AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func definedRiskID(value string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("defined-risk/"+value))
}
