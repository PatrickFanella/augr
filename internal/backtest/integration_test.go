package backtest

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ---------------------------------------------------------------------------
// Stub pipeline nodes
// ---------------------------------------------------------------------------

// stubNode is a no-op pipeline node that satisfies a specific phase/role.
type stubNode struct {
	name  string
	role  agent.AgentRole
	phase agent.Phase
}

func (s *stubNode) Name() string                                              { return s.name }
func (s *stubNode) Role() agent.AgentRole                                     { return s.role }
func (s *stubNode) Phase() agent.Phase                                        { return s.phase }
func (s *stubNode) Execute(_ context.Context, state *agent.PipelineState) error { return nil }

// orderAction describes a deterministic order to place on a specific bar.
type orderAction struct {
	barIndex int              // 1-based bar index
	side     domain.OrderSide
	quantity float64
}

// integrationTradingNode is a pipeline trading node that submits orders to the
// backtest broker and applies the resulting trades to the position tracker. It
// uses deterministic actions keyed on bar index so the test is reproducible.
type integrationTradingNode struct {
	broker  *BrokerAdapter
	tracker *PositionTracker
	bars    []domain.OHLCV // sorted bars passed to the runner
	actions []orderAction
	barIdx  int
}

func (n *integrationTradingNode) Name() string          { return "integration-trader" }
func (n *integrationTradingNode) Role() agent.AgentRole { return agent.AgentRoleTrader }
func (n *integrationTradingNode) Phase() agent.Phase    { return agent.PhaseTrading }

func (n *integrationTradingNode) Execute(ctx context.Context, state *agent.PipelineState) error {
	n.barIdx++
	for _, action := range n.actions {
		if action.barIndex != n.barIdx {
			continue
		}

		order := &domain.Order{
			Ticker:    state.Ticker,
			Side:      action.side,
			OrderType: domain.OrderTypeMarket,
			Quantity:  action.quantity,
		}

		_, err := n.broker.SubmitOrder(ctx, order)
		if err != nil {
			return err
		}

		// Apply the trade to the position tracker so that the equity curve
		// reflects actual trading activity. With zero slippage and no spread
		// the fill price equals the current bar close.
		bar := n.bars[n.barIdx-1]
		trade := domain.Trade{
			Ticker:     state.Ticker,
			Side:       action.side,
			Quantity:   action.quantity,
			Price:      bar.Close,
			Fee:        0,
			ExecutedAt: bar.Timestamp,
		}
		if err := n.tracker.ApplyTrade(trade); err != nil {
			return err
		}
	}
	return nil
}

// makeIntegrationPipeline builds a Pipeline with stub nodes for every required
// phase plus a custom trading node that drives order submission.
func makeIntegrationPipeline(trader *integrationTradingNode) *agent.Pipeline {
	events := make(chan agent.PipelineEvent, 128)
	p := agent.NewPipeline(
		agent.PipelineConfig{
			ResearchDebateRounds: 1,
			RiskDebateRounds:     1,
		},
		agent.NoopPersister{},
		events,
		nil,
	)

	// Research debate stubs.
	p.RegisterNode(&stubNode{"bull", agent.AgentRoleBullResearcher, agent.PhaseResearchDebate})
	p.RegisterNode(&stubNode{"bear", agent.AgentRoleBearResearcher, agent.PhaseResearchDebate})
	p.RegisterNode(&stubNode{"judge", agent.AgentRoleInvestJudge, agent.PhaseResearchDebate})

	// Trading node.
	p.RegisterNode(trader)

	// Risk debate stubs.
	p.RegisterNode(&stubNode{"aggressive", agent.AgentRoleAggressiveAnalyst, agent.PhaseRiskDebate})
	p.RegisterNode(&stubNode{"conservative", agent.AgentRoleConservativeAnalyst, agent.PhaseRiskDebate})
	p.RegisterNode(&stubNode{"neutral", agent.AgentRoleNeutralAnalyst, agent.PhaseRiskDebate})
	p.RegisterNode(&stubNode{"risk-mgr", agent.AgentRoleRiskManager, agent.PhaseRiskDebate})

	return p
}

// ---------------------------------------------------------------------------
// End-to-end integration test
// ---------------------------------------------------------------------------

// TestBacktestIntegrationEndToEnd validates the complete backtest flow:
//
//  1. Historical data loading (deterministic bars)
//  2. Pipeline execution (all phases)
//  3. Simulated order fills (buy + sell via BrokerAdapter + FillEngine)
//  4. Position tracking and equity curve recording
//  5. Metric computation (validated against hand-computed values)
//  6. Trade analytics computation
//  7. Report generation and JSON serialisation
func TestBacktestIntegrationEndToEnd(t *testing.T) {
	t.Parallel()

	// ---- deterministic test data ----
	//
	// 5 daily bars for ticker "TEST":
	//   Bar 1 (2026-01-05)  Close = 100
	//   Bar 2 (2026-01-06)  Close = 105
	//   Bar 3 (2026-01-07)  Close = 103
	//   Bar 4 (2026-01-08)  Close = 108
	//   Bar 5 (2026-01-09)  Close = 110
	//
	// Actions:
	//   Bar 2: Buy  10 shares at close (105)
	//   Bar 4: Sell 10 shares at close (108)
	//
	// Initial cash: $100,000
	// Slippage: 0, Spread: none, Transaction costs: none
	//
	// Hand-computed equity curve:
	//   Bar 1: Cash=100000  MV=0     Equity=100000
	//   Bar 2: Cash=98950   MV=1050  Equity=100000  (bought 10@105)
	//   Bar 3: Cash=98950   MV=1030  Equity=99980   (mark 10@103)
	//   Bar 4: Cash=100030  MV=0     Equity=100030  (sold 10@108, realized PnL=30)
	//   Bar 5: Cash=100030  MV=0     Equity=100030

	base := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
		makeBar(base.Add(48*time.Hour), 103),
		makeBar(base.Add(72*time.Hour), 108),
		makeBar(base.Add(96*time.Hour), 110),
	}

	const (
		initialCash = 100_000.0
		buyQty      = 10.0
		buyPrice    = 105.0
		sellPrice   = 108.0
	)

	strategyID := uuid.New()
	ticker := "TEST"

	// ---- create components ----

	fillEngine, err := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 0},
	})
	if err != nil {
		t.Fatalf("NewFillEngine: %v", err)
	}

	broker, err := NewBrokerAdapter(initialCash, fillEngine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter: %v", err)
	}

	tracker, err := NewPositionTracker(initialCash)
	if err != nil {
		t.Fatalf("NewPositionTracker: %v", err)
	}

	tradingNode := &integrationTradingNode{
		broker:  broker,
		tracker: tracker,
		bars:    bars,
		actions: []orderAction{
			{barIndex: 2, side: domain.OrderSideBuy, quantity: buyQty},
			{barIndex: 4, side: domain.OrderSideSell, quantity: buyQty},
		},
	}

	pipeline := makeIntegrationPipeline(tradingNode)

	runner, err := NewRunner(
		RunnerConfig{StrategyID: strategyID, Ticker: ticker},
		bars,
		pipeline,
		broker,
		tracker,
		nil, // logger
	)
	if err != nil {
		t.Fatalf("NewRunner: %v", err)
	}

	// ---- execute ----

	runResult, err := runner.Run(context.Background())
	if err != nil {
		t.Fatalf("Runner.Run: %v", err)
	}

	// ---- validate bar results ----

	if got := len(runResult.BarResults); got != 5 {
		t.Fatalf("BarResults count = %d, want 5", got)
	}
	for i, br := range runResult.BarResults {
		if br.Err != nil {
			t.Errorf("BarResults[%d] unexpected pipeline error: %v", i, br.Err)
		}
		if !br.Bar.Timestamp.Equal(bars[i].Timestamp) {
			t.Errorf("BarResults[%d].Timestamp = %v, want %v", i, br.Bar.Timestamp, bars[i].Timestamp)
		}
	}

	// ---- validate equity curve ----

	curve := runResult.EquityCurve
	if got := len(curve); got != 5 {
		t.Fatalf("EquityCurve length = %d, want 5", got)
	}

	wantEquity := []float64{initialCash, initialCash, 99_980, 100_030, 100_030}
	for i, want := range wantEquity {
		assertFloatEqual(t, curve[i].Equity, want, "EquityCurve[%d].Equity")
	}

	// Cash expectations.
	wantCash := []float64{initialCash, 98_950, 98_950, 100_030, 100_030}
	for i, want := range wantCash {
		assertFloatEqual(t, curve[i].Cash, want, "EquityCurve[%d].Cash")
	}

	// ---- validate trades from broker ----

	trades := broker.FilledTrades()
	if got := len(trades); got != 2 {
		t.Fatalf("FilledTrades count = %d, want 2", got)
	}

	// First trade: buy.
	if trades[0].Side != domain.OrderSideBuy {
		t.Errorf("trade[0].Side = %q, want %q", trades[0].Side, domain.OrderSideBuy)
	}
	assertFloatEqual(t, trades[0].Quantity, buyQty, "trade[0].Quantity")
	assertFloatEqual(t, trades[0].Price, buyPrice, "trade[0].Price")

	// Second trade: sell.
	if trades[1].Side != domain.OrderSideSell {
		t.Errorf("trade[1].Side = %q, want %q", trades[1].Side, domain.OrderSideSell)
	}
	assertFloatEqual(t, trades[1].Quantity, buyQty, "trade[1].Quantity")
	assertFloatEqual(t, trades[1].Price, sellPrice, "trade[1].Price")

	// ---- compute and validate metrics ----

	metrics := ComputeMetrics(curve, bars)

	if metrics.TotalBars != 5 {
		t.Errorf("Metrics.TotalBars = %d, want 5", metrics.TotalBars)
	}
	assertFloatEqual(t, metrics.StartEquity, initialCash, "Metrics.StartEquity")
	assertFloatEqual(t, metrics.EndEquity, 100_030, "Metrics.EndEquity")

	// TotalReturn = (100030 − 100000) / 100000 = 0.0003
	wantTotalReturn := 30.0 / initialCash
	assertFloatEqual(t, metrics.TotalReturn, wantTotalReturn, "Metrics.TotalReturn")

	// MaxDrawdown: peak 100000, trough 99980 → (100000−99980)/100000 = 0.0002
	wantMaxDD := 20.0 / initialCash
	assertFloatEqual(t, metrics.MaxDrawdown, wantMaxDD, "Metrics.MaxDrawdown")

	// BuyAndHoldReturn: (110 − 100) / 100 = 0.10
	assertFloatEqual(t, metrics.BuyAndHoldReturn, 0.10, "Metrics.BuyAndHoldReturn")

	// Verify time range.
	if !metrics.StartTime.Equal(base) {
		t.Errorf("Metrics.StartTime = %v, want %v", metrics.StartTime, base)
	}
	wantEndTime := base.Add(96 * time.Hour)
	if !metrics.EndTime.Equal(wantEndTime) {
		t.Errorf("Metrics.EndTime = %v, want %v", metrics.EndTime, wantEndTime)
	}

	// ---- compute and validate trade analytics ----

	analytics := ComputeTradeAnalytics(trades, base, base.Add(96*time.Hour))

	// We have one round-trip (buy then sell) = 1 closed trade.
	if analytics.ClosedTrades != 1 {
		t.Errorf("TradeAnalytics.ClosedTrades = %d, want 1", analytics.ClosedTrades)
	}
	// The single trade was profitable (bought at 105, sold at 108 → PnL = $30).
	if analytics.MaxConsecutiveWins != 1 {
		t.Errorf("TradeAnalytics.MaxConsecutiveWins = %d, want 1", analytics.MaxConsecutiveWins)
	}
	if analytics.MaxConsecutiveLosses != 0 {
		t.Errorf("TradeAnalytics.MaxConsecutiveLosses = %d, want 0", analytics.MaxConsecutiveLosses)
	}
	// Largest single win = PnL of the trade = (108−105)*10 = 30.
	assertFloatEqual(t, analytics.LargestSingleWin, 30.0, "TradeAnalytics.LargestSingleWin")

	// ---- generate and validate report ----

	cfg := OrchestratorConfig{
		StrategyID:    strategyID,
		Ticker:        ticker,
		StartDate:     base,
		EndDate:       base.Add(96 * time.Hour),
		InitialCash:   initialCash,
		PromptVersion: "test-v1",
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	orchResult := &OrchestratorResult{
		Trades:            trades,
		BarResults:        runResult.BarResults,
		Positions:         tracker.Positions(),
		EquityCurve:       curve,
		EquityCurveReport: runResult.EquityCurveReport,
		Metrics:           metrics,
		TradeAnalytics:    analytics,
		PromptVersion:     "test-v1",
		PromptVersionHash: "abc123",
	}

	report, err := GenerateBacktestReport(cfg, orchResult)
	if err != nil {
		t.Fatalf("GenerateBacktestReport: %v", err)
	}

	// Strategy configuration.
	if report.StrategyConfiguration.Ticker != ticker {
		t.Errorf("Report.Ticker = %q, want %q", report.StrategyConfiguration.Ticker, ticker)
	}
	assertFloatEqual(t, report.StrategyConfiguration.InitialCash, initialCash, "Report.InitialCash")

	// Date range.
	if !report.DateRange.Start.Equal(base) {
		t.Errorf("Report.DateRange.Start = %v, want %v", report.DateRange.Start, base)
	}

	// Performance metrics.
	assertFloatEqual(t, report.PerformanceMetrics.TotalReturn, wantTotalReturn, "Report.TotalReturn")
	assertFloatEqual(t, report.PerformanceMetrics.MaxDrawdown, wantMaxDD, "Report.MaxDrawdown")
	assertFloatEqual(t, report.PerformanceMetrics.BuyAndHoldReturn, 0.10, "Report.BuyAndHoldReturn")

	// Benchmark comparison.
	assertFloatEqual(t, report.BenchmarkComparison.BuyAndHoldReturn, 0.10, "Report.Benchmark.BuyAndHold")

	// Trade log.
	if got := len(report.TradeLog); got != 2 {
		t.Errorf("Report.TradeLog length = %d, want 2", got)
	}

	// Trade analytics in report.
	if report.TradeAnalytics.ClosedTrades != 1 {
		t.Errorf("Report.TradeAnalytics.ClosedTrades = %d, want 1", report.TradeAnalytics.ClosedTrades)
	}

	// Equity curve report points.
	if got := len(report.EquityCurve.Points); got != 5 {
		t.Errorf("Report.EquityCurve.Points length = %d, want 5", got)
	}

	// Prompt versioning in report.
	if report.StrategyConfiguration.PromptVersion != "test-v1" {
		t.Errorf("Report.PromptVersion = %q, want %q", report.StrategyConfiguration.PromptVersion, "test-v1")
	}
	if report.StrategyConfiguration.PromptVersionHash != "abc123" {
		t.Errorf("Report.PromptVersionHash = %q, want %q", report.StrategyConfiguration.PromptVersionHash, "abc123")
	}

	// ---- validate JSON serialisation ----

	data, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report): %v", err)
	}

	// Unmarshal into a generic map to verify the JSON is valid and contains
	// expected top-level keys. A full struct round-trip is not possible here
	// because HoldingPeriodStats serialises durations as human-readable
	// strings via JSONDuration, which has no matching UnmarshalJSON.
	var decoded map[string]json.RawMessage
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal(report): %v", err)
	}

	for _, key := range []string{
		"strategy_configuration",
		"date_range",
		"performance_metrics",
		"trade_analytics",
		"benchmark_comparison",
		"equity_curve",
		"trade_log",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("report JSON missing key %q", key)
		}
	}

	// Verify the strategy configuration round-trips correctly.
	var stratCfg ReportStrategyConfiguration
	if err := json.Unmarshal(decoded["strategy_configuration"], &stratCfg); err != nil {
		t.Fatalf("unmarshal strategy_configuration: %v", err)
	}
	if stratCfg.Ticker != ticker {
		t.Errorf("round-trip Ticker = %q, want %q", stratCfg.Ticker, ticker)
	}

	// Verify trade log round-trips correctly.
	var tradeLog []json.RawMessage
	if err := json.Unmarshal(decoded["trade_log"], &tradeLog); err != nil {
		t.Fatalf("unmarshal trade_log: %v", err)
	}
	if got := len(tradeLog); got != 2 {
		t.Errorf("round-trip trade_log length = %d, want 2", got)
	}
}
