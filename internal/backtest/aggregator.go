package backtest

import (
	"context"
	"fmt"
	"math"
	"strings"
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
	Runs                int
	Individual          []Metrics
	PromptVersions      []string
	PromptVersionHashes []string
	Aggregated          AggregatedMetrics
}

// PromptVariantSummary identifies the prompt metadata associated with a set of
// aggregated backtest runs.
type PromptVariantSummary struct {
	PromptVersion     string
	PromptVersionHash string
	Runs              int
	Aggregated        AggregatedMetrics
}

// ConfidenceIntervalBounds stores the explicit interval used in a comparison report.
type ConfidenceIntervalBounds struct {
	Level float64
	Lower float64
	Upper float64
}

// PromptMetricComparison presents a single metric side-by-side for two prompt variants.
type PromptMetricComparison struct {
	Name                       string
	Left                       MetricStats
	Right                      MetricStats
	LeftConfidenceInterval     ConfidenceIntervalBounds
	RightConfidenceInterval    ConfidenceIntervalBounds
	MeanDelta                  float64
	ConfidenceIntervalsOverlap bool
}

// PromptABComparisonReport provides side-by-side aggregated metrics for two
// prompt variants evaluated on the same historical data.
type PromptABComparisonReport struct {
	Left            PromptVariantSummary
	Right           PromptVariantSummary
	ConfidenceLevel float64
	Metrics         []PromptMetricComparison
}

// MetricStats holds descriptive statistics for a single metric across runs.
type MetricStats struct {
	Mean   float64
	StdDev float64 // sample standard deviation (n-1 denominator)
	Min    float64
	Max    float64
	N      int       // effective sample size (finite values only)
	Values []float64 // all raw values including non-finite
}

// ConfidenceInterval returns the lower and upper bounds of a confidence
// interval using t-distribution critical values. Supported levels are
// 0.90, 0.95, and 0.99. An unsupported level returns (NaN, NaN).
// With fewer than 2 finite values the interval collapses to [Mean, Mean].
func (s MetricStats) ConfidenceInterval(level float64) (lower, upper float64) {
	if s.N < 2 {
		return s.Mean, s.Mean
	}

	t, ok := tCritical(s.N-1, level)
	if !ok {
		return math.NaN(), math.NaN()
	}

	se := s.StdDev / math.Sqrt(float64(s.N))
	margin := t * se
	return s.Mean - margin, s.Mean + margin
}

// AggregatedMetrics contains summary statistics for every numeric field of
// Metrics, computed across multiple backtest runs.
type AggregatedMetrics struct {
	TotalReturn      MetricStats
	BuyAndHoldReturn MetricStats
	MaxDrawdown      MetricStats
	CalmarRatio      MetricStats
	SharpeRatio      MetricStats
	SortinoRatio     MetricStats
	Alpha            MetricStats
	Beta             MetricStats
	InformationRatio MetricStats
	WinRate          MetricStats
	ProfitFactor     MetricStats
	AvgWinLossRatio  MetricStats
	Volatility       MetricStats
	StartEquity      MetricStats
	EndEquity        MetricStats
	RealizedPnL      MetricStats
	UnrealizedPnL    MetricStats
	TotalBars        MetricStats
}

// RunMulti executes cfg.RunFunc the specified number of times and aggregates
// the resulting metrics. It returns an error if the configuration is invalid
// or if any individual run fails.
func RunMulti(ctx context.Context, cfg MultiRunConfig) (*MultiRunResult, error) {
	if err := validateMultiRunConfig(cfg); err != nil {
		return nil, err
	}

	individual := make([]Metrics, 0, cfg.Runs)
	promptVersions := make([]string, 0, cfg.Runs)
	promptVersionHashes := make([]string, 0, cfg.Runs)
	for i := 0; i < cfg.Runs; i++ {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("aggregator: context cancelled before run %d: %w", i+1, err)
		}

		result, err := cfg.RunFunc(ctx)
		if err != nil {
			return nil, fmt.Errorf("aggregator: run %d failed: %w", i+1, err)
		}
		if result == nil {
			return nil, fmt.Errorf("aggregator: run %d returned nil result", i+1)
		}
		if err := validatePromptMetadata(result, i+1); err != nil {
			return nil, err
		}

		individual = append(individual, result.Metrics)
		promptVersions = append(promptVersions, result.PromptVersion)
		promptVersionHashes = append(promptVersionHashes, result.PromptVersionHash)
	}

	return &MultiRunResult{
		Runs:                cfg.Runs,
		Individual:          individual,
		PromptVersions:      promptVersions,
		PromptVersionHashes: promptVersionHashes,
		Aggregated:          aggregateMetrics(individual),
	}, nil
}

// ComparePromptVariants produces a side-by-side report for two prompt variants
// that were each aggregated via RunMulti.
func ComparePromptVariants(left, right *MultiRunResult, confidenceLevel float64) (*PromptABComparisonReport, error) {
	if left == nil || right == nil {
		return nil, fmt.Errorf("aggregator: both prompt variants are required")
	}
	if _, ok := tCritical(1, confidenceLevel); !ok {
		return nil, fmt.Errorf("aggregator: unsupported confidence level %.2f", confidenceLevel)
	}

	leftSummary, err := summarizePromptVariant(left)
	if err != nil {
		return nil, err
	}
	rightSummary, err := summarizePromptVariant(right)
	if err != nil {
		return nil, err
	}

	metricNames := []struct {
		name  string
		left  MetricStats
		right MetricStats
	}{
		{name: "TotalReturn", left: left.Aggregated.TotalReturn, right: right.Aggregated.TotalReturn},
		{name: "BuyAndHoldReturn", left: left.Aggregated.BuyAndHoldReturn, right: right.Aggregated.BuyAndHoldReturn},
		{name: "MaxDrawdown", left: left.Aggregated.MaxDrawdown, right: right.Aggregated.MaxDrawdown},
		{name: "CalmarRatio", left: left.Aggregated.CalmarRatio, right: right.Aggregated.CalmarRatio},
		{name: "SharpeRatio", left: left.Aggregated.SharpeRatio, right: right.Aggregated.SharpeRatio},
		{name: "SortinoRatio", left: left.Aggregated.SortinoRatio, right: right.Aggregated.SortinoRatio},
		{name: "Alpha", left: left.Aggregated.Alpha, right: right.Aggregated.Alpha},
		{name: "Beta", left: left.Aggregated.Beta, right: right.Aggregated.Beta},
		{name: "InformationRatio", left: left.Aggregated.InformationRatio, right: right.Aggregated.InformationRatio},
		{name: "WinRate", left: left.Aggregated.WinRate, right: right.Aggregated.WinRate},
		{name: "ProfitFactor", left: left.Aggregated.ProfitFactor, right: right.Aggregated.ProfitFactor},
		{name: "AvgWinLossRatio", left: left.Aggregated.AvgWinLossRatio, right: right.Aggregated.AvgWinLossRatio},
		{name: "Volatility", left: left.Aggregated.Volatility, right: right.Aggregated.Volatility},
		{name: "StartEquity", left: left.Aggregated.StartEquity, right: right.Aggregated.StartEquity},
		{name: "EndEquity", left: left.Aggregated.EndEquity, right: right.Aggregated.EndEquity},
		{name: "RealizedPnL", left: left.Aggregated.RealizedPnL, right: right.Aggregated.RealizedPnL},
		{name: "UnrealizedPnL", left: left.Aggregated.UnrealizedPnL, right: right.Aggregated.UnrealizedPnL},
		{name: "TotalBars", left: left.Aggregated.TotalBars, right: right.Aggregated.TotalBars},
	}

	comparisons := make([]PromptMetricComparison, 0, len(metricNames))
	for _, metric := range metricNames {
		leftLower, leftUpper := metric.left.ConfidenceInterval(confidenceLevel)
		rightLower, rightUpper := metric.right.ConfidenceInterval(confidenceLevel)
		comparisons = append(comparisons, PromptMetricComparison{
			Name:  metric.name,
			Left:  metric.left,
			Right: metric.right,
			LeftConfidenceInterval: ConfidenceIntervalBounds{
				Level: confidenceLevel,
				Lower: leftLower,
				Upper: leftUpper,
			},
			RightConfidenceInterval: ConfidenceIntervalBounds{
				Level: confidenceLevel,
				Lower: rightLower,
				Upper: rightUpper,
			},
			MeanDelta:                  metric.right.Mean - metric.left.Mean,
			ConfidenceIntervalsOverlap: intervalsOverlap(leftLower, leftUpper, rightLower, rightUpper),
		})
	}

	return &PromptABComparisonReport{
		Left:            leftSummary,
		Right:           rightSummary,
		ConfidenceLevel: confidenceLevel,
		Metrics:         comparisons,
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

func validatePromptMetadata(result *OrchestratorResult, runIndex int) error {
	if strings.TrimSpace(result.PromptVersion) == "" {
		return fmt.Errorf("aggregator: run %d missing prompt_version metadata", runIndex)
	}
	if strings.TrimSpace(result.PromptVersionHash) == "" {
		return fmt.Errorf("aggregator: run %d missing prompt_version_hash metadata", runIndex)
	}
	return nil
}

func summarizePromptVariant(result *MultiRunResult) (PromptVariantSummary, error) {
	version, err := uniquePromptField("prompt_version", result.PromptVersions)
	if err != nil {
		return PromptVariantSummary{}, err
	}
	hash, err := uniquePromptField("prompt_version_hash", result.PromptVersionHashes)
	if err != nil {
		return PromptVariantSummary{}, err
	}
	return PromptVariantSummary{
		PromptVersion:     version,
		PromptVersionHash: hash,
		Runs:              result.Runs,
		Aggregated:        result.Aggregated,
	}, nil
}

func uniquePromptField(name string, values []string) (string, error) {
	if len(values) == 0 {
		return "", fmt.Errorf("aggregator: %s metadata is required", name)
	}
	base := values[0]
	if strings.TrimSpace(base) == "" {
		return "", fmt.Errorf("aggregator: %s metadata is required", name)
	}
	for _, value := range values[1:] {
		if value != base {
			return "", fmt.Errorf("aggregator: %s must be identical across runs", name)
		}
	}
	return base, nil
}

func intervalsOverlap(leftLower, leftUpper, rightLower, rightUpper float64) bool {
	if math.IsNaN(leftLower) || math.IsNaN(leftUpper) || math.IsNaN(rightLower) || math.IsNaN(rightUpper) {
		return false
	}
	return leftLower <= rightUpper && rightLower <= leftUpper
}

func aggregateMetrics(metrics []Metrics) AggregatedMetrics {
	n := len(metrics)
	totalReturn := make([]float64, n)
	buyAndHoldReturn := make([]float64, n)
	maxDrawdown := make([]float64, n)
	calmarRatio := make([]float64, n)
	sharpeRatio := make([]float64, n)
	sortinoRatio := make([]float64, n)
	alpha := make([]float64, n)
	beta := make([]float64, n)
	informationRatio := make([]float64, n)
	winRate := make([]float64, n)
	profitFactor := make([]float64, n)
	avgWinLoss := make([]float64, n)
	volatility := make([]float64, n)
	startEquity := make([]float64, n)
	endEquity := make([]float64, n)
	realizedPnL := make([]float64, n)
	unrealizedPnL := make([]float64, n)
	totalBars := make([]float64, n)

	for i, m := range metrics {
		totalReturn[i] = m.TotalReturn
		buyAndHoldReturn[i] = m.BuyAndHoldReturn
		maxDrawdown[i] = m.MaxDrawdown
		calmarRatio[i] = m.CalmarRatio
		sharpeRatio[i] = m.SharpeRatio
		sortinoRatio[i] = m.SortinoRatio
		alpha[i] = m.Alpha
		beta[i] = m.Beta
		informationRatio[i] = m.InformationRatio
		winRate[i] = m.WinRate
		profitFactor[i] = m.ProfitFactor
		avgWinLoss[i] = m.AvgWinLossRatio
		volatility[i] = m.Volatility
		startEquity[i] = m.StartEquity
		endEquity[i] = m.EndEquity
		realizedPnL[i] = m.RealizedPnL
		unrealizedPnL[i] = m.UnrealizedPnL
		totalBars[i] = float64(m.TotalBars)
	}

	return AggregatedMetrics{
		TotalReturn:      computeStats(totalReturn),
		BuyAndHoldReturn: computeStats(buyAndHoldReturn),
		MaxDrawdown:      computeStats(maxDrawdown),
		CalmarRatio:      computeStats(calmarRatio),
		SharpeRatio:      computeStats(sharpeRatio),
		SortinoRatio:     computeStats(sortinoRatio),
		Alpha:            computeStats(alpha),
		Beta:             computeStats(beta),
		InformationRatio: computeStats(informationRatio),
		WinRate:          computeStats(winRate),
		ProfitFactor:     computeStats(profitFactor),
		AvgWinLossRatio:  computeStats(avgWinLoss),
		Volatility:       computeStats(volatility),
		StartEquity:      computeStats(startEquity),
		EndEquity:        computeStats(endEquity),
		RealizedPnL:      computeStats(realizedPnL),
		UnrealizedPnL:    computeStats(unrealizedPnL),
		TotalBars:        computeStats(totalBars),
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
			N:      0,
			Values: values,
		}
	}

	avg := mean(finite)
	sd := sampleStddev(finite, avg)

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
		N:      len(finite),
		Values: values,
	}
}

// sampleStddev computes the sample standard deviation (Bessel-corrected,
// n-1 denominator) given a pre-computed mean. Returns 0 when len < 2.
func sampleStddev(values []float64, avg float64) float64 {
	if len(values) < 2 {
		return 0
	}
	var sumSq float64
	for _, v := range values {
		d := v - avg
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)-1))
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
