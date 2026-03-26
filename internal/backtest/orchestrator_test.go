package backtest

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/analysts"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestNewOrchestratorRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	bars := []domain.OHLCV{
		makeBar(time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), 100),
	}
	pipeline := makePipeline()
	validCfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	t.Run("nil strategy ID", func(t *testing.T) {
		cfg := validCfg
		cfg.StrategyID = uuid.Nil
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for nil strategy ID")
		}
	})

	t.Run("empty ticker", func(t *testing.T) {
		cfg := validCfg
		cfg.Ticker = ""
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for empty ticker")
		}
	})

	t.Run("whitespace-only ticker", func(t *testing.T) {
		cfg := validCfg
		cfg.Ticker = "   "
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for whitespace-only ticker")
		}
	})

	t.Run("zero start date", func(t *testing.T) {
		cfg := validCfg
		cfg.StartDate = time.Time{}
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for zero start date")
		}
	})

	t.Run("zero end date", func(t *testing.T) {
		cfg := validCfg
		cfg.EndDate = time.Time{}
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for zero end date")
		}
	})

	t.Run("end before start", func(t *testing.T) {
		cfg := validCfg
		cfg.StartDate = time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
		cfg.EndDate = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for end before start")
		}
	})

	t.Run("negative initial cash", func(t *testing.T) {
		cfg := validCfg
		cfg.InitialCash = -1
		_, err := NewOrchestrator(cfg, bars, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for negative initial cash")
		}
	})

	t.Run("nil pipeline", func(t *testing.T) {
		_, err := NewOrchestrator(validCfg, bars, nil, nil)
		if err == nil {
			t.Fatal("expected error for nil pipeline")
		}
	})

	t.Run("empty bars", func(t *testing.T) {
		_, err := NewOrchestrator(validCfg, nil, pipeline, nil)
		if err == nil {
			t.Fatal("expected error for empty bars")
		}
	})
}

func TestOrchestratorRunProcessesBars(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(start, 100),
		makeBar(start.Add(24*time.Hour), 101),
		makeBar(start.Add(48*time.Hour), 102),
	}

	cfg := OrchestratorConfig{
		StrategyID:    uuid.New(),
		Ticker:        "AAPL",
		StartDate:     start,
		EndDate:       start.Add(48 * time.Hour),
		InitialCash:   100_000,
		PromptVersion: "baseline",
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, slog.Default())
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if len(result.BarResults) != 3 {
		t.Errorf("BarResults len = %d, want 3", len(result.BarResults))
	}
	if len(result.EquityCurve) != 3 {
		t.Errorf("EquityCurve len = %d, want 3", len(result.EquityCurve))
	}
	if len(result.EquityCurveReport.Points) != 3 {
		t.Errorf("EquityCurveReport.Points len = %d, want 3", len(result.EquityCurveReport.Points))
	}
	if result.Metrics.TotalBars != 3 {
		t.Errorf("Metrics.TotalBars = %d, want 3", result.Metrics.TotalBars)
	}
	if result.Metrics.StartEquity != 100_000 {
		t.Errorf("Metrics.StartEquity = %f, want 100000", result.Metrics.StartEquity)
	}
	if result.PromptVersion != "baseline" {
		t.Errorf("PromptVersion = %q, want %q", result.PromptVersion, "baseline")
	}
	if result.PromptVersionHash != analysts.CurrentPromptVersionHash() {
		t.Errorf("PromptVersionHash = %q, want current prompt-set hash", result.PromptVersionHash)
	}
}

func TestOrchestratorFiltersBarsToDateRange(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base, 100),                   // in range
		makeBar(base.Add(24*time.Hour), 101), // in range
		makeBar(base.Add(48*time.Hour), 102), // in range
		makeBar(base.Add(72*time.Hour), 103), // out of range
		makeBar(base.Add(96*time.Hour), 104), // out of range
	}

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   base,
		EndDate:     base.Add(48 * time.Hour),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// Only 3 bars should be processed (within the date range).
	if len(result.BarResults) != 3 {
		t.Errorf("BarResults len = %d, want 3", len(result.BarResults))
	}
	wantBuyAndHold := (102.0 - 100.0) / 100.0
	if result.Metrics.BuyAndHoldReturn != wantBuyAndHold {
		t.Errorf("Metrics.BuyAndHoldReturn = %f, want %f", result.Metrics.BuyAndHoldReturn, wantBuyAndHold)
	}
}

func TestOrchestratorBenchmarkUsesExecutionOrderWhenInputBarsUnsorted(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base.Add(48*time.Hour), 102),
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 101),
	}

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   base,
		EndDate:     base.Add(48 * time.Hour),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	wantBuyAndHold := (102.0 - 100.0) / 100.0
	if result.Metrics.BuyAndHoldReturn != wantBuyAndHold {
		t.Errorf("Metrics.BuyAndHoldReturn = %f, want %f", result.Metrics.BuyAndHoldReturn, wantBuyAndHold)
	}
}

func TestOrchestratorRunRejectsEmptyDateRange(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	// Bars are outside the requested range.
	bars := []domain.OHLCV{
		makeBar(base.Add(72*time.Hour), 103),
	}

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   base,
		EndDate:     base.Add(48 * time.Hour),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	_, err = orch.Run(context.Background())
	if err == nil {
		t.Fatal("expected error when no bars match date range")
	}
}

func TestOrchestratorRunRespectsContextCancellation(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 101),
	}

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   base,
		EndDate:     base.Add(24 * time.Hour),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = orch.Run(ctx)
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestOrchestratorWiresClockTargets(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base, 100),
	}

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   base,
		EndDate:     base,
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	rec := &nowFuncRecorder{}
	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, nil, rec)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	_, err = orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !rec.called {
		t.Error("clock target was not wired")
	}
}

func TestOrchestratorResultHasMetrics(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
		makeBar(base.Add(48*time.Hour), 110),
	}

	cfg := OrchestratorConfig{
		StrategyID:  uuid.New(),
		Ticker:      "AAPL",
		StartDate:   base,
		EndDate:     base.Add(48 * time.Hour),
		InitialCash: 100_000,
		FillConfig: FillConfig{
			Slippage: ProportionalSlippage{BasisPoints: 0},
		},
	}

	pipeline := makePipeline()
	orch, err := NewOrchestrator(cfg, bars, pipeline, nil)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	result, err := orch.Run(context.Background())
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	// With no pipeline-generated orders, trades should be empty.
	if len(result.Trades) != 0 {
		t.Errorf("Trades len = %d, want 0", len(result.Trades))
	}
	// With no trades, equity stays flat at initial cash.
	if result.Metrics.StartEquity != result.Metrics.EndEquity {
		t.Errorf("with no trades, start (%f) and end (%f) equity should match",
			result.Metrics.StartEquity, result.Metrics.EndEquity)
	}
	if result.Metrics.TotalBars != 3 {
		t.Errorf("Metrics.TotalBars = %d, want 3", result.Metrics.TotalBars)
	}
	if !result.Metrics.StartTime.Equal(base) {
		t.Errorf("Metrics.StartTime = %v, want %v", result.Metrics.StartTime, base)
	}
	if result.TradeAnalytics.ClosedTrades != 0 {
		t.Errorf("TradeAnalytics.ClosedTrades = %d, want 0", result.TradeAnalytics.ClosedTrades)
	}
}

func TestFilterBars(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 101),
		makeBar(base.Add(48*time.Hour), 102),
		makeBar(base.Add(72*time.Hour), 103),
	}

	t.Run("full range", func(t *testing.T) {
		f := filterBars(bars, base, base.Add(72*time.Hour))
		if len(f) != 4 {
			t.Errorf("len = %d, want 4", len(f))
		}
	})

	t.Run("partial range", func(t *testing.T) {
		f := filterBars(bars, base.Add(24*time.Hour), base.Add(48*time.Hour))
		if len(f) != 2 {
			t.Errorf("len = %d, want 2", len(f))
		}
	})

	t.Run("no match", func(t *testing.T) {
		f := filterBars(bars, base.Add(96*time.Hour), base.Add(120*time.Hour))
		if len(f) != 0 {
			t.Errorf("len = %d, want 0", len(f))
		}
	})

	t.Run("inclusive boundaries", func(t *testing.T) {
		f := filterBars(bars, base, base)
		if len(f) != 1 {
			t.Errorf("len = %d, want 1", len(f))
		}
	})
}
