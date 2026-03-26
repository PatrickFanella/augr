package backtest

import "math"

// MetricComparisonInput contains the display name and metrics for one side of a
// side-by-side comparison table.
type MetricComparisonInput struct {
	Name    string
	Metrics *Metrics
}

// MetricComparisonDiff summarizes a metric's deltas relative to the first
// compared entry in a metric comparison table.
type MetricComparisonDiff struct {
	Metric   string
	Baseline float64
	Deltas   []float64
}

type metricComparisonSpec struct {
	name  string
	value func(Metrics) float64
}

var metricComparisonSpecs = []metricComparisonSpec{
	{name: "Total Return", value: func(m Metrics) float64 { return m.TotalReturn }},
	{name: "Buy & Hold Return", value: func(m Metrics) float64 { return m.BuyAndHoldReturn }},
	{name: "Max Drawdown", value: func(m Metrics) float64 { return m.MaxDrawdown }},
	{name: "Calmar Ratio", value: func(m Metrics) float64 { return m.CalmarRatio }},
	{name: "Sharpe Ratio", value: func(m Metrics) float64 { return m.SharpeRatio }},
	{name: "Sortino Ratio", value: func(m Metrics) float64 { return m.SortinoRatio }},
	{name: "Alpha", value: func(m Metrics) float64 { return m.Alpha }},
	{name: "Beta", value: func(m Metrics) float64 { return m.Beta }},
	{name: "Information Ratio", value: func(m Metrics) float64 { return m.InformationRatio }},
	{name: "Win Rate", value: func(m Metrics) float64 { return m.WinRate }},
	{name: "Profit Factor", value: func(m Metrics) float64 { return m.ProfitFactor }},
	{name: "Avg Win/Loss Ratio", value: func(m Metrics) float64 { return m.AvgWinLossRatio }},
	{name: "Volatility", value: func(m Metrics) float64 { return m.Volatility }},
	{name: "Start Equity", value: func(m Metrics) float64 { return m.StartEquity }},
	{name: "End Equity", value: func(m Metrics) float64 { return m.EndEquity }},
	{name: "Realized PnL", value: func(m Metrics) float64 { return m.RealizedPnL }},
	{name: "Unrealized PnL", value: func(m Metrics) float64 { return m.UnrealizedPnL }},
	{name: "Total Bars", value: func(m Metrics) float64 { return float64(m.TotalBars) }},
}

// BuildMetricComparisonTable returns the aligned metric table for the provided
// entries.
func BuildMetricComparisonTable(inputs []MetricComparisonInput) MetricComparisonTable {
	headers := make([]string, 0, len(inputs)+1)
	headers = append(headers, "Metric")
	for _, input := range inputs {
		headers = append(headers, input.Name)
	}

	rows := make([]MetricComparisonRow, 0, len(metricComparisonSpecs))
	for _, spec := range metricComparisonSpecs {
		row := MetricComparisonRow{
			Metric: spec.name,
			Values: make([]float64, 0, len(inputs)),
		}
		for _, input := range inputs {
			if input.Metrics == nil {
				row.Values = append(row.Values, math.NaN())
				continue
			}
			row.Values = append(row.Values, spec.value(*input.Metrics))
		}
		rows = append(rows, row)
	}

	return MetricComparisonTable{
		Headers: headers,
		Rows:    rows,
	}
}

// BuildMetricComparisonDiffs returns per-metric deltas relative to the first
// compared run in the provided table.
func BuildMetricComparisonDiffs(table MetricComparisonTable) []MetricComparisonDiff {
	diffs := make([]MetricComparisonDiff, 0, len(table.Rows))
	for _, row := range table.Rows {
		diff := MetricComparisonDiff{
			Metric: row.Metric,
			Deltas: make([]float64, len(row.Values)),
		}
		if len(row.Values) == 0 {
			diff.Baseline = math.NaN()
			diffs = append(diffs, diff)
			continue
		}

		baseline := row.Values[0]
		diff.Baseline = baseline
		for i, value := range row.Values {
			if i == 0 {
				diff.Deltas[i] = 0
				continue
			}
			if math.IsNaN(baseline) || math.IsNaN(value) || math.IsInf(baseline, 0) || math.IsInf(value, 0) {
				diff.Deltas[i] = math.NaN()
				continue
			}
			diff.Deltas[i] = value - baseline
		}

		diffs = append(diffs, diff)
	}
	return diffs
}
