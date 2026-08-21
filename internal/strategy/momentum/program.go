package momentum

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
)

const (
	AdapterKindV1    = "momentum-quality"
	AdapterVersionV1 = "v1"
	adapterSchemaV1  = "momentum-quality-adapter-v1"
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
	if identity == nil || policy == nil || scenario == nil || identity.AdapterKind() != AdapterKindV1 || identity.AdapterVersion() != AdapterVersionV1 || identity.AdapterSHA256() != AdapterSHA256(policy, scenario) || identity.RunnerContract() != experimentrun.RunnerContractV1 {
		return nil, fmt.Errorf("momentum program identity is invalid")
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
		return nil, fmt.Errorf("momentum program is incomplete")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if input.ExperimentID == uuid.Nil || input.AccountID == uuid.Nil || input.ManifestID == uuid.Nil || input.EvaluationStart != program.scenario.canonical.EvaluationStart || input.EvaluationEnd != program.scenario.canonical.EvaluationEnd || input.Mode != program.scenario.Mode() {
		return nil, fmt.Errorf("momentum program input does not match scenario")
	}
	expected := []memberCanonical{}
	for _, rebalance := range program.scenario.canonical.Rebalances {
		expected = append(expected, rebalance.Members...)
	}
	if len(input.Evidence) != len(expected) {
		return nil, fmt.Errorf("momentum program requires exact scenario evidence")
	}
	for index, member := range expected {
		value := input.Evidence[index]
		if value.PartitionContentSHA256 != member.PartitionContentSHA256 || value.SourceKey != member.SourceKey || value.ContentSHA256 != member.EvidenceSHA256 || value.AvailableAt != member.AvailableAt {
			return nil, fmt.Errorf("momentum program requires exact ordered manifest evidence")
		}
	}
	steps := []experimentrun.StepInput{}
	for _, rebalance := range program.report.canonical.Rebalances {
		event := program.scenario.canonical.Rebalances[rebalance.Sequence]
		for tradeSequence, trade := range rebalance.Trades {
			member, ok := scenarioMember(event, trade.InstrumentID)
			if !ok {
				return nil, fmt.Errorf("momentum trade lacks scenario member")
			}
			decision, _ := json.Marshal(map[string]any{"schema": "momentum-quality-decision-v1", "policy_id": program.policy.ID().String(), "scenario_id": program.scenario.ID().String(), "report_id": program.report.ID().String(), "rebalance_sequence": rebalance.Sequence, "trade_sequence": tradeSequence, "regime": rebalance.Regime, "instrument_id": trade.InstrumentID, "side": trade.Side, "capital_notional": trade.Notional})
			price := decimal.RequireFromString(trade.Price).String()
			quantity := decimal.RequireFromString(trade.Quantity).String()
			occurredAt := parseTime(rebalance.OccurredAt)
			steps = append(steps, experimentrun.StepInput{PartitionContentSHA256: member.PartitionContentSHA256, ObservationSourceKey: member.SourceKey, ObservationContentSHA256: member.EvidenceSHA256, AvailableAt: parseTime(member.AvailableAt), Decision: decision, Action: experimentrun.ActionExecute, Intent: &experimentrun.IntentSpecInput{InstrumentID: uuid.MustParse(trade.InstrumentID), VenueContractID: uuid.MustParse(trade.VenueContractID), Side: trade.Side, OrderType: "limit", TimeInForce: "day", Quantity: quantity, LimitPrice: &price, DecisionAt: occurredAt, RouteAt: occurredAt}})
		}
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("momentum scenario contains no executable rebalance")
	}
	return experimentrun.NewPlan(experimentrun.PlanInput{ExperimentID: input.ExperimentID, ProgramID: program.identity.ID(), AccountID: input.AccountID, CapitalStateID: input.CapitalStateID, CapitalStateSHA256: input.CapitalStateSHA256, CapitalProjectionCheckpointID: input.CapitalProjectionCheckpointID, CapitalStateBytes: input.CapitalStateBytes, ManifestID: input.ManifestID, ManifestSHA256: input.ManifestSHA256, EvaluationStart: parseTime(input.EvaluationStart), EvaluationEnd: parseTime(input.EvaluationEnd), Seed: input.Seed, Mode: input.Mode, Steps: steps})
}

func scenarioMember(rebalance rebalanceCanonical, instrumentID string) (memberCanonical, bool) {
	for _, member := range rebalance.Members {
		if member.InstrumentID == instrumentID {
			return member, true
		}
	}
	return memberCanonical{}, false
}

var _ experimentrun.Program = (*Program)(nil)
