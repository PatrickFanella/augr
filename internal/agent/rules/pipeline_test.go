package rules

import (
	"context"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// generateBars creates n OHLCV bars with a configurable price generator.
// priceFunc receives the bar index (0-based) and returns (open, high, low, close, volume).
func generateBars(n int, start time.Time, priceFunc func(i int) (float64, float64, float64, float64, float64)) []domain.OHLCV {
	bars := make([]domain.OHLCV, n)
	for i := range bars {
		o, h, l, c, v := priceFunc(i)
		bars[i] = domain.OHLCV{
			Timestamp: start.AddDate(0, 0, i),
			Open:      o,
			High:      h,
			Low:       l,
			Close:     c,
			Volume:    v,
		}
	}
	return bars
}

// flatPriceAt returns a priceFunc that produces flat bars at the given price.
func flatPriceAt(price, volume float64) func(int) (float64, float64, float64, float64, float64) {
	return func(_ int) (float64, float64, float64, float64, float64) {
		return price, price + 1, price - 1, price, volume
	}
}

// smaCrossConfig returns a RulesEngineConfig using SMA-20 cross above/below close
// as entry/exit conditions. This is a simple config suitable for integration testing.
func smaCrossConfig() RulesEngineConfig {
	return RulesEngineConfig{
		Version: 1,
		Name:    "test_sma_cross",
		Entry: ConditionGroup{
			Operator: "AND",
			Conditions: []Condition{
				{Field: "close", Op: "gt", Ref: "sma_20"},
			},
		},
		Exit: ConditionGroup{
			Operator: "AND",
			Conditions: []Condition{
				{Field: "close", Op: "lt", Ref: "sma_20"},
			},
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 3},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}
}

func TestPipeline_FullExecution_BuySignal(t *testing.T) {
	t.Parallel()
	// Build 250 bars: first 200 at $100, then 50 trending up to trigger close > sma_20.
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(250, start, func(i int) (float64, float64, float64, float64, float64) {
		if i < 200 {
			return 100, 101, 99, 100, 1000000
		}
		// Rising price: 101, 102, 103, ...
		p := 100 + float64(i-199)
		return p - 1, p + 1, p - 2, p, 1000000
	})

	// Start the pipeline after warmup at bar 210 (well past SMA-200 warmup).
	startDate := start.AddDate(0, 0, 210)
	pipeline := NewRulesPipeline(smaCrossConfig(), bars, startDate, 100000, agent.NoopPersister{}, nil, nil)

	state, err := pipeline.Execute(context.Background(), domain.Strategy{Ticker: "TEST"}.ID, "TEST")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state == nil {
		t.Fatal("state is nil")
	}

	// After warmup + rising prices, close > sma_20 should produce a buy signal.
	if state.TradingPlan.Action != domain.PipelineSignalBuy {
		t.Errorf("action = %q, want buy", state.TradingPlan.Action)
	}
	if state.TradingPlan.Ticker != "TEST" {
		t.Errorf("ticker = %q, want TEST", state.TradingPlan.Ticker)
	}
	if state.TradingPlan.EntryPrice <= 0 {
		t.Errorf("entry price = %v, want > 0", state.TradingPlan.EntryPrice)
	}
	if state.TradingPlan.StopLoss <= 0 {
		t.Errorf("stop loss = %v, want > 0", state.TradingPlan.StopLoss)
	}
	if state.TradingPlan.PositionSize <= 0 {
		t.Errorf("position size = %v, want > 0", state.TradingPlan.PositionSize)
	}
}

func TestPipeline_FullExecution_HoldSignal(t *testing.T) {
	t.Parallel()
	// All bars at same price -> close == sma_20 -> no entry (gt not met).
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(250, start, flatPriceAt(100, 1000000))
	startDate := start.AddDate(0, 0, 210)

	pipeline := NewRulesPipeline(smaCrossConfig(), bars, startDate, 100000, agent.NoopPersister{}, nil, nil)

	state, err := pipeline.Execute(context.Background(), domain.Strategy{Ticker: "FLAT"}.ID, "FLAT")
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.TradingPlan.Action != domain.PipelineSignalHold {
		t.Errorf("action = %q, want hold (flat market)", state.TradingPlan.Action)
	}
}

func TestPipeline_WalkForward_CursorAdvances(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(30, start, flatPriceAt(100, 1000000))

	config := RulesEngineConfig{
		Version: 1,
		Entry: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(20)}}, // unlikely to trigger
		},
		Exit: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(80)}}, // unlikely to trigger
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	// Start at the beginning, execute multiple times to see cursor advance.
	indicatorNode := NewIndicatorAnalystNode(bars, time.Time{}, nil)
	traderNode := NewRulesTraderNode(config, 100000, nil, nil)

	// Execute 5 times manually, verify market data grows each time.
	for i := 0; i < 5; i++ {
		state := &agent.PipelineState{Ticker: "TEST"}
		if err := indicatorNode.Execute(context.Background(), state); err != nil {
			t.Fatalf("indicator bar %d: %v", i, err)
		}
		if len(state.Market.Bars) != i+1 {
			t.Errorf("bar %d: got %d bars, want %d", i, len(state.Market.Bars), i+1)
		}
		if err := traderNode.Execute(context.Background(), state); err != nil {
			t.Fatalf("trader bar %d: %v", i, err)
		}
	}
}

func TestPipeline_WalkForward_CursorWraps(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(5, start, flatPriceAt(100, 1000000))

	node := NewIndicatorAnalystNode(bars, time.Time{}, nil)

	// Exhaust all 5 bars.
	for i := 0; i < 5; i++ {
		state := &agent.PipelineState{Ticker: "TEST"}
		if err := node.Execute(context.Background(), state); err != nil {
			t.Fatalf("bar %d: %v", i, err)
		}
	}

	// Next call should wrap back to start cursor (bar 0).
	state := &agent.PipelineState{Ticker: "TEST"}
	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("wrap: %v", err)
	}
	if len(state.Market.Bars) != 1 {
		t.Errorf("after wrap: got %d bars, want 1", len(state.Market.Bars))
	}
}

func TestPipeline_WalkForward_ResetRestoresCursor(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(10, start, flatPriceAt(100, 1000000))

	node := NewIndicatorAnalystNode(bars, time.Time{}, nil)

	// Advance cursor 5 bars.
	for i := 0; i < 5; i++ {
		state := &agent.PipelineState{Ticker: "TEST"}
		_ = node.Execute(context.Background(), state)
	}

	// Reset should set cursor to 0.
	node.Reset()

	state := &agent.PipelineState{Ticker: "TEST"}
	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("after reset: %v", err)
	}
	if len(state.Market.Bars) != 1 {
		t.Errorf("after reset: got %d bars, want 1", len(state.Market.Bars))
	}
}

func TestPipeline_JournalIntegration_BuyThenSell(t *testing.T) {
	t.Parallel()
	journal := NewTradeJournal()

	config := RulesEngineConfig{
		Version: 1,
		Entry: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(30)}},
		},
		Exit: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(70)}},
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	node := NewRulesTraderNode(config, 100000, journal, nil)

	// Step 1: RSI=25 (below 30) -> buy signal.
	buyState := &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars:       []domain.OHLCV{{Close: 150, Open: 148, High: 152, Low: 147, Volume: 200000}},
			Indicators: []domain.Indicator{{Name: "rsi_14", Value: 25}, {Name: "atr_14", Value: 3}},
		},
	}
	if err := node.Execute(context.Background(), buyState); err != nil {
		t.Fatalf("buy step: %v", err)
	}
	if buyState.TradingPlan.Action != domain.PipelineSignalBuy {
		t.Fatalf("expected buy, got %q", buyState.TradingPlan.Action)
	}

	// Simulate that the order was filled: open a journal position.
	journal.OpenNewPosition(OpenPosition{
		Ticker:     "AAPL",
		Side:       domain.PositionSideLong,
		EntryPrice: 150,
		Quantity:   buyState.TradingPlan.PositionSize,
		EntryDate:  time.Now(),
	})

	if !journal.IsHolding("AAPL") {
		t.Fatal("expected journal to show holding AAPL")
	}

	// Step 2: RSI=75 (above 70) -> sell signal (because journal.IsHolding is true).
	sellState := &agent.PipelineState{
		Ticker: "AAPL",
		Market: &agent.MarketData{
			Bars:       []domain.OHLCV{{Close: 160, Open: 158, High: 162, Low: 157, Volume: 200000}},
			Indicators: []domain.Indicator{{Name: "rsi_14", Value: 75}, {Name: "atr_14", Value: 3}},
		},
	}
	if err := node.Execute(context.Background(), sellState); err != nil {
		t.Fatalf("sell step: %v", err)
	}
	if sellState.TradingPlan.Action != domain.PipelineSignalSell {
		t.Fatalf("expected sell, got %q", sellState.TradingPlan.Action)
	}

	// Close the position in journal and verify P&L.
	closed := journal.ClosePosition("AAPL", 160, time.Now(), "exit_signal")
	if closed == nil {
		t.Fatal("expected closed position")
	}
	if closed.RealizedPnL <= 0 {
		t.Errorf("expected positive P&L, got %v", closed.RealizedPnL)
	}
	if journal.IsHolding("AAPL") {
		t.Error("expected journal to no longer hold AAPL")
	}
}

func TestPipeline_FilterRejection_HoldsEvenWhenEntryMet(t *testing.T) {
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
		Filters:        &FilterConfig{MinVolume: 500000}, // Volume filter will reject
	}

	node := NewRulesTraderNode(config, 100000, nil, nil)
	state := &agent.PipelineState{
		Ticker: "LOW_VOL",
		Market: &agent.MarketData{
			Bars: []domain.OHLCV{{Close: 150, Open: 148, High: 152, Low: 147, Volume: 100000}}, // Below MinVolume
			Indicators: []domain.Indicator{
				{Name: "rsi_14", Value: 25}, // Entry condition met
				{Name: "atr_14", Value: 3},
			},
		},
	}

	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.TradingPlan.Action != domain.PipelineSignalHold {
		t.Errorf("action = %q, want hold (filter rejection)", state.TradingPlan.Action)
	}
}

func TestPipeline_NoMarketData_Holds(t *testing.T) {
	t.Parallel()
	config := RulesEngineConfig{
		Version:        1,
		Entry:          ConditionGroup{Operator: "AND", Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(30)}}},
		Exit:           ConditionGroup{Operator: "AND", Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(70)}}},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	node := NewRulesTraderNode(config, 100000, nil, nil)
	state := &agent.PipelineState{Ticker: "EMPTY", Market: nil}

	if err := node.Execute(context.Background(), state); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if state.TradingPlan.Action != domain.PipelineSignalHold {
		t.Errorf("action = %q, want hold (no market data)", state.TradingPlan.Action)
	}
}

func TestPipeline_SkipsResearchAndRiskPhases(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(50, start, flatPriceAt(100, 1000000))

	pipeline := NewRulesPipeline(
		RulesEngineConfig{
			Version:        1,
			Entry:          ConditionGroup{Operator: "AND", Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(20)}}},
			Exit:           ConditionGroup{Operator: "AND", Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(80)}}},
			PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
			StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
			TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
		},
		bars, time.Time{}, 100000,
		agent.NoopPersister{}, nil, nil,
	)

	// Verify SkipPhases is configured correctly.
	cfg := pipeline.Config()
	if !cfg.SkipPhases[agent.PhaseResearchDebate] {
		t.Error("expected research debate phase to be skipped")
	}
	if !cfg.SkipPhases[agent.PhaseRiskDebate] {
		t.Error("expected risk debate phase to be skipped")
	}

	// Verify registered nodes.
	nodes := pipeline.Nodes()
	if len(nodes[agent.PhaseAnalysis]) != 1 {
		t.Errorf("analysis nodes = %d, want 1", len(nodes[agent.PhaseAnalysis]))
	}
	if len(nodes[agent.PhaseTrading]) != 1 {
		t.Errorf("trading nodes = %d, want 1", len(nodes[agent.PhaseTrading]))
	}
	if len(nodes[agent.PhaseResearchDebate]) != 0 {
		t.Errorf("research debate nodes = %d, want 0", len(nodes[agent.PhaseResearchDebate]))
	}
	if len(nodes[agent.PhaseRiskDebate]) != 0 {
		t.Errorf("risk debate nodes = %d, want 0", len(nodes[agent.PhaseRiskDebate]))
	}
}

func TestPipeline_MultipleExecutions_CursorProgresses(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// 30 bars, all flat -> hold signal each time, but cursor should advance.
	bars := generateBars(30, start, flatPriceAt(100, 1000000))

	config := RulesEngineConfig{
		Version:        1,
		Entry:          ConditionGroup{Operator: "AND", Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(10)}}},
		Exit:           ConditionGroup{Operator: "AND", Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(90)}}},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2},
	}

	pipeline := NewRulesPipeline(config, bars, time.Time{}, 100000, agent.NoopPersister{}, nil, nil)

	// Execute 10 times — should succeed each time without error.
	for i := 0; i < 10; i++ {
		state, err := pipeline.Execute(context.Background(), domain.Strategy{Ticker: "MULTI"}.ID, "MULTI")
		if err != nil {
			t.Fatalf("execution %d: %v", i, err)
		}
		if state == nil {
			t.Fatalf("execution %d: state is nil", i)
		}
		if state.TradingPlan.Action != domain.PipelineSignalHold {
			t.Errorf("execution %d: action = %q, want hold", i, state.TradingPlan.Action)
		}
	}
}

func TestPipeline_ContextCancellation(t *testing.T) {
	t.Parallel()
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	bars := generateBars(50, start, flatPriceAt(100, 1000000))

	pipeline := NewRulesPipeline(smaCrossConfig(), bars, time.Time{}, 100000, agent.NoopPersister{}, nil, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	// Rules pipeline nodes are pure computation and don't check context,
	// so a pre-cancelled context still completes successfully. This test
	// verifies the pipeline doesn't panic on a cancelled context.
	state, err := pipeline.Execute(ctx, domain.Strategy{Ticker: "CANCEL"}.ID, "CANCEL")
	if err != nil {
		// Context cancellation may propagate through errgroup — either outcome is acceptable.
		return
	}
	if state == nil {
		t.Error("expected non-nil state")
	}
}
