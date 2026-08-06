package discovery

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	llmparse "github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
)

// GeneratorConfig controls how the LLM is called when generating a strategy.
type GeneratorConfig struct {
	Provider   llm.Provider
	Model      string
	MaxRetries int // default 3
	Metrics    GeneratorMetrics
}

// GeneratorMetrics records terminal generator outcomes. Implementations must
// keep asset and outcome labels bounded; *metrics.Metrics satisfies it.
type GeneratorMetrics interface {
	RecordGeneratorOutcome(asset, outcome string)
}

// GenerationAttemptEvidence is the compact, non-content provenance for one
// provider attempt. Hashes allow exact correlation with logs without storing
// prompts or model output.
type GenerationAttemptEvidence struct {
	Attempt          int     `json:"attempt"`
	RequestedModel   string  `json:"requested_model,omitempty"`
	ResponseModel    string  `json:"response_model,omitempty"`
	ContentSHA256    string  `json:"content_sha256,omitempty"`
	CacheHits        int     `json:"cache_hits"`
	CacheMisses      int     `json:"cache_misses"`
	PromptTokens     int     `json:"prompt_tokens,omitempty"`
	CompletionTokens int     `json:"completion_tokens,omitempty"`
	LatencyMS        int     `json:"latency_ms,omitempty"`
	CostUSD          float64 `json:"cost_usd,omitempty"`
	UsedFallback     bool    `json:"used_fallback,omitempty"`
	TimedOut         bool    `json:"timed_out,omitempty"`
	Outcome          string  `json:"outcome"`
}

// GenerationEvidence records prompt identity and every provider attempt for a
// candidate without persisting raw prompts or responses.
type GenerationEvidence struct {
	Ticker             string                      `json:"ticker"`
	SystemPromptSHA256 string                      `json:"system_prompt_sha256"`
	UserPromptSHA256   string                      `json:"user_prompt_sha256"`
	Attempts           []GenerationAttemptEvidence `json:"attempts"`
	Config             *rules.RulesEngineConfig    `json:"config,omitempty"`
}

const generatorSystemPrompt = `You are a quantitative trading strategy designer. Given recent market data and technical indicators for a ticker, generate a complete RulesEngineConfig as JSON.

The JSON schema is:
{
  "version": 1,
  "name": "<strategy name>",
  "description": "<brief description>",
  "entry": {
    "operator": "AND" | "OR",
    "conditions": [
      {
        "field": "<field name>",
        "op": "<operator>",
        "value": <number>,
        "ref": "<other field name>"
      }
    ]
  },
  "exit": { ... same structure as entry ... },
  "position_sizing": {
    "method": "fixed_fraction" | "atr_based" | "fixed_amount",
    "risk_per_trade_pct": <float>,
    "atr_multiplier": <float>,
    "fixed_amount_usd": <float>,
    "fraction_pct": <float>
  },
  "stop_loss": {
    "method": "fixed_pct" | "atr_multiple" | "indicator",
    "pct": <float>,
    "atr_multiplier": <float>,
    "indicator_ref": "<indicator name>"
  },
  "take_profit": {
    "method": "fixed_pct" | "atr_multiple" | "risk_reward",
    "pct": <float>,
    "atr_multiplier": <float>,
    "ratio": <float>
  },
  "filters": {
    "min_volume": <float>,
    "min_atr": <float>
  }
}

Available field names for conditions:
- OHLCV fields: open, high, low, close, volume
- Indicator fields: sma_20, sma_50, sma_200, ema_12, rsi_14, mfi_14, williams_r_14, cci_20, roc_12, atr_14, vwma_20, obv, adl, macd_line, macd_signal, macd_histogram, stochastic_k, stochastic_d, bollinger_upper, bollinger_middle, bollinger_lower

Available operators: gt, gte, lt, lte, eq, cross_above, cross_below

Each condition must have "field" and "op". Use either "value" (literal number) or "ref" (another field name), not both.

Rules:
- version must be 1
- entry and exit must each have at least one condition
- position_sizing, stop_loss, and take_profit are required
- IMPORTANT: Use only 1-2 entry conditions, not more. Strategies with too many
  conditions rarely trigger any trades. A single RSI threshold or a moving average
  crossover is sufficient for entry. Keep it simple.
- Use moderate thresholds that will trigger regularly: RSI 40 instead of 30,
  RSI 60 instead of 70. The goal is a strategy that trades 10-30 times per year.
- Prefer "gt"/"lt" operators over "cross_above"/"cross_below" for more frequent signals.
- All "value" fields must be numbers (not strings). Example: "value": 40 not "value": "40"

Respond with ONLY the JSON object, no markdown fences.`

// GenerateStrategy asks the LLM to create a RulesEngineConfig for the given candidate.
// Retries up to MaxRetries on validation errors, feeding the error back to the LLM.
func GenerateStrategy(ctx context.Context, cfg GeneratorConfig, candidate ScreenResult, logger *slog.Logger) (*rules.RulesEngineConfig, error) {
	generated, _, err := GenerateStrategyWithEvidence(ctx, cfg, candidate, logger)
	return generated, err
}

// GenerateStrategyWithEvidence generates a strategy and returns compact,
// durable model-call provenance for discovery-run persistence.
func GenerateStrategyWithEvidence(ctx context.Context, cfg GeneratorConfig, candidate ScreenResult, logger *slog.Logger) (*rules.RulesEngineConfig, *GenerationEvidence, error) {
	if logger == nil {
		logger = slog.Default()
	}
	maxRetries := cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}

	userPrompt := buildGeneratorUserPrompt(candidate)
	evidence := &GenerationEvidence{
		Ticker:             candidate.Ticker,
		SystemPromptSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte(generatorSystemPrompt))),
		UserPromptSHA256:   fmt.Sprintf("%x", sha256.Sum256([]byte(userPrompt))),
	}

	messages := []llm.Message{
		{Role: "system", Content: generatorSystemPrompt},
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
			evidence.Attempts = append(evidence.Attempts, GenerationAttemptEvidence{
				Attempt: attempt + 1, RequestedModel: cfg.Model, Outcome: "provider_error",
			})
			recordGeneratorOutcome(cfg.Metrics, "stock", "provider_error")
			return nil, evidence, fmt.Errorf("discovery/generator: LLM call failed: %w", err)
		}
		stats := cacheStats.Snapshot()
		responseHash := fmt.Sprintf("%x", sha256.Sum256([]byte(resp.Content)))
		attemptEvidence := GenerationAttemptEvidence{
			Attempt: attempt + 1, RequestedModel: cfg.Model, ResponseModel: resp.Model,
			ContentSHA256: responseHash, CacheHits: stats.Hits, CacheMisses: stats.Misses,
			PromptTokens: resp.Usage.PromptTokens, CompletionTokens: resp.Usage.CompletionTokens,
			LatencyMS: resp.LatencyMS, CostUSD: resp.CostUSD, UsedFallback: resp.UsedFallback, TimedOut: resp.TimedOut,
		}
		if stats.Hits > 0 {
			attemptEvidence.Outcome = "cache_rejected"
			evidence.Attempts = append(evidence.Attempts, attemptEvidence)
			recordGeneratorOutcome(cfg.Metrics, "stock", "cache_rejected")
			return nil, evidence, fmt.Errorf("discovery/generator: cached model response rejected")
		}

		logger.Debug("discovery/generator: LLM response received",
			slog.String("ticker", candidate.Ticker),
			slog.Int("attempt", attempt+1),
			slog.Int("content_bytes", len(resp.Content)),
			slog.String("content_sha256", responseHash),
			slog.Int("cache_hits", stats.Hits),
			slog.Int("cache_misses", stats.Misses),
		)

		var jsonObject string
		var extractErr error
		if strings.TrimSpace(resp.Content) == "" {
			extractErr = errors.New("rules: empty JSON response")
		} else {
			jsonObject, extractErr = llmparse.ExtractJSONObject(resp.Content)
		}
		var parsed *rules.RulesEngineConfig
		parseErr := extractErr
		if extractErr == nil {
			parsed, parseErr = rules.Parse(json.RawMessage(jsonObject))
		}
		if parsed == nil && parseErr == nil {
			parseErr = errors.New("rules: empty JSON response")
		}
		if parseErr == nil && parsed != nil {
			outcome := "success_first_attempt"
			if attempt > 0 {
				outcome = "success_after_retry"
			}
			attemptEvidence.Outcome = outcome
			evidence.Attempts = append(evidence.Attempts, attemptEvidence)
			evidence.Config = parsed
			recordGeneratorOutcome(cfg.Metrics, "stock", outcome)
			logger.Info("discovery/generator: strategy generated",
				slog.String("ticker", candidate.Ticker),
				slog.String("name", parsed.Name),
				slog.Int("attempt", attempt+1),
			)
			return parsed, evidence, nil
		}

		lastErr = parseErr
		attemptEvidence.Outcome = "validation_exhausted"
		if attempt < maxRetries {
			attemptEvidence.Outcome = "validation_retry"
			logger.Warn("discovery/generator: parse/validation failed, retrying",
				slog.String("ticker", candidate.Ticker),
				slog.Int("attempt", attempt+1),
				slog.Any("error", parseErr),
			)
			parseErrText := parseErr.Error()

			// Append correction prompt for the next attempt.
			messages = append(messages,
				llm.Message{Role: "user", Content: fmt.Sprintf(
					"The JSON you produced failed validation with this error:\n%s\n\nPlease fix the issue and return corrected JSON only.",
					parseErrText,
				)},
			)
		}
		evidence.Attempts = append(evidence.Attempts, attemptEvidence)
	}

	recordGeneratorOutcome(cfg.Metrics, "stock", "validation_exhausted")
	return nil, evidence, fmt.Errorf("discovery/generator: failed after %d retries: %w", maxRetries+1, lastErr)
}

func recordGeneratorOutcome(metrics GeneratorMetrics, asset, outcome string) {
	if metrics != nil {
		metrics.RecordGeneratorOutcome(asset, outcome)
	}
}

func buildGeneratorUserPrompt(c ScreenResult) string {
	var sb strings.Builder

	fmt.Fprintf(&sb, "Ticker: %s\nCurrent close: %.4f\n\n", c.Ticker, c.Close)

	// Recent price action (last 10 bars).
	sb.WriteString("Recent price action (last 10 bars):\n")
	start := 0
	if len(c.Bars) > 10 {
		start = len(c.Bars) - 10
	}
	for _, bar := range c.Bars[start:] {
		fmt.Fprintf(&sb, "  %s  O=%.4f H=%.4f L=%.4f C=%.4f V=%.0f\n",
			bar.Timestamp.Format("2006-01-02"),
			bar.Open, bar.High, bar.Low, bar.Close, bar.Volume,
		)
	}

	// All indicator values.
	sb.WriteString("\nIndicator values:\n")
	for _, ind := range c.Indicators {
		fmt.Fprintf(&sb, "  %s = %.6f\n", ind.Name, ind.Value)
	}

	fmt.Fprintf(&sb, "\nGenerate a simple trading strategy for %s that will trigger trades regularly (10-30 times per year). Use 1-2 entry conditions with moderate thresholds. Keep it simple — fewer conditions means more trades.", c.Ticker)
	return sb.String()
}
