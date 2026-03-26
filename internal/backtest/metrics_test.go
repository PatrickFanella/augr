package backtest

import (
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestComputeMetricsEmpty(t *testing.T) {
	t.Parallel()

	m := ComputeMetrics(nil, nil)
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
	m := ComputeMetrics(curve, nil)

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
	m := ComputeMetrics(curve, nil)

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
	m := ComputeMetrics(curve, nil)

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
	m := ComputeMetrics(curve, nil)

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
	m := ComputeMetrics(curve, nil)

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
	m := ComputeMetrics(curve, nil)

	if m.WinRate != 1.0 {
		t.Errorf("WinRate = %f, want 1.0", m.WinRate)
	}
	if !math.IsInf(m.ProfitFactor, 1) {
		t.Errorf("ProfitFactor = %f, want +Inf", m.ProfitFactor)
	}
	if !math.IsInf(m.AvgWinLossRatio, 1) {
		t.Errorf("AvgWinLossRatio = %f, want +Inf", m.AvgWinLossRatio)
	}
}

func TestComputeMetricsCalmarAndAvgWinLossRatio(t *testing.T) {
	t.Parallel()

	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	// Returns: +25%, -20%, +10%, -5% over >1 year.
	curve := []EquityPoint{
		{Timestamp: start, Equity: 100},
		{Timestamp: start.Add(24 * time.Hour), Equity: 125},
		{Timestamp: start.Add(48 * time.Hour), Equity: 100},
		{Timestamp: start.Add(72 * time.Hour), Equity: 110},
		{Timestamp: start.Add(96 * time.Hour), Equity: 104.5},
		{Timestamp: start.Add(366 * 24 * time.Hour), Equity: 104.5},
	}

	m := ComputeMetrics(curve, nil)

	wantAvgWinLoss := ((0.25 + 0.10) / 2.0) / ((0.20 + 0.05) / 2.0) // 1.4
	if math.Abs(m.AvgWinLossRatio-wantAvgWinLoss) > 1e-9 {
		t.Errorf("AvgWinLossRatio = %f, want %f", m.AvgWinLossRatio, wantAvgWinLoss)
	}
	if m.CalmarRatio <= 0 {
		t.Errorf("CalmarRatio = %f, want > 0", m.CalmarRatio)
	}
}

func TestComputeMetricsCalmarZeroWhenNoDrawdown(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(48 * time.Hour), Equity: 120},
	}
	m := ComputeMetrics(curve, nil)

	if m.MaxDrawdown != 0 {
		t.Errorf("MaxDrawdown = %f, want 0", m.MaxDrawdown)
	}
	if m.CalmarRatio != 0 {
		t.Errorf("CalmarRatio = %f, want 0", m.CalmarRatio)
	}
}

func TestComputeMetricsCalmarZeroWhenEndingEquityNonPositive(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 120},
		{Timestamp: base.Add(48 * time.Hour), Equity: -10},
		{Timestamp: base.Add(366 * 24 * time.Hour), Equity: -10},
	}
	m := ComputeMetrics(curve, nil)

	if m.MaxDrawdown <= 0 {
		t.Errorf("MaxDrawdown = %f, want > 0", m.MaxDrawdown)
	}
	if m.CalmarRatio != 0 {
		t.Errorf("CalmarRatio = %f, want 0 for non-positive equity ratio", m.CalmarRatio)
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
	m := ComputeMetrics(curve, nil)

	if !m.StartTime.Equal(start) {
		t.Errorf("StartTime = %v, want %v", m.StartTime, start)
	}
	if !m.EndTime.Equal(end) {
		t.Errorf("EndTime = %v, want %v", m.EndTime, end)
	}
}

func TestComputeMetricsBenchmarkComparison(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(48 * time.Hour), Equity: 116.6},
	}
	benchmark := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
		makeBar(base.Add(48*time.Hour), 108.15),
	}

	m := ComputeMetrics(curve, benchmark)

	if math.Abs(m.BuyAndHoldReturn-0.0815) > 1e-9 {
		t.Errorf("BuyAndHoldReturn = %f, want %f", m.BuyAndHoldReturn, 0.0815)
	}
	if math.Abs(m.Beta-2.0) > 1e-9 {
		t.Errorf("Beta = %f, want %f", m.Beta, 2.0)
	}
	if math.Abs(m.Alpha) > 1e-9 {
		t.Errorf("Alpha = %f, want 0", m.Alpha)
	}
}

func TestComputeMetricsInformationRatio(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 109},
		{Timestamp: base.Add(48 * time.Hour), Equity: 114.45},
	}
	benchmark := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
		makeBar(base.Add(48*time.Hour), 109.2),
	}

	m := ComputeMetrics(curve, benchmark)
	if m.InformationRatio <= 0 {
		t.Errorf("InformationRatio = %f, want > 0", m.InformationRatio)
	}
}

func TestComputeMetricsBenchmarkZeroTrackingError(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(48 * time.Hour), Equity: 121},
	}
	benchmark := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 110),
		makeBar(base.Add(48*time.Hour), 121),
	}

	m := ComputeMetrics(curve, benchmark)

	if m.InformationRatio != 0 {
		t.Errorf("InformationRatio = %f, want 0 when tracking error is zero", m.InformationRatio)
	}
	if m.Beta != 0 {
		t.Errorf("Beta = %f, want 0 when benchmark variance is zero", m.Beta)
	}
	if m.Alpha != 0 {
		t.Errorf("Alpha = %f, want 0 when strategy returns match zero-variance benchmark", m.Alpha)
	}
}

func TestComputeMetricsBenchmarkOrderIndependence(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110},
		{Timestamp: base.Add(48 * time.Hour), Equity: 116.6},
	}
	sortedBenchmark := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
		makeBar(base.Add(48*time.Hour), 108.15),
	}
	unsortedBenchmark := []domain.OHLCV{
		makeBar(base.Add(48*time.Hour), 108.15),
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
	}

	sortedMetrics := ComputeMetrics(curve, sortedBenchmark)
	unsortedMetrics := ComputeMetrics(curve, unsortedBenchmark)

	if math.Abs(sortedMetrics.BuyAndHoldReturn-unsortedMetrics.BuyAndHoldReturn) > 1e-9 {
		t.Errorf("BuyAndHoldReturn mismatch for unsorted bars: sorted=%f unsorted=%f", sortedMetrics.BuyAndHoldReturn, unsortedMetrics.BuyAndHoldReturn)
	}
	if math.Abs(sortedMetrics.Beta-unsortedMetrics.Beta) > 1e-9 {
		t.Errorf("Beta mismatch for unsorted bars: sorted=%f unsorted=%f", sortedMetrics.Beta, unsortedMetrics.Beta)
	}
	if math.Abs(sortedMetrics.Alpha-unsortedMetrics.Alpha) > 1e-9 {
		t.Errorf("Alpha mismatch for unsorted bars: sorted=%f unsorted=%f", sortedMetrics.Alpha, unsortedMetrics.Alpha)
	}
	if math.Abs(sortedMetrics.InformationRatio-unsortedMetrics.InformationRatio) > 1e-9 {
		t.Errorf("InformationRatio mismatch for unsorted bars: sorted=%f unsorted=%f", sortedMetrics.InformationRatio, unsortedMetrics.InformationRatio)
	}
}

func TestComputeMetricsBenchmarkSampleTooShort(t *testing.T) {
	t.Parallel()

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	curve := []EquityPoint{
		{Timestamp: base, Equity: 100},
		{Timestamp: base.Add(24 * time.Hour), Equity: 110},
	}
	benchmark := []domain.OHLCV{
		makeBar(base, 100),
		makeBar(base.Add(24*time.Hour), 105),
	}

	m := ComputeMetrics(curve, benchmark)
	if m.Beta != 0 {
		t.Errorf("Beta = %f, want 0 for single aligned return observation", m.Beta)
	}
	if m.Alpha != 0 {
		t.Errorf("Alpha = %f, want 0 for single aligned return observation", m.Alpha)
	}
	if m.InformationRatio != 0 {
		t.Errorf("InformationRatio = %f, want 0 for single aligned return observation", m.InformationRatio)
	}
}
