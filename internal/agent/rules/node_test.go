package rules

import (
	"context"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestRulesTraderNode_BuySignal(t *testing.T) {
	t.Parallel()
	config := RulesEngineConfig{
		Version: 1,
		Entry: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(30)}},
		},
		Exit: ConditionGroup{
			Operator:   "OR",
			Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(70)}},
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	node := NewRulesTraderNode(config, 100000, nil, nil)
	state := &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars:       []domain.OHLCV{{Close: 150, Open: 148, High: 152, Low: 147, Volume: 200000}},
			Indicators: []domain.Indicator{{Name: "rsi_14", Value: 25}, {Name: "atr_14", Value: 3}},
		},
	}

	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.TradingPlan.Action != domain.PipelineSignalBuy {
		t.Errorf("action = %q, want buy", state.TradingPlan.Action)
	}
	if state.TradingPlan.EntryPrice != 150 {
		t.Errorf("entry = %v, want 150", state.TradingPlan.EntryPrice)
	}
	if state.TradingPlan.PositionSize <= 0 {
		t.Errorf("position size = %v, want > 0", state.TradingPlan.PositionSize)
	}
	if state.FinalSignal.Signal != domain.PipelineSignalBuy {
		t.Errorf("final signal = %q, want buy", state.FinalSignal.Signal)
	}
}

func TestRulesTraderNode_HoldWhenNoConditionsMet(t *testing.T) {
	t.Parallel()
	config := RulesEngineConfig{
		Version: 1,
		Entry: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(30)}},
		},
		Exit: ConditionGroup{
			Operator:   "OR",
			Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(70)}},
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	node := NewRulesTraderNode(config, 100000, nil, nil)
	state := &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars:       []domain.OHLCV{{Close: 150, Open: 148, High: 152, Low: 147, Volume: 200000}},
			Indicators: []domain.Indicator{{Name: "rsi_14", Value: 50}},
		},
	}

	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.TradingPlan.Action != domain.PipelineSignalHold {
		t.Errorf("action = %q, want hold", state.TradingPlan.Action)
	}
}

func TestRulesTraderNode_SellSignal(t *testing.T) {
	t.Parallel()
	config := RulesEngineConfig{
		Version: 1,
		Entry: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(30)}},
		},
		Exit: ConditionGroup{
			Operator:   "OR",
			Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(70)}},
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	journal := NewTradeJournal()
	journal.OpenNewPosition(OpenPosition{Ticker: "AAPL", Side: domain.PositionSideLong, EntryPrice: 140, Quantity: 10})
	node := NewRulesTraderNode(config, 100000, journal, nil)
	state := &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars:       []domain.OHLCV{{Close: 150, Open: 148, High: 152, Low: 147, Volume: 200000}},
			Indicators: []domain.Indicator{{Name: "rsi_14", Value: 75}},
		},
	}

	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.TradingPlan.Action != domain.PipelineSignalSell {
		t.Errorf("action = %q, want sell", state.TradingPlan.Action)
	}
}

func TestIndicatorAnalystNode_AdvancesCursor(t *testing.T) {
	t.Parallel()
	bars := []domain.OHLCV{
		{Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Close: 100, Open: 99, High: 101, Low: 98, Volume: 10000},
		{Timestamp: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), Close: 102, Open: 100, High: 103, Low: 99, Volume: 12000},
		{Timestamp: time.Date(2025, 1, 3, 0, 0, 0, 0, time.UTC), Close: 105, Open: 102, High: 106, Low: 101, Volume: 15000},
	}

	node := NewIndicatorAnalystNode(bars, time.Time{}, nil)

	// Bar 1
	state := &agent.PipelineState{Ticker: "TEST"}
	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("bar 1: %v", err)
	}
	if state.Market == nil {
		t.Fatal("bar 1: market is nil")
	}
	if len(state.Market.Bars) != 1 {
		t.Errorf("bar 1: bars = %d, want 1", len(state.Market.Bars))
	}

	// Bar 2
	state2 := &agent.PipelineState{Ticker: "TEST"}
	if err := node.Execute(context.Background(), state2); err != nil {
		t.Fatalf("bar 2: %v", err)
	}
	if len(state2.Market.Bars) != 2 {
		t.Errorf("bar 2: bars = %d, want 2", len(state2.Market.Bars))
	}

	// Bar 3
	state3 := &agent.PipelineState{Ticker: "TEST"}
	if err := node.Execute(context.Background(), state3); err != nil {
		t.Fatalf("bar 3: %v", err)
	}
	if len(state3.Market.Bars) != 3 {
		t.Errorf("bar 3: bars = %d, want 3", len(state3.Market.Bars))
	}

	// Bar 4 should error (exhausted)
	state4 := &agent.PipelineState{Ticker: "TEST"}
	if err := node.Execute(context.Background(), state4); err == nil {
		t.Fatal("bar 4: expected error for exhausted bars")
	}
}
