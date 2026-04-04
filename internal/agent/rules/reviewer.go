package rules

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const reviewSystemPrompt = `You are a trading signal reviewer. A quantitative rules engine has generated a trading signal based on technical indicators. Your job is to review it against the current market context and decide whether to confirm, modify, or veto the trade.

Respond with JSON only:
{
  "verdict": "confirm" | "modify" | "veto",
  "confidence": <float 0.0 to 1.0>,
  "adjusted_position_size": <float, 0 to keep original>,
  "adjusted_stop_loss": <float, 0 to keep original>,
  "reasoning": "<one or two sentences>"
}

Verdicts:
- "confirm": the signal looks sound, execute as proposed
- "modify": execute but with your adjusted position_size and/or stop_loss
- "veto": do not execute — the signal is likely a false positive or conditions are unfavorable`

// ReviewVerdict is the LLM's response to a signal review request.
type ReviewVerdict struct {
	Verdict              string  `json:"verdict"`
	Confidence           float64 `json:"confidence"`
	AdjustedPositionSize float64 `json:"adjusted_position_size"`
	AdjustedStopLoss     float64 `json:"adjusted_stop_loss"`
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

// Review asks the LLM to evaluate a rules-engine signal. It returns the verdict
// and applies any modifications to the plan. Returns true if the trade should
// proceed (confirm or modify), false if vetoed.
func (r *SignalReviewer) Review(ctx context.Context, plan *agent.TradingPlan, indicators []domain.Indicator, bar domain.OHLCV) bool {
	userPrompt := buildReviewPrompt(plan, indicators, bar)

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
		plan.Confidence = verdict.Confidence
		plan.Rationale = plan.Rationale + " | LLM: " + verdict.Reasoning
		return true
	default: // "confirm"
		plan.Confidence = verdict.Confidence
		plan.Rationale = plan.Rationale + " | LLM: " + verdict.Reasoning
		return true
	}
}

func buildReviewPrompt(plan *agent.TradingPlan, indicators []domain.Indicator, bar domain.OHLCV) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Signal: %s %s at $%.2f\n", plan.Action, plan.Ticker, plan.EntryPrice)
	fmt.Fprintf(&b, "Position Size: %.2f shares\n", plan.PositionSize)
	fmt.Fprintf(&b, "Stop Loss: $%.2f | Take Profit: $%.2f\n", plan.StopLoss, plan.TakeProfit)
	fmt.Fprintf(&b, "Rules Rationale: %s\n\n", plan.Rationale)
	fmt.Fprintf(&b, "Current Bar: O=%.2f H=%.2f L=%.2f C=%.2f V=%.0f\n\n", bar.Open, bar.High, bar.Low, bar.Close, bar.Volume)
	fmt.Fprintf(&b, "Key Indicators:\n")
	for _, ind := range indicators {
		fmt.Fprintf(&b, "  %s = %.4f\n", ind.Name, ind.Value)
	}
	return b.String()
}
