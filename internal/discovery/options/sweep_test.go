package options

import (
	"context"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestRunOptionsSweepReturnsTradesAndEquityCurve(t *testing.T) {
	t.Parallel()

	bars := syntheticBars(340)
	cfg := rules.OptionsRulesConfig{
		Version:      1,
		StrategyType: domain.StrategyBullPutSpread,
		Underlying:   "QQQ",
		Entry: rules.ConditionGroup{
			Operator: "AND",
			Conditions: []rules.Condition{
				{Field: "close", Op: "gt", Ref: "sma_50"},
				{Field: "iv_rank", Op: "gt", Value: fp(30)},
			},
		},
		Exit: rules.ConditionGroup{
			Operator: "OR",
			Conditions: []rules.Condition{
				{Field: "close", Op: "lt", Ref: "sma_50"},
				{Field: "pnl_pct", Op: "gte", Value: fp(50)},
			},
		},
		LegSelection: map[string]rules.LegSelector{
			"short_put": {
				OptionType:  domain.OptionTypePut,
				DeltaTarget: 0.25,
				DTEMin:      30,
				DTEMax:      45,
				Side:        domain.OrderSideSell,
				Intent:      domain.PositionIntentSellToOpen,
				Ratio:       1,
			},
			"long_put": {
				OptionType:  domain.OptionTypePut,
				DeltaTarget: 0.10,
				DTEMin:      30,
				DTEMax:      45,
				Side:        domain.OrderSideBuy,
				Intent:      domain.PositionIntentBuyToOpen,
				Ratio:       1,
			},
		},
		PositionSizing: rules.OptionsSizingConfig{Method: "max_risk", MaxRiskUSD: 1000},
		Management:     rules.OptionsManagement{CloseAtProfitPct: 50, CloseAtDTE: 7, StopLossPct: 100},
	}

	results, err := RunOptionsSweep(context.Background(), cfg, OptionsSweepConfig{
		Ticker:      "QQQ",
		Bars:        bars,
		StartDate:   bars[40].Timestamp,
		EndDate:     bars[len(bars)-1].Timestamp,
		InitialCash: 100_000,
		Variations:  0,
	}, discovery.DefaultScoringConfig(), nil)
	if err != nil {
		t.Fatalf("RunOptionsSweep() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("RunOptionsSweep() returned no results")
	}

	best := results[0]
	if len(best.Trades) == 0 {
		t.Fatal("best.Trades empty, want full trade log")
	}
	if len(best.EquityCurve) < 10 {
		t.Fatalf("best.EquityCurve len = %d, want >= 10", len(best.EquityCurve))
	}
	if best.Trades[0].AssetClass != domain.AssetClassOption {
		t.Fatalf("first trade asset_class = %q, want %q", best.Trades[0].AssetClass, domain.AssetClassOption)
	}
	if best.EquityCurve[0].Timestamp.IsZero() || best.EquityCurve[len(best.EquityCurve)-1].Timestamp.IsZero() {
		t.Fatal("equity curve timestamps must be populated")
	}
}

func TestRunOptionsSweepForceClosesOpenPositionOnFinalBar(t *testing.T) {
	t.Parallel()

	bars := syntheticBars(260)
	cfg := rules.OptionsRulesConfig{
		Version:      1,
		StrategyType: domain.StrategyBullPutSpread,
		Underlying:   "QQQ",
		Entry: rules.ConditionGroup{
			Operator: "AND",
			Conditions: []rules.Condition{
				{Field: "close", Op: "gt", Ref: "sma_50"},
				{Field: "iv_rank", Op: "gt", Value: fp(30)},
			},
		},
		Exit: rules.ConditionGroup{
			Operator: "AND",
			Conditions: []rules.Condition{{Field: "pnl_pct", Op: "gt", Value: fp(5000)}},
		},
		LegSelection: map[string]rules.LegSelector{
			"short_put": {OptionType: domain.OptionTypePut, DeltaTarget: 0.25, DTEMin: 30, DTEMax: 45, Side: domain.OrderSideSell, Intent: domain.PositionIntentSellToOpen, Ratio: 1},
			"long_put":  {OptionType: domain.OptionTypePut, DeltaTarget: 0.10, DTEMin: 30, DTEMax: 45, Side: domain.OrderSideBuy, Intent: domain.PositionIntentBuyToOpen, Ratio: 1},
		},
		PositionSizing: rules.OptionsSizingConfig{Method: "max_risk", MaxRiskUSD: 1000},
		Management:     rules.OptionsManagement{CloseAtProfitPct: 0, CloseAtDTE: 0, StopLossPct: 0},
	}

	results, err := RunOptionsSweep(context.Background(), cfg, OptionsSweepConfig{
		Ticker:      "QQQ",
		Bars:        bars,
		StartDate:   bars[40].Timestamp,
		EndDate:     bars[len(bars)-1].Timestamp,
		InitialCash: 100_000,
		Variations:  0,
	}, discovery.DefaultScoringConfig(), nil)
	if err != nil {
		t.Fatalf("RunOptionsSweep() error = %v", err)
	}
	if len(results) == 0 {
		t.Fatal("RunOptionsSweep() returned no results")
	}

	trades := results[0].Trades
	if len(trades) < 4 {
		t.Fatalf("trades len = %d, want >= 4", len(trades))
	}
	last := trades[len(trades)-1]
	if last.OpenClose != "close" {
		t.Fatalf("last trade open_close = %q, want close", last.OpenClose)
	}
	if last.ExitReason != "final_bar" {
		t.Fatalf("last trade exit_reason = %q, want %q", last.ExitReason, "final_bar")
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

func fp(v float64) *float64 { return &v }

var _ = backtest.EquityPoint{}
