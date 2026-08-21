package definedrisk

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
)

const (
	AdapterKindV1    = "defined-risk-options"
	AdapterVersionV1 = "v1"
	adapterSchemaV1  = "defined-risk-options-adapter-v1"
)

type adapterCanonical struct {
	Schema, PolicyID, PolicySHA256, ScenarioID, ScenarioSHA256 string
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
	encoded, _ := json.Marshal(adapterCanonical{adapterSchemaV1, policy.ID().String(), policy.Digest(), scenario.ID().String(), scenario.Digest()})
	return hash(encoded)
}

func NewProgram(identity *experimentrun.ProgramIdentity, policy *Policy, scenario *Scenario) (*Program, error) {
	if identity == nil || policy == nil || scenario == nil || identity.AdapterKind() != AdapterKindV1 || identity.AdapterVersion() != AdapterVersionV1 || identity.AdapterSHA256() != AdapterSHA256(policy, scenario) || identity.RunnerContract() != experimentrun.RunnerContractV1 {
		return nil, fmt.Errorf("defined-risk program identity is invalid")
	}
	report, err := NewReport(policy, scenario)
	if err != nil {
		return nil, err
	}
	return &Program{identity, policy, scenario, report}, nil
}

func (p *Program) Identity() *experimentrun.ProgramIdentity {
	if p == nil {
		return nil
	}
	return p.identity
}

func (p *Program) Report() *Report {
	if p == nil {
		return nil
	}
	return p.report
}

func (p *Program) Plan(ctx context.Context, input experimentrun.ProgramInput) (*experimentrun.Plan, error) {
	if p == nil || p.identity == nil || p.policy == nil || p.scenario == nil || p.report == nil {
		return nil, fmt.Errorf("defined-risk program is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.ExperimentID == uuid.Nil || input.AccountID == uuid.Nil || input.ManifestID == uuid.Nil || input.EvaluationStart != p.scenario.canonical.DecisionAt || input.EvaluationEnd != p.scenario.canonical.ExpiryAt || input.Mode != p.scenario.canonical.Mode {
		return nil, fmt.Errorf("defined-risk program input does not match scenario")
	}
	expected := p.expectedEvidence()
	if len(input.Evidence) != len(expected) {
		return nil, fmt.Errorf("defined-risk program requires exact scenario evidence")
	}
	for i, value := range expected {
		if input.Evidence[i] != value {
			return nil, fmt.Errorf("defined-risk program lacks exact ordered manifest evidence")
		}
	}
	if p.report.canonical.Outcome == "rejected" {
		return nil, fmt.Errorf("defined-risk rejected scenario has no executable plan")
	}
	steps := make([]experimentrun.StepInput, len(p.report.canonical.Fills))
	for i, fill := range p.report.canonical.Fills {
		leg, evidence, ok := p.fillMaterial(fill)
		if !ok {
			return nil, fmt.Errorf("defined-risk fill lacks scenario evidence")
		}
		side := "buy"
		if fill.Position == "short" && fill.Action == "open" || fill.Position == "long" && fill.Action == "unwind" {
			side = "sell"
		}
		gross := decimal.RequireFromString(fill.Price).Mul(decimal.RequireFromString(leg.Multiplier)).Mul(decimal.NewFromInt(int64(fill.Quantity)))
		capitalNotional := decimal.RequireFromString(p.report.canonical.ReservedCapital)
		if gross.GreaterThan(capitalNotional) {
			capitalNotional = gross
		}
		decision, _ := json.Marshal(map[string]any{"schema": "defined-risk-options-decision-v1", "policy_id": p.policy.ID().String(), "scenario_id": p.scenario.ID().String(), "report_id": p.report.ID().String(), "execution_mode": p.policy.canonical.ExecutionMode, "fill_sequence": fill.Sequence, "fill_action": fill.Action, "spread_reserved_capital": p.report.canonical.ReservedCapital, "capital_notional": capitalNotional.String()})
		price := fill.Price
		steps[i] = experimentrun.StepInput{PartitionContentSHA256: evidence.PartitionContentSHA256, ObservationSourceKey: evidence.SourceKey, ObservationContentSHA256: evidence.EvidenceSHA256, AvailableAt: parseTime(evidence.AvailableAt), Decision: decision, Action: experimentrun.ActionExecute, Intent: &experimentrun.IntentSpecInput{InstrumentID: uuid.MustParse(leg.InstrumentID), VenueContractID: uuid.MustParse(leg.VenueContractID), Side: side, OrderType: "limit", TimeInForce: "day", Quantity: fmt.Sprint(fill.Quantity), LimitPrice: &price, DecisionAt: parseTime(p.scenario.canonical.DecisionAt), RouteAt: parseTime(p.scenario.canonical.DecisionAt)}}
	}
	return experimentrun.NewPlan(experimentrun.PlanInput{ExperimentID: input.ExperimentID, ProgramID: p.identity.ID(), AccountID: input.AccountID, CapitalStateID: input.CapitalStateID, CapitalStateSHA256: input.CapitalStateSHA256, CapitalProjectionCheckpointID: input.CapitalProjectionCheckpointID, CapitalStateBytes: input.CapitalStateBytes, ManifestID: input.ManifestID, ManifestSHA256: input.ManifestSHA256, EvaluationStart: parseTime(input.EvaluationStart), EvaluationEnd: parseTime(input.EvaluationEnd), Seed: input.Seed, Mode: input.Mode, Steps: steps})
}

func (p *Program) expectedEvidence() []experimentrun.ObservationEvidence {
	values := make([]experimentrun.ObservationEvidence, 0, 4)
	for _, leg := range p.scenario.canonical.Legs {
		values = append(values, observationEvidence(leg.Entry))
		if leg.Unwind != nil {
			values = append(values, observationEvidence(*leg.Unwind))
		}
	}
	values = append(values, experimentrun.ObservationEvidence{PartitionContentSHA256: p.scenario.canonical.TerminalPartitionContentSHA256, SourceKey: p.scenario.canonical.TerminalSourceKey, ContentSHA256: p.scenario.canonical.TerminalEvidenceSHA256, AvailableAt: p.scenario.canonical.TerminalAvailableAt})
	return values
}

func observationEvidence(value quoteCanonical) experimentrun.ObservationEvidence {
	return experimentrun.ObservationEvidence{PartitionContentSHA256: value.PartitionContentSHA256, SourceKey: value.SourceKey, ContentSHA256: value.EvidenceSHA256, AvailableAt: value.AvailableAt}
}

func (p *Program) fillMaterial(fill Fill) (legCanonical, quoteCanonical, bool) {
	for _, leg := range p.scenario.canonical.Legs {
		if leg.InstrumentID != fill.InstrumentID {
			continue
		}
		if fill.Action == "open" && leg.Entry.EvidenceID == fill.EvidenceID {
			return leg, leg.Entry, true
		}
		if fill.Action == "unwind" && leg.Unwind != nil && leg.Unwind.EvidenceID == fill.EvidenceID {
			return leg, *leg.Unwind, true
		}
	}
	return legCanonical{}, quoteCanonical{}, false
}
