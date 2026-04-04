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
// It uses the TradeJournal for position awareness.
type RulesTraderNode struct {
	config   RulesEngineConfig
	prevSnap *Snapshot
	equity   float64
	journal  *TradeJournal
	logger   *slog.Logger
}

// NewRulesTraderNode creates a rules-based trader node. If journal is nil, a
// new empty journal is created.
func NewRulesTraderNode(config RulesEngineConfig, equity float64, journal *TradeJournal, logger *slog.Logger) *RulesTraderNode {
	if logger == nil {
		logger = slog.Default()
	}
	if journal == nil {
		journal = NewTradeJournal()
	}
	return &RulesTraderNode{config: config, equity: equity, journal: journal, logger: logger}
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

	// Evaluate entry and exit conditions with journal-based position awareness.
	signal := domain.PipelineSignalHold
	holding := n.journal.IsHolding(state.Ticker)
	if !holding && EvaluateGroup(n.config.Entry, snap, n.prevSnap) {
		signal = domain.PipelineSignalBuy
	} else if holding && EvaluateGroup(n.config.Exit, snap, n.prevSnap) {
		signal = domain.PipelineSignalSell
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
