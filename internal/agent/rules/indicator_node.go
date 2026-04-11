package rules

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// IndicatorAnalystNode computes technical indicators from OHLCV bars without
// calling an LLM. It is stateful: it holds the full bar slice and advances a
// cursor on each Execute call (one call per bar in the backtest loop).
// When startDate is provided, the cursor starts at the first bar on or after
// that date, so earlier bars serve as indicator warmup (e.g., SMA-200 needs
// 200 bars of history before the first signal can fire).
type IndicatorAnalystNode struct {
	bars        []domain.OHLCV
	cursor      int
	startCursor int
	logger      *slog.Logger
}

// NewIndicatorAnalystNode creates an indicator node. If startDate is non-zero,
// the cursor is positioned at the first bar on or after startDate so that
// preceding bars provide indicator warmup history.
func NewIndicatorAnalystNode(bars []domain.OHLCV, startDate time.Time, logger *slog.Logger) *IndicatorAnalystNode {
	if logger == nil {
		logger = slog.Default()
	}
	cursor := 0
	if !startDate.IsZero() {
		for i, bar := range bars {
			if !bar.Timestamp.Before(startDate) {
				cursor = i
				break
			}
		}
	}
	return &IndicatorAnalystNode{bars: bars, cursor: cursor, startCursor: cursor, logger: logger}
}

func (n *IndicatorAnalystNode) Name() string          { return "indicator_analyst" }
func (n *IndicatorAnalystNode) Role() agent.AgentRole { return agent.AgentRoleMarketAnalyst }
func (n *IndicatorAnalystNode) Phase() agent.Phase    { return agent.PhaseAnalysis }

// Execute computes indicators from bars[:cursor+1] and populates state.Market.
// If the cursor is exhausted (e.g., walk-forward reuse), it wraps back to the
// start position so the node can be reused across multiple windows.
func (n *IndicatorAnalystNode) Execute(_ context.Context, state *agent.PipelineState) error {
	if n.cursor >= len(n.bars) {
		n.cursor = n.startCursor
	}
	barsToDate := n.bars[:n.cursor+1]
	bar := n.bars[n.cursor]
	n.cursor++

	indicators := data.IndicatorSnapshotFromBars(barsToDate)
	state.Market = &agent.MarketData{
		Bars:       barsToDate,
		Indicators: indicators,
	}
	state.SetAnalystReport(agent.AgentRoleMarketAnalyst, formatIndicatorSummary(indicators, bar))
	return nil
}

// Reset resets the cursor to the beginning, allowing the node to be reused.
func (n *IndicatorAnalystNode) Reset() { n.cursor = 0 }

func formatIndicatorSummary(indicators []domain.Indicator, bar domain.OHLCV) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Bar: O=%.2f H=%.2f L=%.2f C=%.2f V=%.0f\n", bar.Open, bar.High, bar.Low, bar.Close, bar.Volume)
	for _, ind := range indicators {
		fmt.Fprintf(&b, "%s=%.4f ", ind.Name, ind.Value)
	}
	return b.String()
}
