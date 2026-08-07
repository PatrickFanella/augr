package options

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	llmparse "github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
)

const optionsGeneratorSystemPrompt = `You are a quantitative options strategy designer. Given recent market data, technical indicators, and options chain metrics for a ticker, generate a complete OptionsRulesConfig as JSON.

The JSON schema is:
{
  "version": 1,
  "strategy_type": "<one of: bull_put_spread, bear_call_spread, covered_call>",
  "underlying": "<TICKER>",
  "entry": {
    "operator": "AND" | "OR",
    "conditions": [
      {"field": "<field_name>", "op": "<op>", "value": <number>}
    ]
  },
  "exit": {
    "operator": "AND" | "OR",
    "conditions": [
      {"field": "<field_name>", "op": "<op>", "value": <number>}
    ]
  },
  "leg_selection": {
    "<leg_name>": {
      "option_type": "call" | "put",
      "delta_target": <0.0-1.0>,
      "dte_min": <int>,
      "dte_max": <int>,
      "side": "buy" | "sell",
      "position_intent": "buy_to_open" | "sell_to_open",
      "ratio": 1
    }
  },
  "position_sizing": {
    "method": "max_risk" | "fixed_contracts" | "premium_budget",
    "max_risk_usd": <number>,
    "fixed_contracts": <int>,
    "premium_budget": <number>
  },
  "management": {
    "close_at_profit_pct": <0.0-1.0>,
    "close_at_dte": <int>,
    "roll_at_dte": <int>,
    "stop_loss_pct": <0.0-1.0>
  }
}

Available strategy types for v1:
- "bull_put_spread": Sell higher-strike put, buy lower-strike put. Bullish/neutral, benefits from high IV. Use when IV rank > 50.
- "bear_call_spread": Sell lower-strike call, buy higher-strike call. Bearish/neutral, benefits from high IV. Use when IV rank > 50.
- "covered_call": Sell OTM call against long stock. Neutral/mildly bullish. Good for income when IV is moderate.

Available fields for conditions (same as equity indicators + options-specific):
- Equity: close, volume, sma_20, sma_50, sma_200, ema_12, rsi_14, mfi_14, atr_14, macd_line, macd_signal, macd_histogram, bollinger_upper, bollinger_middle, bollinger_lower, stochastic_k, stochastic_d
- Options: iv_rank, iv_percentile, atm_iv, put_call_ratio

Available operators: gt, gte, lt, lte, eq, cross_above, cross_below

Leg selection guidelines:
- bull_put_spread: 2 legs — "short_put" (sell, delta ~0.16-0.30, sell_to_open) and "long_put" (buy, delta ~0.05-0.16, buy_to_open)
- bear_call_spread: 2 legs — "short_call" (sell, delta ~0.16-0.30, sell_to_open) and "long_call" (buy, delta ~0.05-0.16, buy_to_open)
- covered_call: 1 leg — "short_call" (sell, delta ~0.20-0.35, sell_to_open)

DTE ranges should be 20-50 days for premium selling strategies.

Management guidelines:
- Premium selling: close_at_profit_pct 0.50 (take profit at 50% of max), close_at_dte 5-7, stop_loss_pct 1.0-2.0
- Covered call: close_at_profit_pct 0.75, close_at_dte 3

Position sizing: use "max_risk" with max_risk_usd between 500-2000 for paper trading.

Respond with ONLY the JSON object, no markdown fences.`

// OptionsGenerationEvidence is compact, non-content provenance for one
// options strategy generation. Prompts and responses are represented only by
// hashes; the parsed configuration is safe to persist with the run result.
type OptionsGenerationEvidence struct {
	Ticker             string                                `json:"ticker"`
	SystemPromptSHA256 string                                `json:"system_prompt_sha256"`
	UserPromptSHA256   string                                `json:"user_prompt_sha256"`
	Attempts           []discovery.GenerationAttemptEvidence `json:"attempts"`
	Config             *rules.OptionsRulesConfig             `json:"config,omitempty"`
}

// GenerateOptionsStrategy asks the LLM to create an OptionsRulesConfig for a scored candidate.
func GenerateOptionsStrategy(ctx context.Context, cfg discovery.GeneratorConfig, candidate OptionsScoredCandidate, logger *slog.Logger) (*rules.OptionsRulesConfig, error) {
	generated, _, err := GenerateOptionsStrategyWithEvidence(ctx, cfg, candidate, logger)
	return generated, err
}

// GenerateOptionsStrategyWithEvidence generates an options strategy and
// returns durable model/cache/attempt provenance without raw model content.
func GenerateOptionsStrategyWithEvidence(ctx context.Context, cfg discovery.GeneratorConfig, candidate OptionsScoredCandidate, logger *slog.Logger) (*rules.OptionsRulesConfig, *OptionsGenerationEvidence, error) {
	if logger == nil {
		logger = slog.Default()
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	userPrompt := buildOptionsUserPrompt(candidate)
	evidence := &OptionsGenerationEvidence{
		Ticker:             candidate.Ticker,
		SystemPromptSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(optionsGeneratorSystemPrompt))),
		UserPromptSHA256:   fmt.Sprintf("%x", sha256.Sum256([]byte(userPrompt))),
	}

	messages := []llm.Message{
		{Role: "system", Content: optionsGeneratorSystemPrompt},
		{Role: "user", Content: userPrompt},
	}

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		cacheStats := llm.NewCacheStatsCollector()
		resp, err := cfg.Provider.Complete(llm.WithCacheStatsCollector(ctx, cacheStats), llm.CompletionRequest{
			Model:          cfg.Model,
			Messages:       messages,
			ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
		})
		if err != nil {
			evidence.Attempts = append(evidence.Attempts, discovery.GenerationAttemptEvidence{
				Attempt: attempt + 1, RequestedModel: cfg.Model, Outcome: "provider_error",
			})
			if cfg.Metrics != nil {
				cfg.Metrics.RecordGeneratorOutcome("options", "provider_error")
			}
			return nil, evidence, fmt.Errorf("options/generator: LLM call failed: %w", err)
		}

		stats := cacheStats.Snapshot()
		responseHash := fmt.Sprintf("%x", sha256.Sum256([]byte(resp.Content)))
		attemptEvidence := discovery.GenerationAttemptEvidence{
			Attempt: attempt + 1, RequestedModel: cfg.Model, ResponseModel: resp.Model,
			ContentSHA256: responseHash, CacheHits: stats.Hits, CacheMisses: stats.Misses,
			PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens,
			LatencyMS: resp.LatencyMS, CostUSD: resp.CostUSD, UsedFallback: resp.UsedFallback, TimedOut: resp.TimedOut,
		}
		if stats.Hits > 0 {
			attemptEvidence.Outcome = "cache_rejected"
			evidence.Attempts = append(evidence.Attempts, attemptEvidence)
			if cfg.Metrics != nil {
				cfg.Metrics.RecordGeneratorOutcome("options", "cache_rejected")
			}
			return nil, evidence, fmt.Errorf("options/generator: cached model response rejected")
		}
		logger.Debug("options/generator: LLM response received",
			slog.String("ticker", candidate.Ticker),
			slog.Int("attempt", attempt+1),
			slog.Int("content_bytes", len(resp.Content)),
			slog.String("content_sha256", responseHash),
		)

		var jsonObject string
		var extractErr error
		if strings.TrimSpace(resp.Content) == "" {
			extractErr = errors.New("rules: empty JSON response")
		} else {
			jsonObject, extractErr = llmparse.ExtractJSONObject(resp.Content)
		}
		var parsed *rules.OptionsRulesConfig
		parseErr := extractErr
		if extractErr == nil {
			parsed, parseErr = rules.ParseOptions(json.RawMessage(jsonObject))
		}
		if parsed == nil && parseErr == nil {
			parseErr = errors.New("rules: empty JSON response")
		}
		if parseErr == nil && parsed != nil {
			outcome := "success_first_attempt"
			if attempt > 0 {
				outcome = "success_after_retry"
			}
			if cfg.Metrics != nil {
				cfg.Metrics.RecordGeneratorOutcome("options", outcome)
			}
			attemptEvidence.Outcome = outcome
			evidence.Attempts = append(evidence.Attempts, attemptEvidence)
			evidence.Config = parsed
			logger.Info("options/generator: strategy generated",
				slog.String("ticker", candidate.Ticker),
				slog.String("type", string(parsed.StrategyType)),
				slog.Int("attempt", attempt+1),
			)
			return parsed, evidence, nil
		}

		lastErr = parseErr
		attemptEvidence.Outcome = "validation_exhausted"
		if attempt < maxRetries {
			attemptEvidence.Outcome = "validation_retry"
			logger.Warn("options/generator: parse/validation failed, retrying",
				slog.String("ticker", candidate.Ticker),
				slog.Int("attempt", attempt+1),
				slog.Any("error", parseErr),
			)
			messages = append(messages, llm.Message{Role: "user", Content: fmt.Sprintf(
				"The JSON you produced failed validation with this error:\n%s\n\nPlease fix the issue and return corrected JSON only.",
				parseErr.Error(),
			)})
		}
		evidence.Attempts = append(evidence.Attempts, attemptEvidence)
	}

	if cfg.Metrics != nil {
		cfg.Metrics.RecordGeneratorOutcome("options", "validation_exhausted")
	}
	return nil, evidence, fmt.Errorf("options/generator: failed after %d retries: %w", maxRetries+1, lastErr)
}

func buildOptionsUserPrompt(c OptionsScoredCandidate) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Ticker: %s\nCurrent close: $%.2f\n", c.Ticker, c.Close)
	fmt.Fprintf(&sb, "ADV: $%.0f\n\n", c.ADV)

	// Options metrics.
	fmt.Fprintf(&sb, "Options metrics:\n")
	fmt.Fprintf(&sb, "  IV Rank: %.1f\n", c.IVRank)
	fmt.Fprintf(&sb, "  ATM IV: %.2f%%\n", c.ATMIV*100)
	fmt.Fprintf(&sb, "  Put/Call Ratio: %.2f\n", c.PutCallRatio)
	fmt.Fprintf(&sb, "  Volume Ratio: %.2f\n", c.VolumeRatio)
	fmt.Fprintf(&sb, "  Chain Depth: %d contracts\n", c.ChainDepth)
	fmt.Fprintf(&sb, "  ATM Open Interest: %.0f\n\n", c.ATMOI)

	// Recent price action.
	sb.WriteString("Recent price action (last 10 bars):\n")
	start := 0
	if len(c.Bars) > 10 {
		start = len(c.Bars) - 10
	}
	for _, bar := range c.Bars[start:] {
		fmt.Fprintf(&sb, "  %s  O=%.2f H=%.2f L=%.2f C=%.2f V=%.0f\n",
			bar.Timestamp.Format("2006-01-02"),
			bar.Open, bar.High, bar.Low, bar.Close, bar.Volume,
		)
	}

	// Indicators.
	sb.WriteString("\nIndicator values:\n")
	for _, ind := range c.Indicators {
		fmt.Fprintf(&sb, "  %s = %.4f\n", ind.Name, ind.Value)
	}

	fmt.Fprintf(&sb, "\nGenerate an options strategy for %s. ", c.Ticker)
	if c.IVRank > 50 {
		sb.WriteString("IV rank is elevated — prefer premium-selling strategies (bull_put_spread or bear_call_spread). ")
	} else {
		sb.WriteString("IV rank is moderate — consider a covered_call for income. ")
	}
	sb.WriteString("Keep entry conditions simple (1-2 conditions). Use the options metrics above to inform your thresholds.")

	return sb.String()
}
