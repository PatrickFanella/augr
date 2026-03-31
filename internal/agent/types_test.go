package agent_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

type stubNode struct{}

func (stubNode) Name() string                                        { return "stub" }
func (stubNode) Role() agent.AgentRole                               { return agent.AgentRoleMarketAnalyst }
func (stubNode) Phase() agent.Phase                                  { return agent.PhaseAnalysis }
func (stubNode) Execute(context.Context, *agent.PipelineState) error { return nil }

func TestNodeInterface(_ *testing.T) {
	var _ agent.Node = stubNode{}
}

func TestPipelineStateStoresPhaseOutputs(t *testing.T) {
	runID := uuid.New()
	strategyID := uuid.New()
	expectedErr := errors.New("risk rejected trade")

	state := agent.PipelineState{
		PipelineRunID: runID,
		StrategyID:    strategyID,
		Ticker:        "AAPL",
		AnalystReports: map[agent.AgentRole]string{
			agent.AgentRoleMarketAnalyst:  "Uptrend confirmed",
			agent.AgentRoleBullResearcher: "Momentum remains strong",
			agent.AgentRoleBearResearcher: "Valuation is stretched",
		},
		ResearchDebate: agent.ResearchDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleBullResearcher: "Bull case",
						agent.AgentRoleBearResearcher: "Bear case",
					},
				},
			},
			InvestmentPlan: "Accumulate on pullbacks with a defined stop.",
		},
		TradingPlan: agent.TradingPlan{
			Action:       agent.PipelineSignalBuy,
			Ticker:       "AAPL",
			EntryType:    "limit",
			EntryPrice:   215.25,
			PositionSize: 5000,
			StopLoss:     209.50,
			TakeProfit:   228.00,
			TimeHorizon:  "swing",
			Confidence:   0.78,
			Rationale:    "Momentum and analyst alignment support a swing entry.",
			RiskReward:   2.2,
		},
		RiskDebate: agent.RiskDebateState{
			Rounds: []agent.DebateRound{
				{
					Number: 1,
					Contributions: map[agent.AgentRole]string{
						agent.AgentRoleRiskManager: "Size should remain below normal due to earnings proximity.",
					},
				},
			},
			FinalSignal: "Approve reduced-size long position.",
		},
		FinalSignal: agent.FinalSignal{
			Signal:     agent.PipelineSignalBuy,
			Confidence: 0.72,
		},
		Errors: []error{expectedErr},
	}

	if state.PipelineRunID != runID {
		t.Fatalf("PipelineRunID = %v, want %v", state.PipelineRunID, runID)
	}
	if state.StrategyID != strategyID {
		t.Fatalf("StrategyID = %v, want %v", state.StrategyID, strategyID)
	}
	if got := state.AnalystReports[agent.AgentRoleMarketAnalyst]; got != "Uptrend confirmed" {
		t.Fatalf("AnalystReports[market_analyst] = %q, want %q", got, "Uptrend confirmed")
	}
	if got := state.ResearchDebate.InvestmentPlan; got != "Accumulate on pullbacks with a defined stop." {
		t.Fatalf("ResearchDebate.InvestmentPlan = %q, want expected plan", got)
	}
	if got := state.TradingPlan.Action; got != agent.PipelineSignalBuy {
		t.Fatalf("TradingPlan.Action = %q, want %q", got, agent.PipelineSignalBuy)
	}
	if got := state.RiskDebate.FinalSignal; got != "Approve reduced-size long position." {
		t.Fatalf("RiskDebate.FinalSignal = %q, want expected verdict", got)
	}
	if got := state.FinalSignal.Signal; got != agent.PipelineSignalBuy {
		t.Fatalf("FinalSignal.Signal = %q, want %q", got, agent.PipelineSignalBuy)
	}
	if len(state.Errors) != 1 || !errors.Is(state.Errors[0], expectedErr) {
		t.Fatalf("Errors = %v, want [%v]", state.Errors, expectedErr)
	}
}

func TestPipelineEventTypesCoverUserVisibleTransitions(t *testing.T) {
	timestamp := time.Now().UTC()
	finalSignalPayload, err := json.Marshal(agent.FinalSignal{
		Signal:     agent.PipelineSignalHold,
		Confidence: 0.64,
	})
	if err != nil {
		t.Fatalf("json.Marshal(FinalSignal) error = %v", err)
	}

	event := agent.PipelineEvent{
		Type:          agent.SignalGenerated,
		PipelineRunID: uuid.New(),
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		AgentRole:     agent.AgentRoleRiskManager,
		Phase:         agent.PhaseRiskDebate,
		Round:         2,
		Payload:       finalSignalPayload,
		OccurredAt:    timestamp,
	}

	if got := agent.PipelineStarted.String(); got != "pipeline_started" {
		t.Fatalf("PipelineStarted.String() = %q, want %q", got, "pipeline_started")
	}
	if got := agent.AgentDecisionMade.String(); got != "agent_decision_made" {
		t.Fatalf("AgentDecisionMade.String() = %q, want %q", got, "agent_decision_made")
	}
	if got := agent.DebateRoundCompleted.String(); got != "debate_round_completed" {
		t.Fatalf("DebateRoundCompleted.String() = %q, want %q", got, "debate_round_completed")
	}
	if got := agent.SignalGenerated.String(); got != "signal_generated" {
		t.Fatalf("SignalGenerated.String() = %q, want %q", got, "signal_generated")
	}
	if got := agent.LLMCacheStatsReported.String(); got != "llm_cache_stats_reported" {
		t.Fatalf("LLMCacheStatsReported.String() = %q, want %q", got, "llm_cache_stats_reported")
	}
	if got := agent.PipelineCompleted.String(); got != "pipeline_completed" {
		t.Fatalf("PipelineCompleted.String() = %q, want %q", got, "pipeline_completed")
	}
	if got := agent.PipelineError.String(); got != "pipeline_error" {
		t.Fatalf("PipelineError.String() = %q, want %q", got, "pipeline_error")
	}
	if got := agent.AgentEventKindPhaseStarted.String(); got != "phase_started" {
		t.Fatalf("AgentEventKindPhaseStarted.String() = %q, want %q", got, "phase_started")
	}
	if got := agent.AgentEventKindPhaseCompleted.String(); got != "phase_completed" {
		t.Fatalf("AgentEventKindPhaseCompleted.String() = %q, want %q", got, "phase_completed")
	}
	if got := agent.AgentEventKindAgentStarted.String(); got != "agent_started" {
		t.Fatalf("AgentEventKindAgentStarted.String() = %q, want %q", got, "agent_started")
	}
	if got := agent.AgentEventKindAgentCompleted.String(); got != "agent_completed" {
		t.Fatalf("AgentEventKindAgentCompleted.String() = %q, want %q", got, "agent_completed")
	}
	if got := agent.AgentEventKindDebateRoundCompleted.String(); got != "debate_round_completed" {
		t.Fatalf("AgentEventKindDebateRoundCompleted.String() = %q, want %q", got, "debate_round_completed")
	}
	if got := agent.AgentEventKindSignalProduced.String(); got != "signal_produced" {
		t.Fatalf("AgentEventKindSignalProduced.String() = %q, want %q", got, "signal_produced")
	}
	if got := agent.AgentEventKindPipelineStarted.String(); got != "pipeline_started" {
		t.Fatalf("AgentEventKindPipelineStarted.String() = %q, want %q", got, "pipeline_started")
	}
	if got := agent.AgentEventKindPipelineCompleted.String(); got != "pipeline_completed" {
		t.Fatalf("AgentEventKindPipelineCompleted.String() = %q, want %q", got, "pipeline_completed")
	}
	if got := agent.AgentEventKindPipelineFailed.String(); got != "pipeline_failed" {
		t.Fatalf("AgentEventKindPipelineFailed.String() = %q, want %q", got, "pipeline_failed")
	}

	if string(event.Payload) != string(finalSignalPayload) {
		t.Fatalf("Payload = %s, want %s", event.Payload, finalSignalPayload)
	}

	marshaled, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("json.Marshal(PipelineEvent) error = %v", err)
	}
	if !json.Valid(marshaled) {
		t.Fatalf("json.Marshal(PipelineEvent) produced invalid JSON: %s", marshaled)
	}

	var decoded struct {
		Type          string          `json:"type"`
		PipelineRunID string          `json:"pipeline_run_id"`
		Payload       json.RawMessage `json:"payload"`
	}
	if err := json.Unmarshal(marshaled, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(PipelineEvent) error = %v", err)
	}
	if decoded.Type != agent.SignalGenerated.String() {
		t.Fatalf("decoded.Type = %q, want %q", decoded.Type, agent.SignalGenerated.String())
	}
	if decoded.PipelineRunID == "" {
		t.Fatal("decoded.PipelineRunID is empty")
	}

	var payload agent.FinalSignal
	if err := json.Unmarshal(decoded.Payload, &payload); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}
	if payload.Signal != agent.PipelineSignalHold || payload.Confidence != 0.64 {
		t.Fatalf("Payload = %+v, want hold @ 0.64", payload)
	}
}
