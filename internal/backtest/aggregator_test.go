package backtest

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

// --- helpers ---------------------------------------------------------------

// makeOrchestratorResult builds an OrchestratorResult with the given Metrics.
func makeOrchestratorResult(m Metrics) *OrchestratorResult {
	return &OrchestratorResult{Metrics: m}
}

// fixedRunFunc returns a RunFunc that always returns the provided Metrics.
func fixedRunFunc(m Metrics) RunFunc {
	return func(_ context.Context) (*OrchestratorResult, error) {
		return makeOrchestratorResult(m), nil
	}
}

// sequentialRunFunc returns a RunFunc that yields the Metrics from the
// supplied slice in order. Panics if called more times than len(results).
func sequentialRunFunc(results []Metrics) RunFunc {
	idx := 0
	return func(_ context.Context) (*OrchestratorResult, error) {
		m := results[idx]
		idx++
		return makeOrchestratorResult(m), nil
	}
}

// --- validation tests ------------------------------------------------------

func TestRunMulti_RequiresMinRuns(t *testing.T) {
	cfg := MultiRunConfig{
		Runs:    2,
		RunFunc: fixedRunFunc(Metrics{}),
	}
	_, err := RunMulti(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error for fewer than MinRecommendedRuns")
	}
}

func TestRunMulti_RequiresRunFunc(t *testing.T) {
	cfg := MultiRunConfig{
		Runs: 3,
	}
	_, err := RunMulti(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when RunFunc is nil")
	}
}

// --- successful aggregation -----------------------------------------------

func TestRunMulti_IdenticalRuns(t *testing.T) {
	m := Metrics{
		TotalReturn:     0.10,
		MaxDrawdown:     0.05,
		CalmarRatio:     2.0,
		SharpeRatio:     1.5,
		SortinoRatio:    2.0,
		WinRate:         0.6,
		ProfitFactor:    1.8,
		AvgWinLossRatio: 1.2,
		Volatility:      0.15,
		EndEquity:       110_000,
		RealizedPnL:     5_000,
		UnrealizedPnL:   5_000,
	}

	cfg := MultiRunConfig{
		Runs:    5,
		RunFunc: fixedRunFunc(m),
	}

	result, err := RunMulti(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Runs != 5 {
		t.Fatalf("expected 5 runs, got %d", result.Runs)
	}
	if len(result.Individual) != 5 {
		t.Fatalf("expected 5 individual results, got %d", len(result.Individual))
	}

	// All identical → stddev should be 0 for every metric.
	agg := result.Aggregated
	assertStats(t, "TotalReturn", agg.TotalReturn, 0.10, 0, 0.10, 0.10)
	assertStats(t, "MaxDrawdown", agg.MaxDrawdown, 0.05, 0, 0.05, 0.05)
	assertStats(t, "SharpeRatio", agg.SharpeRatio, 1.5, 0, 1.5, 1.5)
	assertStats(t, "WinRate", agg.WinRate, 0.6, 0, 0.6, 0.6)
	assertStats(t, "EndEquity", agg.EndEquity, 110_000, 0, 110_000, 110_000)
}

func TestRunMulti_VaryingRuns(t *testing.T) {
	metrics := []Metrics{
		{TotalReturn: 0.10, SharpeRatio: 1.0, EndEquity: 110_000},
		{TotalReturn: 0.20, SharpeRatio: 2.0, EndEquity: 120_000},
		{TotalReturn: 0.30, SharpeRatio: 3.0, EndEquity: 130_000},
	}

	cfg := MultiRunConfig{
		Runs:    3,
		RunFunc: sequentialRunFunc(metrics),
	}

	result, err := RunMulti(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	agg := result.Aggregated

	// Mean of {0.10, 0.20, 0.30} = 0.20
	assertFloat(t, "TotalReturn.Mean", agg.TotalReturn.Mean, 0.20)
	assertFloat(t, "TotalReturn.Min", agg.TotalReturn.Min, 0.10)
	assertFloat(t, "TotalReturn.Max", agg.TotalReturn.Max, 0.30)

	// Sample stddev of {0.10, 0.20, 0.30} = 0.10
	if math.Abs(agg.TotalReturn.StdDev-0.10) > 0.001 {
		t.Errorf("TotalReturn.StdDev: got %f, want ~0.10", agg.TotalReturn.StdDev)
	}

	assertFloat(t, "SharpeRatio.Mean", agg.SharpeRatio.Mean, 2.0)
	assertFloat(t, "EndEquity.Mean", agg.EndEquity.Mean, 120_000)
}

// --- confidence intervals --------------------------------------------------

func TestMetricStats_ConfidenceInterval_95(t *testing.T) {
	// 3 values → df=2, t(0.975,2) = 4.303
	stats := computeStats([]float64{10, 20, 30})
	lower, upper := stats.ConfidenceInterval(0.95)

	// mean=20, sample sd=10, se=10/sqrt(3)=5.774, margin=4.303*5.774≈24.86
	expectedMargin := 4.303 * (stats.StdDev / math.Sqrt(float64(stats.N)))
	assertFloat(t, "lower", lower, stats.Mean-expectedMargin)
	assertFloat(t, "upper", upper, stats.Mean+expectedMargin)
}

func TestMetricStats_ConfidenceInterval_SingleValue(t *testing.T) {
	stats := computeStats([]float64{42})
	lower, upper := stats.ConfidenceInterval(0.95)
	assertFloat(t, "lower", lower, 42)
	assertFloat(t, "upper", upper, 42)
}

func TestMetricStats_ConfidenceInterval_UnsupportedLevel(t *testing.T) {
	stats := computeStats([]float64{1, 2, 3})
	lower, upper := stats.ConfidenceInterval(0.50)
	if !math.IsNaN(lower) || !math.IsNaN(upper) {
		t.Errorf("expected NaN for unsupported level, got %f, %f", lower, upper)
	}
}

func TestMetricStats_ConfidenceInterval_LargeN(t *testing.T) {
	// 50 values → df > 30, should use z=1.96 for 95%
	values := make([]float64, 50)
	for i := range values {
		values[i] = float64(i)
	}
	stats := computeStats(values)
	lower, upper := stats.ConfidenceInterval(0.95)

	se := stats.StdDev / math.Sqrt(float64(stats.N))
	expectedMargin := 1.960 * se
	assertFloat(t, "lower", lower, stats.Mean-expectedMargin)
	assertFloat(t, "upper", upper, stats.Mean+expectedMargin)
}

// --- edge cases ------------------------------------------------------------

func TestRunMulti_RunFuncError(t *testing.T) {
	callCount := 0
	cfg := MultiRunConfig{
		Runs: 3,
		RunFunc: func(_ context.Context) (*OrchestratorResult, error) {
			callCount++
			if callCount == 2 {
				return nil, errors.New("simulated failure")
			}
			return makeOrchestratorResult(Metrics{}), nil
		},
	}

	_, err := RunMulti(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when a run fails")
	}
}

func TestRunMulti_NilResult(t *testing.T) {
	cfg := MultiRunConfig{
		Runs: 3,
		RunFunc: func(_ context.Context) (*OrchestratorResult, error) {
			return nil, nil
		},
	}

	_, err := RunMulti(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error when RunFunc returns nil result")
	}
}

func TestRunMulti_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	cfg := MultiRunConfig{
		Runs:    3,
		RunFunc: fixedRunFunc(Metrics{}),
	}

	_, err := RunMulti(ctx, cfg)
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestRunMulti_InfValues(t *testing.T) {
	// ProfitFactor/AvgWinLossRatio can be +Inf when there are no losses.
	metrics := []Metrics{
		{ProfitFactor: math.Inf(1), AvgWinLossRatio: math.Inf(1), TotalReturn: 0.1},
		{ProfitFactor: 2.0, AvgWinLossRatio: 1.5, TotalReturn: 0.2},
		{ProfitFactor: 3.0, AvgWinLossRatio: 2.0, TotalReturn: 0.3},
	}

	cfg := MultiRunConfig{
		Runs:    3,
		RunFunc: sequentialRunFunc(metrics),
	}

	result, err := RunMulti(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Inf values should be filtered; mean of {2.0, 3.0} = 2.5
	assertFloat(t, "ProfitFactor.Mean", result.Aggregated.ProfitFactor.Mean, 2.5)
	// Original values slice should still contain all 3 values.
	if len(result.Aggregated.ProfitFactor.Values) != 3 {
		t.Errorf("expected 3 values, got %d", len(result.Aggregated.ProfitFactor.Values))
	}
}

func TestRunMulti_AllInfValues(t *testing.T) {
	metrics := []Metrics{
		{ProfitFactor: math.Inf(1)},
		{ProfitFactor: math.Inf(1)},
		{ProfitFactor: math.Inf(1)},
	}

	cfg := MultiRunConfig{
		Runs:    3,
		RunFunc: sequentialRunFunc(metrics),
	}

	result, err := RunMulti(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !math.IsNaN(result.Aggregated.ProfitFactor.Mean) {
		t.Errorf("expected NaN when all values are Inf, got %f", result.Aggregated.ProfitFactor.Mean)
	}
}

// --- computeStats unit tests -----------------------------------------------

func TestComputeStats_Empty(t *testing.T) {
	stats := computeStats(nil)
	if stats.Mean != 0 || stats.StdDev != 0 {
		t.Errorf("empty stats should be zero: got mean=%f, stddev=%f", stats.Mean, stats.StdDev)
	}
}

func TestComputeStats_Single(t *testing.T) {
	stats := computeStats([]float64{42})
	assertFloat(t, "Mean", stats.Mean, 42)
	assertFloat(t, "StdDev", stats.StdDev, 0)
	assertFloat(t, "Min", stats.Min, 42)
	assertFloat(t, "Max", stats.Max, 42)
}

// --- tCritical unit tests --------------------------------------------------

func TestTCritical_KnownValues(t *testing.T) {
	tests := []struct {
		df    int
		level float64
		want  float64
	}{
		{2, 0.95, 4.303},
		{9, 0.95, 2.262},
		{30, 0.95, 2.042},
		{100, 0.95, 1.960}, // z-approximation
	}
	for _, tt := range tests {
		got, ok := tCritical(tt.df, tt.level)
		if !ok {
			t.Errorf("tCritical(%d, %f): expected ok=true", tt.df, tt.level)
			continue
		}
		if math.Abs(got-tt.want) > 0.001 {
			t.Errorf("tCritical(%d, %f) = %f, want %f", tt.df, tt.level, got, tt.want)
		}
	}
}

func TestTCritical_InvalidDF(t *testing.T) {
	_, ok := tCritical(0, 0.95)
	if ok {
		t.Error("expected ok=false for df=0")
	}
}

func TestTCritical_UnsupportedLevel(t *testing.T) {
	_, ok := tCritical(5, 0.50)
	if ok {
		t.Error("expected ok=false for unsupported level")
	}
}

// --- timing test -----------------------------------------------------------

func TestRunMulti_PreservesStartEndTime(t *testing.T) {
	now := time.Now()
	m1 := Metrics{StartTime: now, EndTime: now.Add(time.Hour)}
	m2 := Metrics{StartTime: now, EndTime: now.Add(2 * time.Hour)}
	m3 := Metrics{StartTime: now, EndTime: now.Add(3 * time.Hour)}

	cfg := MultiRunConfig{
		Runs:    3,
		RunFunc: sequentialRunFunc([]Metrics{m1, m2, m3}),
	}

	result, err := RunMulti(context.Background(), cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify individual metrics are preserved.
	if !result.Individual[0].StartTime.Equal(now) {
		t.Error("individual run start time not preserved")
	}
	if !result.Individual[2].EndTime.Equal(now.Add(3 * time.Hour)) {
		t.Error("individual run end time not preserved")
	}
}

// --- helpers ---------------------------------------------------------------

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-6 {
		t.Errorf("%s: got %f, want %f", name, got, want)
	}
}

func assertStats(t *testing.T, name string, s MetricStats, wantMean, wantStdDev, wantMin, wantMax float64) {
	t.Helper()
	assertFloat(t, name+".Mean", s.Mean, wantMean)
	assertFloat(t, name+".StdDev", s.StdDev, wantStdDev)
	assertFloat(t, name+".Min", s.Min, wantMin)
	assertFloat(t, name+".Max", s.Max, wantMax)
}
