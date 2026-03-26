package backtest

import (
	"context"
	"io"
	"log/slog"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestRunStrategyComparisonRejectsInvalidConfig(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	start := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2024, 1, 3, 0, 0, 0, 0, time.UTC)

	orch, err := NewOrchestrator(
		OrchestratorConfig{
			StrategyID:  uuid.New(),
			Ticker:      "AAPL",
			StartDate:   start,
			EndDate:     end,
			InitialCash: 100_000,
			FillConfig: FillConfig{
				Slippage: ProportionalSlippage{BasisPoints: 0},
			},
		},
		makeDailyBars(start, end),
		makePipelineWithLogger(logger),
		logger,
	)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	testCases := []struct {
		name string
		cfg  StrategyComparisonConfig
	}{
		{
			name: "requires at least two strategies",
			cfg: StrategyComparisonConfig{
				Strategies: []StrategyComparisonStrategy{
					{Name: "Growth", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
				},
			},
		},
		{
			name: "requires strategy name",
			cfg: StrategyComparisonConfig{
				Strategies: []StrategyComparisonStrategy{
					{Name: "", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
					{Name: "Value", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
				},
			},
		},
		{
			name: "requires strategy id",
			cfg: StrategyComparisonConfig{
				Strategies: []StrategyComparisonStrategy{
					{Name: "Growth", StrategyID: uuid.Nil, Pipeline: makePipelineWithLogger(logger)},
					{Name: "Value", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
				},
			},
		},
		{
			name: "requires pipeline",
			cfg: StrategyComparisonConfig{
				Strategies: []StrategyComparisonStrategy{
					{Name: "Growth", StrategyID: uuid.New(), Pipeline: nil},
					{Name: "Value", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
				},
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := orch.RunStrategyComparison(context.Background(), tc.cfg)
			if err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestOrchestratorRunStrategyComparisonUsesSharedConditions(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	barsStart := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	barsEnd := time.Date(2024, 1, 6, 0, 0, 0, 0, time.UTC)
	runStart := time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC)
	runEnd := time.Date(2024, 1, 4, 0, 0, 0, 0, time.UTC)

	orch, err := NewOrchestrator(
		OrchestratorConfig{
			StrategyID:  uuid.New(),
			Ticker:      "AAPL",
			StartDate:   runStart,
			EndDate:     runEnd,
			InitialCash: 100_000,
			FillConfig: FillConfig{
				Slippage: ProportionalSlippage{BasisPoints: 0},
			},
		},
		makeDailyBars(barsStart, barsEnd),
		makePipelineWithLogger(logger),
		logger,
	)
	if err != nil {
		t.Fatalf("NewOrchestrator() error = %v", err)
	}

	result, err := orch.RunStrategyComparison(context.Background(), StrategyComparisonConfig{
		Strategies: []StrategyComparisonStrategy{
			{Name: "Growth", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
			{Name: "Value", StrategyID: uuid.New(), Pipeline: makePipelineWithLogger(logger)},
		},
	})
	if err != nil {
		t.Fatalf("RunStrategyComparison() error = %v", err)
	}

	if len(result.Strategies) != 2 {
		t.Fatalf("len(result.Strategies) = %d, want 2", len(result.Strategies))
	}
	if result.Ticker != "AAPL" {
		t.Fatalf("result.Ticker = %q, want AAPL", result.Ticker)
	}
	if !result.StartDate.Equal(runStart) {
		t.Fatalf("result.StartDate = %v, want %v", result.StartDate, runStart)
	}
	if !result.EndDate.Equal(runEnd) {
		t.Fatalf("result.EndDate = %v, want %v", result.EndDate, runEnd)
	}

	first := result.Strategies[0].Result.Metrics
	second := result.Strategies[1].Result.Metrics
	if first.TotalBars != 3 || second.TotalBars != 3 {
		t.Fatalf("TotalBars = %d and %d, want 3 for both", first.TotalBars, second.TotalBars)
	}
	if !first.StartTime.Equal(runStart) || !second.StartTime.Equal(runStart) {
		t.Fatalf("StartTime = %v and %v, want %v", first.StartTime, second.StartTime, runStart)
	}
	if !first.EndTime.Equal(runEnd) || !second.EndTime.Equal(runEnd) {
		t.Fatalf("EndTime = %v and %v, want %v", first.EndTime, second.EndTime, runEnd)
	}
	if first.StartEquity != second.StartEquity {
		t.Fatalf("StartEquity mismatch: %f vs %f", first.StartEquity, second.StartEquity)
	}
	if len(result.Strategies[0].Result.BarResults) != len(result.Strategies[1].Result.BarResults) {
		t.Fatalf(
			"bar result length mismatch: %d vs %d",
			len(result.Strategies[0].Result.BarResults),
			len(result.Strategies[1].Result.BarResults),
		)
	}
}

func TestStrategyComparisonResultMetricTableAndFormatting(t *testing.T) {
	t.Parallel()

	result := StrategyComparisonResult{
		Strategies: []StrategyComparisonEntry{
			{
				Name: "Growth",
				Result: &OrchestratorResult{
					Metrics: Metrics{
						TotalReturn:      0.12,
						ProfitFactor:     math.Inf(1),
						EndEquity:        112_000,
						RealizedPnL:      12_000,
						UnrealizedPnL:    0,
						TotalBars:        20,
						BuyAndHoldReturn: 0.08,
					},
				},
			},
			{
				Name: "Value",
				Result: &OrchestratorResult{
					Metrics: Metrics{
						TotalReturn:      0.05,
						ProfitFactor:     1.4,
						EndEquity:        105_000,
						RealizedPnL:      5_000,
						UnrealizedPnL:    0,
						TotalBars:        20,
						BuyAndHoldReturn: 0.08,
					},
				},
			},
		},
	}

	table := result.MetricTable()
	if len(table.Headers) != 3 {
		t.Fatalf("len(table.Headers) = %d, want 3", len(table.Headers))
	}
	if table.Headers[0] != "Metric" || table.Headers[1] != "Growth" || table.Headers[2] != "Value" {
		t.Fatalf("unexpected headers: %#v", table.Headers)
	}
	if len(table.Rows) != len(metricComparisonSpecs) {
		t.Fatalf("len(table.Rows) = %d, want %d", len(table.Rows), len(metricComparisonSpecs))
	}

	totalReturnRow := findMetricComparisonRow(t, table.Rows, "Total Return")
	if len(totalReturnRow.Values) != 2 {
		t.Fatalf("len(totalReturnRow.Values) = %d, want 2", len(totalReturnRow.Values))
	}
	if totalReturnRow.Values[0] != 0.12 || totalReturnRow.Values[1] != 0.05 {
		t.Fatalf("unexpected Total Return values: %#v", totalReturnRow.Values)
	}

	totalBarsRow := findMetricComparisonRow(t, table.Rows, "Total Bars")
	if totalBarsRow.Values[0] != 20 || totalBarsRow.Values[1] != 20 {
		t.Fatalf("unexpected Total Bars values: %#v", totalBarsRow.Values)
	}

	formatted := result.FormatMetricTable()
	t.Logf("\n%s", formatted)
	for _, want := range []string{"Metric", "Growth", "Value", "Total Return", "0.12", "Total Bars", "20", "+Inf"} {
		if !strings.Contains(formatted, want) {
			t.Fatalf("formatted table missing %q:\n%s", want, formatted)
		}
	}
}

func findMetricComparisonRow(t *testing.T, rows []MetricComparisonRow, name string) MetricComparisonRow {
	t.Helper()
	for _, row := range rows {
		if row.Metric == name {
			return row
		}
	}
	t.Fatalf("metric row %q not found", name)
	return MetricComparisonRow{}
}
