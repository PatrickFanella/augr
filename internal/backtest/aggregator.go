package backtest

import (
	"context"
	"fmt"
	"math"
)

// MinRecommendedRuns is the minimum number of runs recommended to establish
// meaningful confidence intervals when quantifying LLM non-determinism.
const MinRecommendedRuns = 3

// RunFunc executes a single backtest run and returns its result.
type RunFunc func(ctx context.Context) (*OrchestratorResult, error)

// MultiRunConfig configures the multi-run aggregator.
type MultiRunConfig struct {
	// Runs is the number of times to execute the backtest. Must be >= MinRecommendedRuns.
	Runs int

	// RunFunc executes a single backtest and returns the result.
	RunFunc RunFunc
}

// MultiRunResult holds per-run metrics and aggregated statistics across N
// backtest runs, enabling quantification of non-determinism from LLM responses.
type MultiRunResult struct {
	Runs       int
	Individual []Metrics
	Aggregated AggregatedMetrics
}

// MetricStats holds descriptive statistics for a single metric across runs.
type MetricStats struct {
	Mean   float64
	StdDev float64
	Min    float64
	Max    float64
	Values []float64
}

// ConfidenceInterval returns the lower and upper bounds of a confidence
// interval using t-distribution critical values. Supported levels are
// 0.90, 0.95, and 0.99. An unsupported level returns (NaN, NaN).
// With fewer than 2 values the interval collapses to [Mean, Mean].
func (s MetricStats) ConfidenceInterval(level float64) (lower, upper float64) {
	n := len(s.Values)
	if n < 2 {
		return s.Mean, s.Mean
	}

	t, ok := tCritical(n-1, level)
	if !ok {
		return math.NaN(), math.NaN()
	}

	se := s.StdDev / math.Sqrt(float64(n))
	margin := t * se
	return s.Mean - margin, s.Mean + margin
}

// AggregatedMetrics contains summary statistics for every numeric field of
// Metrics, computed across multiple backtest runs.
type AggregatedMetrics struct {
	TotalReturn     MetricStats
	MaxDrawdown     MetricStats
	CalmarRatio     MetricStats
	SharpeRatio     MetricStats
	SortinoRatio    MetricStats
	WinRate         MetricStats
	ProfitFactor    MetricStats
	AvgWinLossRatio MetricStats
	Volatility      MetricStats
	EndEquity       MetricStats
	RealizedPnL     MetricStats
	UnrealizedPnL   MetricStats
}

// RunMulti executes cfg.RunFunc the specified number of times and aggregates
// the resulting metrics. It returns an error if the configuration is invalid
// or if any individual run fails.
func RunMulti(ctx context.Context, cfg MultiRunConfig) (*MultiRunResult, error) {
	if err := validateMultiRunConfig(cfg); err != nil {
		return nil, err
	}

	individual := make([]Metrics, 0, cfg.Runs)
	for i := 0; i < cfg.Runs; i++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("aggregator: context cancelled before run %d: %w", i+1, err)
		}

		result, err := cfg.RunFunc(ctx)
		if err != nil {
			return nil, fmt.Errorf("aggregator: run %d failed: %w", i+1, err)
		}

		individual = append(individual, result.Metrics)
	}

	return &MultiRunResult{
		Runs:       cfg.Runs,
		Individual: individual,
		Aggregated: aggregateMetrics(individual),
	}, nil
}

func validateMultiRunConfig(cfg MultiRunConfig) error {
	if cfg.Runs < MinRecommendedRuns {
		return fmt.Errorf("aggregator: runs must be >= %d, got %d", MinRecommendedRuns, cfg.Runs)
	}
	if cfg.RunFunc == nil {
		return fmt.Errorf("aggregator: RunFunc is required")
	}
	return nil
}

func aggregateMetrics(metrics []Metrics) AggregatedMetrics {
	n := len(metrics)
	totalReturn := make([]float64, n)
	maxDrawdown := make([]float64, n)
	calmarRatio := make([]float64, n)
	sharpeRatio := make([]float64, n)
	sortinoRatio := make([]float64, n)
	winRate := make([]float64, n)
	profitFactor := make([]float64, n)
	avgWinLoss := make([]float64, n)
	volatility := make([]float64, n)
	endEquity := make([]float64, n)
	realizedPnL := make([]float64, n)
	unrealizedPnL := make([]float64, n)

	for i, m := range metrics {
		totalReturn[i] = m.TotalReturn
		maxDrawdown[i] = m.MaxDrawdown
		calmarRatio[i] = m.CalmarRatio
		sharpeRatio[i] = m.SharpeRatio
		sortinoRatio[i] = m.SortinoRatio
		winRate[i] = m.WinRate
		profitFactor[i] = m.ProfitFactor
		avgWinLoss[i] = m.AvgWinLossRatio
		volatility[i] = m.Volatility
		endEquity[i] = m.EndEquity
		realizedPnL[i] = m.RealizedPnL
		unrealizedPnL[i] = m.UnrealizedPnL
	}

	return AggregatedMetrics{
		TotalReturn:     computeStats(totalReturn),
		MaxDrawdown:     computeStats(maxDrawdown),
		CalmarRatio:     computeStats(calmarRatio),
		SharpeRatio:     computeStats(sharpeRatio),
		SortinoRatio:    computeStats(sortinoRatio),
		WinRate:         computeStats(winRate),
		ProfitFactor:    computeStats(profitFactor),
		AvgWinLossRatio: computeStats(avgWinLoss),
		Volatility:      computeStats(volatility),
		EndEquity:       computeStats(endEquity),
		RealizedPnL:     computeStats(realizedPnL),
		UnrealizedPnL:   computeStats(unrealizedPnL),
	}
}

func computeStats(values []float64) MetricStats {
	if len(values) == 0 {
		return MetricStats{}
	}

	// Filter out non-finite values (Inf, NaN) that can arise from metrics
	// like ProfitFactor or AvgWinLossRatio.
	finite := make([]float64, 0, len(values))
	for _, v := range values {
		if !math.IsInf(v, 0) && !math.IsNaN(v) {
			finite = append(finite, v)
		}
	}

	if len(finite) == 0 {
		return MetricStats{
			Mean:   math.NaN(),
			StdDev: math.NaN(),
			Min:    math.NaN(),
			Max:    math.NaN(),
			Values: values,
		}
	}

	avg := mean(finite)
	sd := stddev(finite, avg)

	minVal := finite[0]
	maxVal := finite[0]
	for _, v := range finite[1:] {
		if v < minVal {
			minVal = v
		}
		if v > maxVal {
			maxVal = v
		}
	}

	return MetricStats{
		Mean:   avg,
		StdDev: sd,
		Min:    minVal,
		Max:    maxVal,
		Values: values,
	}
}

// tKey identifies a (degrees-of-freedom, confidence-level) pair in the
// t-distribution lookup table.
type tKey struct {
	df    int
	level float64
}

// tTable holds two-tailed t-distribution critical values for common
// confidence levels and small degrees of freedom.
var tTable = map[tKey]float64{
	{1, 0.90}: 6.314, {1, 0.95}: 12.706, {1, 0.99}: 63.657,
	{2, 0.90}: 2.920, {2, 0.95}: 4.303, {2, 0.99}: 9.925,
	{3, 0.90}: 2.353, {3, 0.95}: 3.182, {3, 0.99}: 5.841,
	{4, 0.90}: 2.132, {4, 0.95}: 2.776, {4, 0.99}: 4.604,
	{5, 0.90}: 2.015, {5, 0.95}: 2.571, {5, 0.99}: 4.032,
	{6, 0.90}: 1.943, {6, 0.95}: 2.447, {6, 0.99}: 3.707,
	{7, 0.90}: 1.895, {7, 0.95}: 2.365, {7, 0.99}: 3.499,
	{8, 0.90}: 1.860, {8, 0.95}: 2.306, {8, 0.99}: 3.355,
	{9, 0.90}: 1.833, {9, 0.95}: 2.262, {9, 0.99}: 3.250,
	{10, 0.90}: 1.812, {10, 0.95}: 2.228, {10, 0.99}: 3.169,
	{15, 0.90}: 1.753, {15, 0.95}: 2.131, {15, 0.99}: 2.947,
	{20, 0.90}: 1.725, {20, 0.95}: 2.086, {20, 0.99}: 2.845,
	{25, 0.90}: 1.708, {25, 0.95}: 2.060, {25, 0.99}: 2.787,
	{30, 0.90}: 1.697, {30, 0.95}: 2.042, {30, 0.99}: 2.750,
}

// zValues holds z-approximation critical values used for df > 30.
var zValues = map[float64]float64{
	0.90: 1.645,
	0.95: 1.960,
	0.99: 2.576,
}

// tTableDF lists the degrees of freedom present in tTable, sorted ascending.
var tTableDF = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30}

// tCritical returns the two-tailed t-distribution critical value for the
// given degrees of freedom and confidence level. Only 0.90, 0.95, and 0.99
// are supported. For df > 30 the z-approximation is used.
func tCritical(df int, level float64) (float64, bool) {
	if df < 1 {
		return 0, false
	}

	if df > 30 {
		z, ok := zValues[level]
		return z, ok
	}

	// Try exact lookup.
	if v, ok := tTable[tKey{df, level}]; ok {
		return v, true
	}

	// For unlisted df, use the next lower available df in the table
	// (conservative estimate).
	best := -1
	for _, d := range tTableDF {
		if d <= df {
			best = d
		}
	}
	if best < 0 {
		return 0, false
	}
	v, ok := tTable[tKey{best, level}]
	return v, ok
}
