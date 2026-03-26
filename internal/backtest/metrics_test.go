package backtest

import (
	"math"
	"testing"
	"time"
)

func TestComputeMetricsEmpty(t *testing.T) {
	t.Parallel()

	m := ComputeMetrics(nil)
	if m.TotalBars != 0 {
		t.Errorf("TotalBars = %d, want 0", m.TotalBars)
	}
}

func TestComputeMetricsSinglePoint(t *testing.T) {
	t.Parallel()

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: ts, Equity: 100_000, Cash: 100_000},
	}
	m := ComputeMetrics(curve)

	if m.TotalBars != 1 {
		t.Errorf("TotalBars = %d, want 1", m.TotalBars)
	}
	if m.StartEquity != 100_000 {
		t.Errorf("StartEquity = %f, want 100000", m.StartEquity)
	}
	if m.EndEquity != 100_000 {
		t.Errorf("EndEquity = %f, want 100000", m.EndEquity)
	}
	// With a single point, return-based metrics should be zero.
	if m.TotalReturn != 0 {
		t.Errorf("TotalReturn = %f, want 0", m.TotalReturn)
	}
}

func TestComputeMetricsTotalReturn(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100_000},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110_000},
	}
	m := ComputeMetrics(curve)

	wantReturn := 0.1
	if math.Abs(m.TotalReturn-wantReturn) > 1e-9 {
		t.Errorf("TotalReturn = %f, want %f", m.TotalReturn, wantReturn)
	}
}

func TestComputeMetricsMaxDrawdown(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Equity rises to 110, drops to 88 (20% drawdown from peak), recovers.
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(1 * 24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(2 * 24 * time.Hour), Equity: 88},
		{Timestamp: base.Add(3 * 24 * time.Hour), Equity: 105},
	}
	m := ComputeMetrics(curve)

	wantDD := (110.0 - 88.0) / 110.0 // 0.2
	if math.Abs(m.MaxDrawdown-wantDD) > 1e-9 {
		t.Errorf("MaxDrawdown = %f, want %f", m.MaxDrawdown, wantDD)
	}
}

func TestComputeMetricsWinRateAndProfitFactor(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 3 returns: +10%, -5%, +3% → 2 wins, 1 loss
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(1 * 24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(2 * 24 * time.Hour), Equity: 104.5},
		{Timestamp: base.Add(3 * 24 * time.Hour), Equity: 107.635},
	}
	m := ComputeMetrics(curve)

	// Win rate: 2 wins / 3 total (flat excluded) → 0.6667
	wantWR := 2.0 / 3.0
	if math.Abs(m.WinRate-wantWR) > 1e-4 {
		t.Errorf("WinRate = %f, want %f", m.WinRate, wantWR)
	}

	if m.ProfitFactor <= 0 {
		t.Errorf("ProfitFactor = %f, want > 0", m.ProfitFactor)
	}
}

func TestComputeMetricsSharpeAndSortino(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// Mixed returns with an overall upward trend and some down bars so
	// both Sharpe and Sortino can be meaningfully computed.
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100_000},
		{Timestamp: base.Add(1 * 24 * time.Hour), Equity: 101_000},
		{Timestamp: base.Add(2 * 24 * time.Hour), Equity: 100_500},
		{Timestamp: base.Add(3 * 24 * time.Hour), Equity: 102_000},
		{Timestamp: base.Add(4 * 24 * time.Hour), Equity: 101_500},
		{Timestamp: base.Add(5 * 24 * time.Hour), Equity: 103_000},
	}
	m := ComputeMetrics(curve)

	if m.SharpeRatio <= 0 {
		t.Errorf("SharpeRatio = %f, want > 0", m.SharpeRatio)
	}
	if m.SortinoRatio <= 0 {
		t.Errorf("SortinoRatio = %f, want > 0", m.SortinoRatio)
	}
	if m.Volatility <= 0 {
		t.Errorf("Volatility = %f, want > 0", m.Volatility)
	}
}

func TestComputeMetricsNoLosses(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(48 * time.Hour), Equity: 120},
	}
	m := ComputeMetrics(curve)

	if m.WinRate != 1.0 {
		t.Errorf("WinRate = %f, want 1.0", m.WinRate)
	}
	if !math.IsInf(m.ProfitFactor, 1) {
		t.Errorf("ProfitFactor = %f, want +Inf", m.ProfitFactor)
	}
}

func TestComputeMetricsTimestamps(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: start, Equity: 100},
		{Timestamp: end, Equity: 105},
	}
	m := ComputeMetrics(curve)

	if !m.StartTime.Equal(start) {
		t.Errorf("StartTime = %v, want %v", m.StartTime, start)
	}
	if !m.EndTime.Equal(end) {
		t.Errorf("EndTime = %v, want %v", m.EndTime, end)
	}
}
