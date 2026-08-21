package trend

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

func TestTrendProgramBuildsStableOrderedOVR303Plan(t *testing.T) {
	program, input := trendProgramFixture(t)
	first, err := program.Plan(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	second, err := program.Plan(context.Background(), input)
	if err != nil || first.ID() != second.ID() || first.Digest() != second.Digest() {
		t.Fatalf("plan replay=%v", err)
	}
	steps := first.Steps()
	if len(steps) == 0 || steps[0].Intent == nil || steps[0].Intent.OrderType != "limit" {
		t.Fatalf("steps=%+v", steps)
	}
}

func TestTrendProgramRejectsEvidenceModeWindowAndIdentityDrift(t *testing.T) {
	program, input := trendProgramFixture(t)
	mutated := input
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
	mutated = input
	mutated.Mode = strategycatalog.ExperimentPaperStress
	if _, err := program.Plan(context.Background(), mutated); err == nil {
		t.Fatal("mode drift admitted")
	}
	policy := trendPolicy(t)
	scenario := trendScenario(t, policy)
	identity := trendIdentity(t, policy, scenario)
	wrong, _ := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: identity.VersionID(), VersionSHA256: identity.VersionSHA256(), CompilerKind: identity.CompilerKind(), CompilerVersion: identity.CompilerVersion(), SourceCommit: identity.SourceCommit(), SourceTreeSHA256: identity.SourceTreeSHA256(), DecisionContract: identity.DecisionContract(), AdapterKind: identity.AdapterKind(), AdapterVersion: identity.AdapterVersion(), AdapterSHA256: strings.Repeat("f", 64), RunnerContract: identity.RunnerContract()})
	if _, err := NewProgram(wrong, policy, scenario); err == nil {
		t.Fatal("identity drift admitted")
	}
}

func trendProgramFixture(t *testing.T) (*Program, experimentrun.ProgramInput) {
	t.Helper()
	policy := trendPolicy(t)
	scenario := trendScenario(t, policy)
	program, err := NewProgram(trendIdentity(t, policy, scenario), policy, scenario)
	if err != nil {
		t.Fatal(err)
	}
	state := json.RawMessage(`{"schema":"trend-capital-state-test-v1"}`)
	stateSHA := hash(state)
	evidence := []experimentrun.ObservationEvidence{}
	for _, rebalance := range scenario.canonical.Rebalances {
		for _, member := range rebalance.Members {
			evidence = append(evidence, experimentrun.ObservationEvidence{PartitionContentSHA256: member.PartitionContentSHA256, SourceKey: member.SourceKey, ContentSHA256: member.EvidenceSHA256, AvailableAt: member.AvailableAt})
		}
	}
	return program, experimentrun.ProgramInput{ExperimentID: trendID("experiment"), AccountID: trendID("account"), CapitalStateID: economicid.DeterministicUUID("capital-state", stateSHA), CapitalStateSHA256: stateSHA, CapitalProjectionCheckpointID: trendID("checkpoint"), CapitalStateBytes: state, ManifestID: trendID("manifest"), ManifestSHA256: strings.Repeat("a", 64), EvaluationStart: scenario.canonical.EvaluationStart, EvaluationEnd: scenario.canonical.EvaluationEnd, Seed: 404, Mode: strategycatalog.ExperimentPaperScored, Evidence: evidence}
}

func trendIdentity(t *testing.T, policy *Policy, scenario *Scenario) *experimentrun.ProgramIdentity {
	t.Helper()
	value, err := experimentrun.NewProgramIdentity(experimentrun.ProgramIdentityInput{VersionID: trendID("version"), VersionSHA256: strings.Repeat("b", 64), CompilerKind: "go", CompilerVersion: "go1.25.8", SourceCommit: strings.Repeat("c", 40), SourceTreeSHA256: strings.Repeat("d", 64), DecisionContract: "etf-time-series-trend-decision-v1", AdapterKind: AdapterKindV1, AdapterVersion: AdapterVersionV1, AdapterSHA256: AdapterSHA256(policy, scenario), RunnerContract: experimentrun.RunnerContractV1})
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func trendID(value string) uuid.UUID { return uuid.NewSHA1(uuid.NameSpaceOID, []byte("trend/"+value)) }
