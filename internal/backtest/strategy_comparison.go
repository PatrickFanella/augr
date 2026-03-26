package backtest

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
)

// StrategyComparisonStrategy defines one strategy variant to run under the
// shared market data and execution conditions established by the parent
// orchestrator.
type StrategyComparisonStrategy struct {
	Name         string
	StrategyID   uuid.UUID
	Pipeline     *agent.Pipeline
	ClockTargets []NowFuncSetter
}

// StrategyComparisonConfig configures a head-to-head comparison across
// multiple strategies on the same date range.
type StrategyComparisonConfig struct {
	Strategies []StrategyComparisonStrategy
}

// StrategyComparisonEntry contains the full result for one compared strategy.
type StrategyComparisonEntry struct {
	Name       string
	StrategyID uuid.UUID
	Result     *OrchestratorResult
}

// MetricComparisonRow contains one metric's values across all compared
// strategies.
type MetricComparisonRow struct {
	Metric string
	Values []float64
}

// MetricComparisonTable is a side-by-side KPI table for compared strategies.
type MetricComparisonTable struct {
	Headers []string
	Rows    []MetricComparisonRow
}

// StrategyComparisonResult contains the compared strategy results and shared
// market context.
type StrategyComparisonResult struct {
	Ticker     string
	StartDate  time.Time
	EndDate    time.Time
	Strategies []StrategyComparisonEntry
}

// RunStrategyComparison executes multiple strategy pipelines against the same
// bars, date range, fill configuration, and starting capital so their metrics
// can be compared under identical conditions.
func (o *Orchestrator) RunStrategyComparison(ctx context.Context, cfg StrategyComparisonConfig) (*StrategyComparisonResult, error) {
	if err := validateStrategyComparisonConfig(cfg); err != nil {
		return nil, err
	}

	results := make([]StrategyComparisonEntry, 0, len(cfg.Strategies))
	for i, strategy := range cfg.Strategies {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("backtest: strategy comparison cancelled before strategy %d: %w", i+1, err)
		}

		strategyCfg := o.config
		strategyCfg.StrategyID = strategy.StrategyID

		clockTargets := append([]NowFuncSetter(nil), o.clockTargets...)
		clockTargets = append(clockTargets, strategy.ClockTargets...)

		orchestrator, err := NewOrchestrator(strategyCfg, o.bars, strategy.Pipeline, o.logger, clockTargets...)
		if err != nil {
			return nil, fmt.Errorf("backtest: creating orchestrator for strategy %q: %w", strategy.Name, err)
		}

		result, err := orchestrator.Run(ctx)
		if err != nil {
			return nil, fmt.Errorf("backtest: running strategy %q: %w", strategy.Name, err)
		}

		results = append(results, StrategyComparisonEntry{
			Name:       strategy.Name,
			StrategyID: strategy.StrategyID,
			Result:     result,
		})
	}

	return &StrategyComparisonResult{
		Ticker:     o.config.Ticker,
		StartDate:  o.config.StartDate,
		EndDate:    o.config.EndDate,
		Strategies: results,
	}, nil
}

// MetricTable returns the side-by-side KPI comparison table for all compared
// strategies.
func (r StrategyComparisonResult) MetricTable() MetricComparisonTable {
	inputs := make([]MetricComparisonInput, 0, len(r.Strategies))
	for _, strategy := range r.Strategies {
		input := MetricComparisonInput{
			Name: strategy.Name,
		}
		if strategy.Result != nil {
			input.Metrics = &strategy.Result.Metrics
		}
		inputs = append(inputs, input)
	}
	return BuildMetricComparisonTable(inputs)
}

// FormatMetricTable renders the metric comparison as a plain-text table.
func (r StrategyComparisonResult) FormatMetricTable() string {
	table := r.MetricTable()
	if len(table.Headers) == 0 {
		return ""
	}

	var builder strings.Builder
	writer := tabwriter.NewWriter(&builder, 0, 0, 2, ' ', 0)

	for i, header := range table.Headers {
		if i > 0 {
			_, _ = writer.Write([]byte{'\t'})
		}
		_, _ = fmt.Fprint(writer, sanitizeComparisonCell(header))
	}
	_, _ = fmt.Fprintln(writer)

	for _, row := range table.Rows {
		_, _ = fmt.Fprint(writer, sanitizeComparisonCell(row.Metric))
		for _, value := range row.Values {
			_, _ = fmt.Fprintf(writer, "\t%s", formatComparisonMetricValue(value))
		}
		_, _ = fmt.Fprintln(writer)
	}

	_ = writer.Flush()
	return builder.String()
}

func validateStrategyComparisonConfig(cfg StrategyComparisonConfig) error {
	if len(cfg.Strategies) < 2 {
		return fmt.Errorf("backtest: strategy comparison requires at least 2 strategies")
	}
	for i, strategy := range cfg.Strategies {
		if strings.TrimSpace(strategy.Name) == "" {
			return fmt.Errorf("backtest: strategy %d name is required", i+1)
		}
		if strategy.StrategyID == uuid.Nil {
			return fmt.Errorf("backtest: strategy %q ID is required", strategy.Name)
		}
		if strategy.Pipeline == nil {
			return fmt.Errorf("backtest: strategy %q pipeline is required", strategy.Name)
		}
	}
	return nil
}

func formatComparisonMetricValue(value float64) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "+Inf"
	case math.IsInf(value, -1):
		return "-Inf"
	default:
		return strconv.FormatFloat(value, 'f', -1, 64)
	}
}

var comparisonCellReplacer = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ")

func sanitizeComparisonCell(value string) string {
	return strings.TrimSpace(comparisonCellReplacer.Replace(value))
}
