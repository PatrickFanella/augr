package strategyscaffold_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/strategyscaffold"
)

func TestStockPaperMovingAverageCrossoverProducesValidPaperStrategy(t *testing.T) {
	strategy, err := strategyscaffold.StockPaperMovingAverageCrossover(" spy ")
	if err != nil {
		t.Fatalf("StockPaperMovingAverageCrossover() error = %v", err)
	}
	if err := strategy.Validate(); err != nil {
		t.Fatalf("strategy.Validate() error = %v", err)
	}
	if strategy.Ticker != "SPY" {
		t.Fatalf("Ticker = %q, want %q", strategy.Ticker, "SPY")
	}
	if strategy.MarketType != domain.MarketTypeStock {
		t.Fatalf("MarketType = %q, want %q", strategy.MarketType, domain.MarketTypeStock)
	}
	if !strategy.IsPaper {
		t.Fatal("IsPaper = false, want true")
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(strategy.Config, &cfg); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}
	parsed, err := rules.Parse(cfg["rules_engine"])
	if err != nil {
		t.Fatalf("rules.Parse(rules_engine) error = %v", err)
	}
	if parsed == nil {
		t.Fatal("rules.Parse(rules_engine) = nil, want config")
	}
	if parsed.PositionSizing.Method != "fixed_fraction" {
		t.Fatalf("PositionSizing.Method = %q, want %q", parsed.PositionSizing.Method, "fixed_fraction")
	}
	if parsed.StopLoss.Method != "atr_multiple" {
		t.Fatalf("StopLoss.Method = %q, want %q", parsed.StopLoss.Method, "atr_multiple")
	}
}

func TestStockPaperBacktestConfigProducesValidConfig(t *testing.T) {
	strategy, err := strategyscaffold.StockPaperMovingAverageCrossover("SPY")
	if err != nil {
		t.Fatalf("StockPaperMovingAverageCrossover() error = %v", err)
	}
	cfg, err := strategyscaffold.StockPaperBacktestConfig(
		strategy,
		time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 12, 31, 0, 0, 0, 0, time.UTC),
		100_000,
	)
	if err != nil {
		t.Fatalf("StockPaperBacktestConfig() error = %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("cfg.Validate() error = %v", err)
	}
	if cfg.StrategyID != strategy.ID {
		t.Fatalf("StrategyID = %s, want %s", cfg.StrategyID, strategy.ID)
	}
	if cfg.Simulation.InitialCapital != 100_000 {
		t.Fatalf("InitialCapital = %v, want 100000", cfg.Simulation.InitialCapital)
	}
}

func TestOptionsPaperBullPutSpreadProducesValidPaperStrategy(t *testing.T) {
	strategy, err := strategyscaffold.OptionsPaperBullPutSpread(" qqq ")
	if err != nil {
		t.Fatalf("OptionsPaperBullPutSpread() error = %v", err)
	}
	if err := strategy.Validate(); err != nil {
		t.Fatalf("strategy.Validate() error = %v", err)
	}
	if strategy.Ticker != "QQQ" {
		t.Fatalf("Ticker = %q, want %q", strategy.Ticker, "QQQ")
	}
	if strategy.MarketType != domain.MarketTypeOptions {
		t.Fatalf("MarketType = %q, want %q", strategy.MarketType, domain.MarketTypeOptions)
	}
	if !strategy.IsPaper {
		t.Fatal("IsPaper = false, want true")
	}

	var cfg map[string]json.RawMessage
	if err := json.Unmarshal(strategy.Config, &cfg); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}
	parsed, err := rules.ParseOptions(cfg["options_rules"])
	if err != nil {
		t.Fatalf("rules.ParseOptions(options_rules) error = %v", err)
	}
	if parsed == nil {
		t.Fatal("rules.ParseOptions(options_rules) = nil, want config")
	}
	if parsed.StrategyType != domain.StrategyBullPutSpread {
		t.Fatalf("StrategyType = %q, want %q", parsed.StrategyType, domain.StrategyBullPutSpread)
	}
	if parsed.PositionSizing.Method != "max_risk" {
		t.Fatalf("PositionSizing.Method = %q, want %q", parsed.PositionSizing.Method, "max_risk")
	}
	if parsed.Management.CloseAtProfitPct != 50 {
		t.Fatalf("CloseAtProfitPct = %v, want 50", parsed.Management.CloseAtProfitPct)
	}
}

func TestRunOptionsPaperBacktestReturnsSummary(t *testing.T) {
	bars := syntheticBars(340)
	startDate := bars[40].Timestamp
	endDate := bars[len(bars)-1].Timestamp

	summary, err := strategyscaffold.RunOptionsPaperBacktest(context.Background(), "QQQ", bars, startDate, endDate, 100_000, nil)
	if err != nil {
		t.Fatalf("RunOptionsPaperBacktest() error = %v", err)
	}
	if summary == nil {
		t.Fatal("RunOptionsPaperBacktest() summary = nil")
	}
	if summary.Strategy.MarketType != domain.MarketTypeOptions {
		t.Fatalf("Strategy.MarketType = %q, want %q", summary.Strategy.MarketType, domain.MarketTypeOptions)
	}
	if summary.Validation == nil {
		t.Fatal("Validation = nil, want result")
	}
	if summary.Metrics.TotalBars == 0 {
		t.Fatal("Metrics.TotalBars = 0, want non-zero")
	}
	if len(summary.Trades) == 0 {
		t.Fatal("Trades = 0, want non-zero")
	}
	if len(summary.EquityCurve) == 0 {
		t.Fatal("EquityCurve = 0, want non-zero")
	}
}

func TestRunOptionsPaperBacktestWithConfigUsesProvidedRules(t *testing.T) {
	bars := syntheticBars(340)
	startDate := bars[40].Timestamp
	endDate := bars[len(bars)-1].Timestamp

	base, err := strategyscaffold.OptionsPaperBullPutSpread("QQQ")
	if err != nil {
		t.Fatalf("OptionsPaperBullPutSpread() error = %v", err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(base.Config, &payload); err != nil {
		t.Fatalf("json.Unmarshal(config) error = %v", err)
	}
	cfg, err := rules.ParseOptions(payload["options_rules"])
	if err != nil {
		t.Fatalf("rules.ParseOptions(options_rules) error = %v", err)
	}
	cfg.Underlying = "SPY"
	cfg.Management.CloseAtProfitPct = 65

	summary, err := strategyscaffold.RunOptionsPaperBacktestWithConfig(context.Background(), *cfg, bars, startDate, endDate, 100_000, nil)
	if err != nil {
		t.Fatalf("RunOptionsPaperBacktestWithConfig() error = %v", err)
	}
	if summary.Strategy.Ticker != "SPY" {
		t.Fatalf("Strategy.Ticker = %q, want %q", summary.Strategy.Ticker, "SPY")
	}
	var strategyPayload map[string]json.RawMessage
	if err := json.Unmarshal(summary.Strategy.Config, &strategyPayload); err != nil {
		t.Fatalf("json.Unmarshal(summary.Strategy.Config) error = %v", err)
	}
	strategyCfg, err := rules.ParseOptions(strategyPayload["options_rules"])
	if err != nil {
		t.Fatalf("rules.ParseOptions(summary.options_rules) error = %v", err)
	}
	if strategyCfg.Management.CloseAtProfitPct != 65 {
		t.Fatalf("CloseAtProfitPct = %v, want %v", strategyCfg.Management.CloseAtProfitPct, 65)
	}
}

func TestScaffoldsRejectBlankTicker(t *testing.T) {
	if _, err := strategyscaffold.StockPaperMovingAverageCrossover("   "); err == nil {
		t.Fatal("StockPaperMovingAverageCrossover(blank) error = nil, want error")
	}
	if _, err := strategyscaffold.OptionsPaperBullPutSpread(""); err == nil {
		t.Fatal("OptionsPaperBullPutSpread(blank) error = nil, want error")
	}
}

func syntheticBars(count int) []domain.OHLCV {
	bars := make([]domain.OHLCV, 0, count)
	base := time.Date(2023, 1, 2, 0, 0, 0, 0, time.UTC)
	price := 100.0
	for i := 0; i < count; i++ {
		if i%23 == 0 {
			price -= 1.2
		} else {
			price += 0.45
		}
		bars = append(bars, domain.OHLCV{
			Timestamp: base.AddDate(0, 0, i),
			Open:      price - 0.3,
			High:      price + 0.8,
			Low:       price - 0.9,
			Close:     price,
			Volume:    1_500_000 + float64(i*1000),
		})
	}
	return bars
}

var _ = backtest.EquityPoint{}
