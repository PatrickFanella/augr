package rules

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// RulesTraderNode is a trading-phase Node that evaluates deterministic rules
// instead of calling an LLM. It reads indicators from state.Market.Indicators,
// evaluates entry/exit conditions, and writes the result to state.TradingPlan.
// It tracks whether a position is open to avoid re-entering immediately after exit.
type RulesTraderNode struct {
	config     RulesEngineConfig
	prevSnap   *Snapshot
	equity     float64
	inPosition bool
	logger     *slog.Logger
}

// NewRulesTraderNode creates a rules-based trader node.
func NewRulesTraderNode(config RulesEngineConfig, equity float64, logger *slog.Logger) *RulesTraderNode {
	if logger == nil {
		logger = slog.Default()
	}
	return &RulesTraderNode{config: config, equity: equity, logger: logger}
}

func (n *RulesTraderNode) Name() string          { return "rules_trader" }
func (n *RulesTraderNode) Role() agent.AgentRole  { return agent.AgentRoleTrader }
func (n *RulesTraderNode) Phase() agent.Phase     { return agent.PhaseTrading }

// Execute evaluates rules against the current bar's indicators and writes a
// TradingPlan to state.
func (n *RulesTraderNode) Execute(_ context.Context, state *agent.PipelineState) error {
	if state.Market == nil || len(state.Market.Bars) == 0 {
		state.TradingPlan = agent.TradingPlan{
			Action:    domain.PipelineSignalHold,
			Ticker:    state.Ticker,
			Rationale: "No market data available.",
		}
		return nil
	}

	bar := state.Market.Bars[len(state.Market.Bars)-1]
	snap := NewSnapshotFromBar(state.Market.Indicators, bar)

	// Check filters first.
	if !PassesFilters(n.config.Filters, snap) {
		state.TradingPlan = agent.TradingPlan{
			Action:    domain.PipelineSignalHold,
			Ticker:    state.Ticker,
			Rationale: "Filters not met (volume or ATR below minimum).",
		}
		n.prevSnap = &snap
		return nil
	}

	// Evaluate entry and exit conditions with position awareness.
	// Only enter when flat, only exit when holding.
	signal := domain.PipelineSignalHold
	if !n.inPosition && EvaluateGroup(n.config.Entry, snap, n.prevSnap) {
		signal = domain.PipelineSignalBuy
		n.inPosition = true
	} else if n.inPosition && EvaluateGroup(n.config.Exit, snap, n.prevSnap) {
		signal = domain.PipelineSignalSell
		n.inPosition = false
	}

	plan := BuildTradingPlan(&n.config, snap, signal, state.Ticker, n.equity)
	state.TradingPlan = plan
	state.FinalSignal = agent.FinalSignal{
		Signal:     signal,
		Confidence: plan.Confidence,
	}

	// Persist for auditability.
	output, _ := json.Marshal(plan)
	state.RecordDecision(agent.AgentRoleTrader, agent.PhaseTrading, nil, string(output), nil)

	n.prevSnap = &snap
	return nil
}

// Reset clears the previous snapshot state for reuse.
func (n *RulesTraderNode) Reset() { n.prevSnap = nil }
