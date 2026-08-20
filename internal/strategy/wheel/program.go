package wheel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
)

const (
	AdapterKindV1    = "quality-filtered-wheel"
	AdapterVersionV1 = "v1"
	adapterSchemaV1  = "quality-filtered-wheel-adapter-v1"
)

type adapterCanonical struct {
	Schema         string `json:"schema"`
	PolicyID       string `json:"policy_id"`
	PolicySHA256   string `json:"policy_sha256"`
	ScenarioID     string `json:"scenario_id"`
	ScenarioSHA256 string `json:"scenario_sha256"`
}

type Program struct {
	identity *experimentrun.ProgramIdentity
	policy   *Policy
	scenario *Scenario
	report   *Report
}

func AdapterSHA256(policy *Policy, scenario *Scenario) string {
	if policy == nil || scenario == nil {
		return ""
	}
	encoded, _ := json.Marshal(adapterCanonical{Schema: adapterSchemaV1, PolicyID: policy.ID().String(), PolicySHA256: policy.Digest(), ScenarioID: scenario.ID().String(), ScenarioSHA256: scenario.Digest()})
	return hash(encoded)
}

func NewProgram(identity *experimentrun.ProgramIdentity, policy *Policy, scenario *Scenario) (*Program, error) {
	if identity == nil || policy == nil || scenario == nil || identity.AdapterKind() != AdapterKindV1 || identity.AdapterVersion() != AdapterVersionV1 ||
		identity.AdapterSHA256() != AdapterSHA256(policy, scenario) || identity.RunnerContract() != experimentrun.RunnerContractV1 {
		return nil, fmt.Errorf("wheel program identity is invalid")
	}
	report, err := NewReport(policy, scenario)
	if err != nil {
		return nil, err
	}
	return &Program{identity: identity, policy: policy, scenario: scenario, report: report}, nil
}

func (program *Program) Identity() *experimentrun.ProgramIdentity {
	if program == nil {
		return nil
	}
	return program.identity
}

func (program *Program) Report() *Report {
	if program == nil {
		return nil
	}
	return program.report
}

func (program *Program) Plan(ctx context.Context, input experimentrun.ProgramInput) (*experimentrun.Plan, error) {
	if program == nil || program.identity == nil || program.policy == nil || program.scenario == nil || program.report == nil {
		return nil, fmt.Errorf("wheel program is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.ExperimentID == uuid.Nil || input.AccountID == uuid.Nil || input.ManifestID == uuid.Nil || input.EvaluationStart != formatTime(program.scenario.EvaluationStart()) ||
		input.EvaluationEnd != formatTime(program.scenario.EvaluationEnd()) || input.Mode != program.scenario.Mode() {
		return nil, fmt.Errorf("wheel program input does not match scenario")
	}
	expectedEvidence := make([]candidateCanonical, 0, len(input.Evidence))
	for _, event := range program.scenario.canonical.Events {
		expectedEvidence = append(expectedEvidence, event.Candidates...)
	}
	if len(input.Evidence) != len(expectedEvidence) {
		return nil, fmt.Errorf("wheel program requires exact scenario evidence")
	}
	for index, candidate := range expectedEvidence {
		value := input.Evidence[index]
		if value.PartitionContentSHA256 != candidate.PartitionContentSHA256 || value.SourceKey != candidate.SourceKey || value.ContentSHA256 != candidate.EvidenceSHA256 || value.AvailableAt != candidate.AvailableAt {
			return nil, fmt.Errorf("wheel program candidate lacks exact ordered manifest evidence")
		}
	}
	steps := make([]experimentrun.StepInput, 0, 2)
	for sequence, transition := range program.report.canonical.Transitions {
		if transition.Action != "short_put_opened" && transition.Action != "covered_call_opened" {
			continue
		}
		event := program.scenario.canonical.Events[sequence]
		candidate, ok := selectedCandidate(event, transition.SelectedInstrumentID)
		if !ok {
			return nil, fmt.Errorf("wheel selected option is absent from scenario")
		}
		quantity := ""
		for _, effect := range transition.Effects {
			if effect.Kind == "premium" && effect.InstrumentID == transition.SelectedInstrumentID {
				quantity = effect.Quantity
				break
			}
		}
		if quantity == "" {
			return nil, fmt.Errorf("wheel opening transition lacks contract quantity")
		}
		capitalNotional := decimal.RequireFromString(candidate.Strike).Mul(decimal.RequireFromString(program.policy.canonical.DeliverableQuantity)).Mul(decimal.RequireFromString(quantity)).String()
		decision, _ := json.Marshal(map[string]any{"schema": "quality-filtered-wheel-decision-v1", "policy_id": program.policy.ID().String(), "scenario_id": program.scenario.ID().String(), "report_id": program.report.ID().String(), "event_sequence": sequence, "action": transition.Action, "reason": transition.Reason, "selected_instrument_id": transition.SelectedInstrumentID, "capital_notional": capitalNotional})
		limitPrice := candidate.Bid
		occurredAt := parseTime(event.OccurredAt)
		steps = append(steps, experimentrun.StepInput{
			PartitionContentSHA256: candidate.PartitionContentSHA256, ObservationSourceKey: candidate.SourceKey,
			ObservationContentSHA256: candidate.EvidenceSHA256, AvailableAt: parseTime(candidate.AvailableAt), Decision: decision, Action: experimentrun.ActionExecute,
			Intent: &experimentrun.IntentSpecInput{InstrumentID: uuid.MustParse(candidate.InstrumentID), VenueContractID: uuid.MustParse(candidate.VenueContractID), Side: "sell", OrderType: "limit", TimeInForce: "day", Quantity: quantity, LimitPrice: &limitPrice, DecisionAt: occurredAt, RouteAt: occurredAt},
		})
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("wheel scenario contains no executable opening decision")
	}
	return experimentrun.NewPlan(experimentrun.PlanInput{
		ExperimentID: input.ExperimentID, ProgramID: program.identity.ID(), AccountID: input.AccountID,
		CapitalStateID: input.CapitalStateID, CapitalStateSHA256: input.CapitalStateSHA256, CapitalProjectionCheckpointID: input.CapitalProjectionCheckpointID,
		CapitalStateBytes: input.CapitalStateBytes, ManifestID: input.ManifestID, ManifestSHA256: input.ManifestSHA256,
		EvaluationStart: parseTime(input.EvaluationStart), EvaluationEnd: parseTime(input.EvaluationEnd), Seed: input.Seed, Mode: input.Mode, Steps: steps,
	})
}

func selectedCandidate(event eventCanonical, instrumentID string) (candidateCanonical, bool) {
	for _, candidate := range event.Candidates {
		if candidate.InstrumentID == instrumentID {
			return candidate, true
		}
	}
	return candidateCanonical{}, false
}
