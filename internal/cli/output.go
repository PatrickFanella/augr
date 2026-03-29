package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/api"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

type portfolioOutput struct {
	Summary   portfolioSummary  `json:"summary"`
	Positions []domain.Position `json:"positions"`
}

type portfolioSummary struct {
	OpenPositions int     `json:"open_positions"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	RealizedPnL   float64 `json:"realized_pnl"`
}

type runOutput struct {
	Strategy domain.Strategy       `json:"strategy"`
	Result   api.StrategyRunResult `json:"result"`
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeTable(w io.Writer, headers []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := fmt.Fprintln(tw, strings.Join(row, "\t")); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func renderStrategiesTable(w io.Writer, strategies []domain.Strategy) error {
	rows := make([][]string, 0, len(strategies))
	for _, strategy := range strategies {
		rows = append(rows, []string{
			strategy.ID.String(),
			strategy.Name,
			strategy.Ticker,
			strategy.MarketType.String(),
			strconv.FormatBool(strategy.IsActive),
			strconv.FormatBool(strategy.IsPaper),
			formatTime(strategy.UpdatedAt),
		})
	}
	return writeTable(w, []string{"ID", "NAME", "TICKER", "MARKET", "ACTIVE", "PAPER", "UPDATED"}, rows)
}

func renderPortfolioTable(w io.Writer, output portfolioOutput) error {
	if err := writeTable(w, []string{"METRIC", "VALUE"}, [][]string{
		{"Open positions", strconv.Itoa(output.Summary.OpenPositions)},
		{"Unrealized P&L", formatFloat(output.Summary.UnrealizedPnL)},
		{"Realized P&L", formatFloat(output.Summary.RealizedPnL)},
	}); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}

	rows := make([][]string, 0, len(output.Positions))
	for _, position := range output.Positions {
		rows = append(rows, []string{
			position.ID.String(),
			position.Ticker,
			position.Side.String(),
			formatFloat(position.Quantity),
			formatFloat(position.AvgEntry),
			formatOptionalFloat(position.CurrentPrice),
			formatOptionalFloat(position.UnrealizedPnL),
			formatFloat(position.RealizedPnL),
			formatTime(position.OpenedAt),
		})
	}
	return writeTable(w, []string{"ID", "TICKER", "SIDE", "QTY", "AVG ENTRY", "CURRENT", "UNREALIZED", "REALIZED", "OPENED"}, rows)
}

func renderRiskStatusTable(w io.Writer, status risk.EngineStatus) error {
	return writeTable(w, []string{"FIELD", "VALUE"}, [][]string{
		{"Risk status", status.RiskStatus.String()},
		{"Circuit breaker state", status.CircuitBreaker.State.String()},
		{"Circuit breaker reason", emptyDash(status.CircuitBreaker.Reason)},
		{"Kill switch active", strconv.FormatBool(status.KillSwitch.Active)},
		{"Kill switch reason", emptyDash(status.KillSwitch.Reason)},
		{"Kill switch mechanisms", formatMechanisms(status.KillSwitch.Mechanisms)},
		{"Max per position", formatPercent(status.PositionLimits.MaxPerPositionPct)},
		{"Max total", formatPercent(status.PositionLimits.MaxTotalPct)},
		{"Max concurrent", strconv.Itoa(status.PositionLimits.MaxConcurrent)},
		{"Max per market", formatPercent(status.PositionLimits.MaxPerMarketPct)},
		{"Updated", formatTime(status.UpdatedAt)},
	})
}

func renderRunTable(w io.Writer, output runOutput) error {
	return writeTable(w, []string{"FIELD", "VALUE"}, [][]string{
		{"Strategy", output.Strategy.Name},
		{"Ticker", output.Strategy.Ticker},
		{"Strategy ID", output.Strategy.ID.String()},
		{"Run ID", output.Result.Run.ID.String()},
		{"Status", output.Result.Run.Status.String()},
		{"Signal", output.Result.Signal.String()},
		{"Orders", strconv.Itoa(len(output.Result.Orders))},
		{"Positions", strconv.Itoa(len(output.Result.Positions))},
		{"Started", formatTime(output.Result.Run.StartedAt)},
		{"Completed", formatOptionalTime(output.Result.Run.CompletedAt)},
	})
}

func renderMemoriesTable(w io.Writer, memories []domain.AgentMemory) error {
	rows := make([][]string, 0, len(memories))
	for _, memory := range memories {
		rows = append(rows, []string{
			memory.ID.String(),
			memory.AgentRole.String(),
			truncate(memory.Situation, 32),
			truncate(memory.Recommendation, 32),
			formatOptionalFloat(memory.RelevanceScore),
			formatTime(memory.CreatedAt),
		})
	}
	return writeTable(w, []string{"ID", "ROLE", "SITUATION", "RECOMMENDATION", "SCORE", "CREATED"}, rows)
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', 2, 64)
}

func formatOptionalFloat(value *float64) string {
	if value == nil {
		return "-"
	}
	return formatFloat(*value)
}

func formatPercent(value float64) string {
	return formatFloat(value*100) + "%"
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "-"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatOptionalTime(value *time.Time) string {
	if value == nil {
		return "-"
	}
	return formatTime(*value)
}

func truncate(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + "…"
}

func emptyDash(value string) string {
	if strings.TrimSpace(value) == "" {
		return "-"
	}
	return value
}

func formatMechanisms(mechanisms []risk.KillSwitchMechanism) string {
	if len(mechanisms) == 0 {
		return "-"
	}
	items := make([]string, 0, len(mechanisms))
	for _, mechanism := range mechanisms {
		items = append(items, mechanism.String())
	}
	return strings.Join(items, ", ")
}
