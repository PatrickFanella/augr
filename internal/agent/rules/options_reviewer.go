package rules

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const optionsEntrySystemPrompt = `You are a senior options strategist. A rules engine has selected an options spread based on technical indicators and IV conditions. Review the proposed trade.

You will receive:
- The proposed spread (legs, strikes, expiry, max risk/reward)
- Options chain context (nearby strikes with Greeks, IV, bid/ask)
- Underlying price and technical indicators
- Portfolio cash balance

Consider:
- Is IV elevated enough for premium-selling strategies (or low enough for premium-buying)?
- Are the selected strikes reasonable given recent price action and ATR?
- Is the risk/reward acceptable for the strategy type?
- Are Greeks balanced (delta-neutral for non-directional strategies)?
- Is there sufficient liquidity (volume, open interest, tight bid-ask)?

Respond with JSON:
{
  "verdict": "confirm" | "modify" | "veto",
  "confidence": 0.0-1.0,
  "holding_strategy": "describe exit conditions, profit target, DTE management",
  "reasoning": "2-3 sentences with specific reference to Greeks, IV, and strikes"
}`

const optionsExitSystemPrompt = `You are a senior options strategist reviewing whether to close an options spread. You have full context: why the position was opened, current Greeks, P&L, and the decision history since entry.

Consider:
- Has the profit target been reached?
- Is theta decay working for or against us?
- Has IV changed significantly since entry (crush or expansion)?
- Is the underlying approaching our short strikes?
- Should we roll instead of close?

Respond with JSON:
{
  "verdict": "confirm" | "veto",
  "confidence": 0.0-1.0,
  "reasoning": "2-3 sentences"
}`

// OptionsEntryVerdict is the LLM's response to an options entry review.
type OptionsEntryVerdict struct {
	Verdict         string  `json:"verdict"`
	Confidence      float64 `json:"confidence"`
	HoldingStrategy string  `json:"holding_strategy,omitempty"`
	Reasoning       string  `json:"reasoning"`
}

// OptionsExitVerdict is the LLM's response to an options exit review.
type OptionsExitVerdict struct {
	Verdict    string  `json:"verdict"`
	Confidence float64 `json:"confidence"`
	Reasoning  string  `json:"reasoning"`
}

// OptionsSignalReviewer reviews options trade signals with Greeks and IV context.
type OptionsSignalReviewer struct {
	provider llm.Provider
	model    string
	logger   *slog.Logger
}

// NewOptionsSignalReviewer creates a reviewer for options spread signals.
func NewOptionsSignalReviewer(provider llm.Provider, model string, logger *slog.Logger) *OptionsSignalReviewer {
	if logger == nil {
		logger = slog.Default()
	}
	return &OptionsSignalReviewer{provider: provider, model: model, logger: logger}
}

// ReviewSpreadEntry reviews a proposed options spread entry.
// Returns (confirmed, holdingStrategy).
func (r *OptionsSignalReviewer) ReviewSpreadEntry(
	ctx context.Context,
	spread *domain.OptionSpread,
	chain []domain.OptionSnapshot,
	state *agent.PipelineState,
	underlyingPrice float64,
	portfolioCash float64,
) (bool, string) {
	if r == nil || r.provider == nil {
		return false, ""
	}
	userPrompt := buildOptionsEntryPrompt(spread, chain, state, underlyingPrice, portfolioCash)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Model: r.model,
		Messages: []llm.Message{
			{Role: "system", Content: optionsEntrySystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		r.logger.Warn("rules/options_reviewer: LLM call failed, vetoing spread entry",
			slog.Any("error", err),
		)
		return false, ""
	}
	if resp == nil {
		r.logger.Warn("rules/options_reviewer: LLM returned nil response, vetoing spread entry")
		return false, ""
	}

	var verdict OptionsEntryVerdict
	if err := decodeStrictReviewJSON(resp.Content, &verdict); err != nil {
		r.logger.Warn("rules/options_reviewer: failed to parse LLM response, vetoing spread entry",
			slog.Any("error", err),
		)
		return false, ""
	}
	verdictName := strings.ToLower(strings.TrimSpace(verdict.Verdict))
	if verdict.Confidence < 0 || verdict.Confidence > 1 || strings.TrimSpace(verdict.Reasoning) == "" {
		r.logger.Warn("rules/options_reviewer: invalid verdict schema, vetoing spread entry", slog.String("verdict", verdictName))
		return false, ""
	}

	r.logger.Info("rules/options_reviewer: entry verdict",
		slog.String("verdict", verdict.Verdict),
		slog.Float64("confidence", verdict.Confidence),
		slog.String("reasoning", verdict.Reasoning),
		slog.String("holding_strategy", verdict.HoldingStrategy),
		slog.String("strategy", string(spread.StrategyType)),
		slog.String("underlying", spread.Underlying),
	)

	switch verdictName {
	case "veto":
		return false, ""
	case "confirm", "modify":
		if strings.TrimSpace(verdict.HoldingStrategy) == "" {
			return false, ""
		}
		return true, verdict.HoldingStrategy
	default:
		r.logger.Warn("rules/options_reviewer: unknown verdict, vetoing spread entry", slog.String("verdict", verdictName))
		return false, ""
	}
}

// ReviewSpreadExit reviews whether to close an options spread.
// Returns (shouldClose, reasoning).
func (r *OptionsSignalReviewer) ReviewSpreadExit(
	ctx context.Context,
	spread *domain.OptionSpread,
	pos *OpenPosition,
	chain []domain.OptionSnapshot,
	state *agent.PipelineState,
	underlyingPrice float64,
	portfolioCash float64,
) (bool, string) {
	if r == nil || r.provider == nil {
		return true, "LLM unavailable, confirming exit"
	}
	userPrompt := buildOptionsExitPrompt(spread, pos, chain, state, underlyingPrice, portfolioCash)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Model: r.model,
		Messages: []llm.Message{
			{Role: "system", Content: optionsExitSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		r.logger.Warn("rules/options_reviewer: exit review LLM call failed, confirming exit by default",
			slog.Any("error", err),
		)
		return true, "LLM unavailable, confirming exit"
	}
	if resp == nil {
		r.logger.Warn("rules/options_reviewer: exit review LLM returned nil response, confirming exit")
		return true, "LLM returned no response, confirming exit"
	}

	var verdict OptionsExitVerdict
	if err := decodeStrictReviewJSON(resp.Content, &verdict); err != nil {
		r.logger.Warn("rules/options_reviewer: failed to parse exit response, confirming exit",
			slog.Any("error", err),
		)
		return true, "LLM parse error, confirming exit"
	}
	verdictName := strings.ToLower(strings.TrimSpace(verdict.Verdict))
	if verdict.Confidence < 0 || verdict.Confidence > 1 || strings.TrimSpace(verdict.Reasoning) == "" {
		r.logger.Warn("rules/options_reviewer: invalid exit verdict schema, confirming exit", slog.String("verdict", verdictName))
		return true, "Invalid LLM exit review, confirming exit"
	}

	r.logger.Info("rules/options_reviewer: exit verdict",
		slog.String("verdict", verdict.Verdict),
		slog.Float64("confidence", verdict.Confidence),
		slog.String("reasoning", verdict.Reasoning),
		slog.String("underlying", spread.Underlying),
	)

	switch verdictName {
	case "veto":
		return false, verdict.Reasoning
	default:
		return true, verdict.Reasoning
	}
}

// buildOptionsEntryPrompt constructs the user message for an entry review.
func buildOptionsEntryPrompt(
	spread *domain.OptionSpread,
	chain []domain.OptionSnapshot,
	state *agent.PipelineState,
	underlyingPrice float64,
	portfolioCash float64,
) string {
	var b strings.Builder

	// Proposed spread
	fmt.Fprintf(&b, "=== PROPOSED SPREAD ===\n")
	fmt.Fprintf(&b, "Strategy: %s on %s\n", spread.StrategyType, spread.Underlying)
	fmt.Fprintf(&b, "Legs:\n")
	for i, leg := range spread.Legs {
		fmt.Fprintf(&b, "  Leg %d: %s %s %s strike=%.2f exp=%s qty=%.0f ratio=%d\n",
			i+1,
			leg.Contract.OCCSymbol,
			strings.ToUpper(string(leg.Side)),
			strings.ToUpper(string(leg.Contract.OptionType)),
			leg.Contract.Strike,
			leg.Contract.Expiry.Format("2006-01-02"),
			leg.Quantity,
			leg.Ratio,
		)
	}
	fmt.Fprintf(&b, "\n=== SPREAD RISK/REWARD ===\n")
	fmt.Fprintf(&b, "Max Risk:   $%.2f\n", spread.MaxRisk)
	fmt.Fprintf(&b, "Max Reward: $%.2f\n", spread.MaxReward)
	if spread.MaxRisk > 0 {
		fmt.Fprintf(&b, "R/R Ratio:  %.2f\n", spread.MaxReward/spread.MaxRisk)
	}
	// Break-even: estimate from legs
	if len(spread.Legs) >= 2 {
		strikes := make([]float64, 0, len(spread.Legs))
		for _, leg := range spread.Legs {
			strikes = append(strikes, leg.Contract.Strike)
		}
		fmt.Fprintf(&b, "Strike Range: $%.2f - $%.2f\n", minFloat(strikes), maxFloat(strikes))
	}

	// Options chain context
	if len(chain) > 0 {
		fmt.Fprintf(&b, "\n=== OPTIONS CHAIN CONTEXT ===\n")
		fmt.Fprintf(&b, "%-8s %-6s %8s %6s %8s %6s %6s %10s %10s\n",
			"Type", "Strike", "Expiry", "Delta", "IV", "Bid", "Ask", "Volume", "OI")
		for _, snap := range chain {
			fmt.Fprintf(&b, "%-8s %6.2f %8s %+6.3f %7.1f%% %6.2f %6.2f %10.0f %10.0f\n",
				strings.ToUpper(string(snap.Contract.OptionType)),
				snap.Contract.Strike,
				snap.Contract.Expiry.Format("Jan02"),
				snap.Greeks.Delta,
				snap.Greeks.IV*100,
				snap.Bid,
				snap.Ask,
				snap.Volume,
				snap.OpenInterest,
			)
		}
	}

	// Underlying context
	fmt.Fprintf(&b, "\n=== UNDERLYING CONTEXT ===\n")
	fmt.Fprintf(&b, "Underlying Price: $%.2f\n", underlyingPrice)
	if state != nil && state.Market != nil && len(state.Market.Indicators) > 0 {
		for _, ind := range state.Market.Indicators {
			fmt.Fprintf(&b, "  %-20s = %12.4f", ind.Name, ind.Value)
			if ind.Name == "rsi_14" {
				if ind.Value < 30 {
					fmt.Fprintf(&b, "  (OVERSOLD)")
				} else if ind.Value > 70 {
					fmt.Fprintf(&b, "  (OVERBOUGHT)")
				}
			}
			fmt.Fprintf(&b, "\n")
		}
	}

	// Portfolio
	fmt.Fprintf(&b, "\n=== PORTFOLIO ===\n")
	fmt.Fprintf(&b, "Available Cash: $%.2f\n", portfolioCash)
	if portfolioCash > 0 && spread.MaxRisk > 0 {
		fmt.Fprintf(&b, "Trade Cost as %% of Portfolio: %.1f%%\n", spread.MaxRisk/portfolioCash*100)
	}

	return b.String()
}

// buildOptionsExitPrompt constructs the user message for an exit review.
func buildOptionsExitPrompt(
	spread *domain.OptionSpread,
	pos *OpenPosition,
	chain []domain.OptionSnapshot,
	state *agent.PipelineState,
	underlyingPrice float64,
	portfolioCash float64,
) string {
	var b strings.Builder

	// Position context
	if pos != nil {
		pnl := pos.UnrealizedPnL(underlyingPrice)
		pnlPct := pos.UnrealizedPnLPct(underlyingPrice)
		fmt.Fprintf(&b, "=== CURRENT POSITION ===\n")
		fmt.Fprintf(&b, "Strategy: %s on %s\n", spread.StrategyType, spread.Underlying)
		fmt.Fprintf(&b, "Entry: $%.2f on %s (%d days held)\n",
			pos.EntryPrice, pos.EntryDate.Format("2006-01-02"), pos.HoldingDays(pos.EntryDate.AddDate(0, 0, pos.HoldingDays(pos.EntryDate))))
		fmt.Fprintf(&b, "P&L: %+.2f (%+.1f%%)\n", pnl*pos.Quantity, pnlPct)

		if pos.HoldingStrategy != "" {
			fmt.Fprintf(&b, "\nHolding Strategy (from entry review):\n\"%s\"\n", pos.HoldingStrategy)
		}

		if len(pos.Journal) > 0 {
			fmt.Fprintf(&b, "\n=== DECISION HISTORY ===\n")
			for _, entry := range pos.Journal {
				date := entry.Timestamp.Format("2006-01-02")
				fmt.Fprintf(&b, "[%s] %s at $%.2f", date, strings.ToUpper(string(entry.Type)), entry.Price)
				if entry.Reasoning != "" {
					fmt.Fprintf(&b, " -- %s", entry.Reasoning)
				}
				fmt.Fprintf(&b, "\n")
			}
		}
	}

	// Current spread legs with Greeks from chain
	fmt.Fprintf(&b, "\n=== CURRENT SPREAD LEGS ===\n")
	for i, leg := range spread.Legs {
		fmt.Fprintf(&b, "  Leg %d: %s %s strike=%.2f exp=%s\n",
			i+1,
			strings.ToUpper(string(leg.Side)),
			strings.ToUpper(string(leg.Contract.OptionType)),
			leg.Contract.Strike,
			leg.Contract.Expiry.Format("2006-01-02"),
		)
		// Find matching chain entry for current Greeks
		for _, snap := range chain {
			if snap.Contract.OCCSymbol == leg.Contract.OCCSymbol {
				fmt.Fprintf(&b, "         delta=%+.3f gamma=%.4f theta=%.4f vega=%.4f IV=%.1f%%\n",
					snap.Greeks.Delta, snap.Greeks.Gamma, snap.Greeks.Theta, snap.Greeks.Vega, snap.Greeks.IV*100)
				break
			}
		}
	}

	// Risk/reward
	fmt.Fprintf(&b, "\nMax Risk: $%.2f | Max Reward: $%.2f\n", spread.MaxRisk, spread.MaxReward)

	// Chain context for nearby strikes
	if len(chain) > 0 {
		fmt.Fprintf(&b, "\n=== OPTIONS CHAIN CONTEXT ===\n")
		fmt.Fprintf(&b, "%-8s %6s %8s %6s %8s %6s %6s\n",
			"Type", "Strike", "Expiry", "Delta", "IV", "Bid", "Ask")
		for _, snap := range chain {
			fmt.Fprintf(&b, "%-8s %6.2f %8s %+6.3f %7.1f%% %6.2f %6.2f\n",
				strings.ToUpper(string(snap.Contract.OptionType)),
				snap.Contract.Strike,
				snap.Contract.Expiry.Format("Jan02"),
				snap.Greeks.Delta,
				snap.Greeks.IV*100,
				snap.Bid,
				snap.Ask,
			)
		}
	}

	// Underlying context
	fmt.Fprintf(&b, "\n=== UNDERLYING CONTEXT ===\n")
	fmt.Fprintf(&b, "Underlying Price: $%.2f\n", underlyingPrice)
	if state != nil && state.Market != nil {
		for _, ind := range state.Market.Indicators {
			fmt.Fprintf(&b, "  %-20s = %12.4f\n", ind.Name, ind.Value)
		}
	}

	// Portfolio
	fmt.Fprintf(&b, "\n=== PORTFOLIO ===\n")
	fmt.Fprintf(&b, "Available Cash: $%.2f\n", portfolioCash)

	fmt.Fprintf(&b, "\nThe rules engine has triggered a CLOSE signal on this options spread. Should we close?")

	return b.String()
}

func minFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := math.MaxFloat64
	for _, v := range vals {
		if v < m {
			m = v
		}
	}
	return m
}

func maxFloat(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	m := -math.MaxFloat64
	for _, v := range vals {
		if v > m {
			m = v
		}
	}
	return m
}
