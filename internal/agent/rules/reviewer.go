package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const reviewSystemPrompt = `You are a senior trading signal reviewer at a quantitative fund. A rules engine has generated a trading signal based on technical indicators. You have access to the full market context — recent price history, all technical indicators, and the portfolio state.

Your job: decide whether to CONFIRM, MODIFY, or VETO the proposed trade.

Consider:
- Is the signal consistent with the broader price trend?
- Are there divergences between price action and momentum indicators?
- Is volume confirming the move?
- Are there signs of exhaustion, reversal patterns, or false breakouts in recent bars?
- Is the position size appropriate given the portfolio's cash balance and risk exposure?
- Would the stop loss survive normal price volatility (check ATR vs stop distance)?

Respond with JSON only:
{
  "verdict": "confirm" | "modify" | "veto",
  "confidence": <float 0.0 to 1.0>,
  "adjusted_position_size": <float, 0 to keep original>,
  "adjusted_stop_loss": <float, 0 to keep original>,
  "adjusted_take_profit": <float, 0 to keep original>,
  "reasoning": "<2-3 sentences explaining your decision with specific reference to the data>"
}

Verdicts:
- "confirm": the signal is sound and well-timed, execute as proposed
- "modify": the signal direction is correct but adjust sizing/stops for better risk management
- "veto": the signal is likely a false positive, conditions are unfavorable, or risk is too high`

// ReviewVerdict is the LLM's response to a signal review request.
type ReviewVerdict struct {
	Verdict              string  `json:"verdict"`
	Confidence           float64 `json:"confidence"`
	AdjustedPositionSize float64 `json:"adjusted_position_size"`
	AdjustedStopLoss     float64 `json:"adjusted_stop_loss"`
	AdjustedTakeProfit   float64 `json:"adjusted_take_profit"`
	Reasoning            string  `json:"reasoning"`
}

// SignalReviewer calls an LLM to confirm, modify, or veto a rules-engine signal.
type SignalReviewer struct {
	provider llm.Provider
	model    string
	logger   *slog.Logger
}

// NewSignalReviewer creates a reviewer that will call the given LLM provider.
func NewSignalReviewer(provider llm.Provider, model string, logger *slog.Logger) *SignalReviewer {
	if logger == nil {
		logger = slog.Default()
	}
	return &SignalReviewer{provider: provider, model: model, logger: logger}
}

// Review asks the LLM to evaluate a rules-engine signal with full market context.
// Returns true if the trade should proceed (confirm or modify), false if vetoed.
func (r *SignalReviewer) Review(ctx context.Context, plan *agent.TradingPlan, state *agent.PipelineState, bar domain.OHLCV, portfolioCash float64) bool {
	userPrompt := buildRichReviewPrompt(plan, state, bar, portfolioCash)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Model: r.model,
		Messages: []llm.Message{
			{Role: "system", Content: reviewSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		r.logger.Warn("rules/reviewer: LLM call failed, confirming signal by default",
			slog.Any("error", err),
		)
		return true
	}

	var verdict ReviewVerdict
	if err := json.Unmarshal([]byte(resp.Content), &verdict); err != nil {
		r.logger.Warn("rules/reviewer: failed to parse LLM response, confirming by default",
			slog.String("content", resp.Content),
			slog.Any("error", err),
		)
		return true
	}

	r.logger.Info("rules/reviewer: LLM verdict",
		slog.String("verdict", verdict.Verdict),
		slog.Float64("confidence", verdict.Confidence),
		slog.String("reasoning", verdict.Reasoning),
		slog.String("signal", plan.Action.String()),
		slog.String("ticker", plan.Ticker),
	)

	switch strings.ToLower(verdict.Verdict) {
	case "veto":
		return false
	case "modify":
		if verdict.AdjustedPositionSize > 0 {
			plan.PositionSize = verdict.AdjustedPositionSize
		}
		if verdict.AdjustedStopLoss > 0 {
			plan.StopLoss = verdict.AdjustedStopLoss
		}
		if verdict.AdjustedTakeProfit > 0 {
			plan.TakeProfit = verdict.AdjustedTakeProfit
		}
		plan.Confidence = verdict.Confidence
		plan.Rationale = plan.Rationale + " | LLM: " + verdict.Reasoning
		return true
	default: // "confirm"
		plan.Confidence = verdict.Confidence
		plan.Rationale = plan.Rationale + " | LLM: " + verdict.Reasoning
		return true
	}
}

func buildRichReviewPrompt(plan *agent.TradingPlan, state *agent.PipelineState, bar domain.OHLCV, portfolioCash float64) string {
	var b strings.Builder

	// Proposed trade
	fmt.Fprintf(&b, "=== PROPOSED TRADE ===\n")
	fmt.Fprintf(&b, "Signal: %s %s\n", strings.ToUpper(plan.Action.String()), plan.Ticker)
	fmt.Fprintf(&b, "Entry Price: $%.2f (market order)\n", plan.EntryPrice)
	fmt.Fprintf(&b, "Position Size: %.2f shares ($%.2f notional)\n", plan.PositionSize, plan.PositionSize*plan.EntryPrice)
	fmt.Fprintf(&b, "Stop Loss: $%.2f (%.1f%% from entry)\n", plan.StopLoss, math.Abs(plan.EntryPrice-plan.StopLoss)/plan.EntryPrice*100)
	fmt.Fprintf(&b, "Take Profit: $%.2f (%.1f%% from entry)\n", plan.TakeProfit, math.Abs(plan.TakeProfit-plan.EntryPrice)/plan.EntryPrice*100)
	fmt.Fprintf(&b, "Risk/Reward: %.2f\n", plan.RiskReward)
	fmt.Fprintf(&b, "Rules Rationale: %s\n\n", plan.Rationale)

	// Portfolio state
	fmt.Fprintf(&b, "=== PORTFOLIO ===\n")
	fmt.Fprintf(&b, "Available Cash: $%.2f\n", portfolioCash)
	notional := plan.PositionSize * plan.EntryPrice
	if portfolioCash > 0 {
		fmt.Fprintf(&b, "Trade as %% of Cash: %.1f%%\n", notional/portfolioCash*100)
	}
	fmt.Fprintf(&b, "\n")

	// Recent price history (last 20 bars)
	if state != nil && state.Market != nil && len(state.Market.Bars) > 0 {
		bars := state.Market.Bars
		start := len(bars) - 20
		if start < 0 {
			start = 0
		}
		recentBars := bars[start:]
		fmt.Fprintf(&b, "=== RECENT PRICE HISTORY (last %d bars) ===\n", len(recentBars))
		fmt.Fprintf(&b, "%-12s %8s %8s %8s %8s %12s\n", "Date", "Open", "High", "Low", "Close", "Volume")
		for _, bar := range recentBars {
			fmt.Fprintf(&b, "%-12s %8.2f %8.2f %8.2f %8.2f %12.0f\n",
				bar.Timestamp.Format("2006-01-02"), bar.Open, bar.High, bar.Low, bar.Close, bar.Volume)
		}

		// Price context
		currentClose := recentBars[len(recentBars)-1].Close
		highestHigh := 0.0
		lowestLow := math.MaxFloat64
		totalVolume := 0.0
		for _, b := range recentBars {
			if b.High > highestHigh {
				highestHigh = b.High
			}
			if b.Low < lowestLow {
				lowestLow = b.Low
			}
			totalVolume += b.Volume
		}
		avgVolume := totalVolume / float64(len(recentBars))
		fmt.Fprintf(&b, "\n20-bar range: $%.2f - $%.2f (%.1f%% width)\n",
			lowestLow, highestHigh, (highestHigh-lowestLow)/currentClose*100)
		fmt.Fprintf(&b, "Current bar volume vs 20-bar avg: %.0f vs %.0f (%.0f%%)\n\n",
			bar.Volume, avgVolume, bar.Volume/avgVolume*100)

		// Indicators
		if len(state.Market.Indicators) > 0 {
			fmt.Fprintf(&b, "=== TECHNICAL INDICATORS ===\n")

			// Group indicators for readability
			trendIndicators := []string{"sma_20", "sma_50", "sma_200", "ema_12"}
			momentumIndicators := []string{"rsi_14", "mfi_14", "williams_r_14", "stochastic_k", "stochastic_d", "cci_20", "roc_12"}
			macdIndicators := []string{"macd_line", "macd_signal", "macd_histogram"}
			volatilityIndicators := []string{"atr_14", "bollinger_upper", "bollinger_middle", "bollinger_lower"}
			volumeIndicators := []string{"vwma_20", "obv", "adl"}

			indMap := make(map[string]float64, len(state.Market.Indicators))
			for _, ind := range state.Market.Indicators {
				indMap[ind.Name] = ind.Value
			}

			writeGroup := func(name string, keys []string) {
				fmt.Fprintf(&b, "\n%s:\n", name)
				for _, k := range keys {
					if v, ok := indMap[k]; ok {
						fmt.Fprintf(&b, "  %-20s = %12.4f", k, v)
						// Add context for key indicators
						if k == "rsi_14" {
							if v < 30 {
								fmt.Fprintf(&b, "  (OVERSOLD)")
							} else if v > 70 {
								fmt.Fprintf(&b, "  (OVERBOUGHT)")
							}
						}
						if strings.HasPrefix(k, "sma_") || k == "ema_12" {
							if currentClose > v {
								fmt.Fprintf(&b, "  (price ABOVE)")
							} else {
								fmt.Fprintf(&b, "  (price BELOW)")
							}
						}
						fmt.Fprintf(&b, "\n")
					}
				}
			}

			writeGroup("Trend", trendIndicators)
			writeGroup("Momentum", momentumIndicators)
			writeGroup("MACD", macdIndicators)
			writeGroup("Volatility", volatilityIndicators)
			writeGroup("Volume", volumeIndicators)
		}
	}

	return b.String()
}
