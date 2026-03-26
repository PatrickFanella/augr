package backtest

import (
	"encoding/json"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestGenerateBacktestReportIncludesStructuredSummary(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 1, 9, 30, 0, 0, time.UTC)
	end := start.Add(48 * time.Hour)
	strategyID := uuid.New()

	cfg := OrchestratorConfig{
		StrategyID:  strategyID,
		Ticker:      "AAPL",
		StartDate:   start,
		EndDate:     end,
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 12},
			Spread:   FixedSpread{SpreadBps: 20},
			Costs: TransactionCosts{
				CommissionPerOrder: 1.25,
				CommissionPerUnit:  0.01,
				ExchangeFeePct:     0.001,
			},
			MaxVolumePct: 0.25,
		},
		PromptVersion: "baseline",
	}

	trade := domain.Trade{
		ID:         uuid.New(),
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		Quantity:   5,
		Price:      101,
		Fee:        1.50,
		ExecutedAt: start.Add(2 * time.Hour),
	}
	curve := []EquityPoint{
		{Timestamp: start, Cash: 100_000, Equity: 100_000},
		{Timestamp: start.Add(24 * time.Hour), Cash: 100_250, Equity: 100_250, RealizedPnL: 250, TotalPnL: 250},
	}

	result := &OrchestratorResult{
		Trades:      []domain.Trade{trade},
		EquityCurve: curve,
		Metrics: Metrics{
			TotalReturn:      0.0025,
			BuyAndHoldReturn: 0.001,
			MaxDrawdown:      0.01,
			CalmarRatio:      1.2,
			SharpeRatio:      1.5,
			SortinoRatio:     1.7,
			Alpha:            0.03,
			Beta:             1.1,
			InformationRatio: 0.8,
			WinRate:          1,
			ProfitFactor:     2.5,
			AvgWinLossRatio:  1.9,
			Volatility:       0.12,
			StartEquity:      100_000,
			EndEquity:        100_250,
			StartTime:        start,
			EndTime:          end,
			TotalBars:        2,
			RealizedPnL:      250,
		},
		TradeAnalytics: TradeAnalytics{
			HoldingPeriods: HoldingPeriodStats{
				Min:    24 * time.Hour,
				Max:    24 * time.Hour,
				Mean:   24 * time.Hour,
				Median: 24 * time.Hour,
			},
			ClosedTrades:         1,
			TradeFrequencyPerDay: 0.5,
			LargestSingleWin:     250,
			MaxConsecutiveWins:   1,
		},
		PromptVersion:     "prompt-v2",
		PromptVersionHash: "hash-123",
	}

	report, err := GenerateBacktestReport(cfg, result)
	if err != nil {
		t.Fatalf("GenerateBacktestReport() error = %v", err)
	}

	if report.StrategyConfiguration.StrategyID != strategyID {
		t.Fatalf("StrategyConfiguration.StrategyID = %v, want %v", report.StrategyConfiguration.StrategyID, strategyID)
	}
	if report.StrategyConfiguration.PromptVersion != "prompt-v2" {
		t.Fatalf("StrategyConfiguration.PromptVersion = %q, want %q", report.StrategyConfiguration.PromptVersion, "prompt-v2")
	}
	if len(report.TradeLog) != 1 {
		t.Fatalf("len(TradeLog) = %d, want 1", len(report.TradeLog))
	}
	if len(report.EquityCurve.Points) != len(curve) {
		t.Fatalf("len(EquityCurve.Points) = %d, want %d", len(report.EquityCurve.Points), len(curve))
	}

	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	strategyConfig := got["strategy_configuration"].(map[string]any)
	if strategyConfig["ticker"] != "AAPL" {
		t.Fatalf("strategy_configuration.ticker = %v, want %q", strategyConfig["ticker"], "AAPL")
	}
	fillConfig := strategyConfig["fill_configuration"].(map[string]any)
	slippage := fillConfig["slippage"].(map[string]any)
	if slippage["type"] != "proportional" {
		t.Fatalf("fill_configuration.slippage.type = %v, want %q", slippage["type"], "proportional")
	}
	if slippage["basis_points"] != 12.0 {
		t.Fatalf("fill_configuration.slippage.basis_points = %v, want %v", slippage["basis_points"], 12.0)
	}

	dateRange := got["date_range"].(map[string]any)
	if dateRange["start"] != "2026-03-01T09:30:00Z" {
		t.Fatalf("date_range.start = %v, want %q", dateRange["start"], "2026-03-01T09:30:00Z")
	}

	performanceMetrics := got["performance_metrics"].(map[string]any)
	if performanceMetrics["alpha"] != 0.03 {
		t.Fatalf("performance_metrics.alpha = %v, want %v", performanceMetrics["alpha"], 0.03)
	}
	if performanceMetrics["total_bars"] != 2.0 {
		t.Fatalf("performance_metrics.total_bars = %v, want %v", performanceMetrics["total_bars"], 2.0)
	}

	tradeAnalytics := got["trade_analytics"].(map[string]any)
	holdingPeriods := tradeAnalytics["holding_periods"].(map[string]any)
	if holdingPeriods["mean"] != "24h0m0s" {
		t.Fatalf("trade_analytics.holding_periods.mean = %v, want %q", holdingPeriods["mean"], "24h0m0s")
	}

	benchmarkComparison := got["benchmark_comparison"].(map[string]any)
	if benchmarkComparison["buy_and_hold_return"] != 0.001 {
		t.Fatalf("benchmark_comparison.buy_and_hold_return = %v, want %v", benchmarkComparison["buy_and_hold_return"], 0.001)
	}

	equityCurve := got["equity_curve"].(map[string]any)
	if len(equityCurve["points"].([]any)) != 2 {
		t.Fatalf("len(equity_curve.points) = %d, want 2", len(equityCurve["points"].([]any)))
	}

	if len(got["trade_log"].([]any)) != 1 {
		t.Fatalf("len(trade_log) = %d, want 1", len(got["trade_log"].([]any)))
	}
}

func TestGenerateBacktestReportJSONHandlesInfiniteMetrics(t *testing.T) {
	t.Parallel()

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 5},
		},
	}

	report, err := GenerateBacktestReport(cfg, &OrchestratorResult{
		Metrics: Metrics{
			ProfitFactor:    math.Inf(1),
			AvgWinLossRatio: math.Inf(1),
		},
	})
	if err != nil {
		t.Fatalf("GenerateBacktestReport() error = %v", err)
	}

	payload, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("json.Marshal(report) error = %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("json.Unmarshal(payload) error = %v", err)
	}

	performanceMetrics := got["performance_metrics"].(map[string]any)
	if performanceMetrics["profit_factor"] != "Infinity" {
		t.Fatalf("performance_metrics.profit_factor = %v, want %q", performanceMetrics["profit_factor"], "Infinity")
	}
	if performanceMetrics["avg_win_loss_ratio"] != "Infinity" {
		t.Fatalf("performance_metrics.avg_win_loss_ratio = %v, want %q", performanceMetrics["avg_win_loss_ratio"], "Infinity")
	}
}
