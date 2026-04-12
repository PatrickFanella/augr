package signal

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/google/uuid"
)

// SignalEvalMetrics is implemented by *metrics.Metrics.
type SignalEvalMetrics interface {
	RecordSignalParseFailure()
}

// EvaluatedSignal is a RawSignalEvent that has passed the keyword filter
// and been scored by the LLM evaluator.
type EvaluatedSignal struct {
	Raw                RawSignalEvent
	AffectedStrategies []uuid.UUID
	Urgency            int    // 1–5; 1=noise, 5=critical/breaking
	Summary            string // one-line LLM summary
	RecommendedAction  string // "monitor", "re-evaluate", or "execute_thesis"
}

// StrategyContext provides the evaluator with just enough about a strategy
// to decide whether a signal is relevant and how urgently to act.
type StrategyContext struct {
	ID            uuid.UUID
	Ticker        string
	WatchTerms    []string
	ThesisSummary string
}

// Evaluator uses an LLM to score RawSignalEvents against active strategies.
type Evaluator struct {
	provider     llm.Provider
	model        string
	logger       *slog.Logger
	metrics      SignalEvalMetrics // nil-safe
	fallbackMode string            // "drop" (default) or "legacy"
}

// NewEvaluator creates an Evaluator backed by the given LLM provider and model.
func NewEvaluator(provider llm.Provider, model string, logger *slog.Logger) *Evaluator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Evaluator{provider: provider, model: model, logger: logger}
}

// WithMetrics attaches a metrics sink for parse-failure tracking.
func (e *Evaluator) WithMetrics(m SignalEvalMetrics) *Evaluator {
	e.metrics = m
	return e
}

// WithFallbackMode sets the fallback behaviour on LLM/parse failures.
// Valid values: "drop" (default, urgency=1, no strategies) or "legacy"
// (urgency=3, all registered strategies — the old broken behaviour).
func (e *Evaluator) WithFallbackMode(mode string) *Evaluator {
	if mode == "legacy" {
		e.fallbackMode = "legacy"
	} else {
		e.fallbackMode = "drop"
	}
	return e
}

const evaluatorSystemPrompt = `You are a financial signal evaluator. Given a news or market signal event and a list of active trading strategies, evaluate the signal's relevance and urgency.

Return exactly this JSON object (no markdown, no extra text):
{
  "affected_strategy_ids": ["<uuid>", ...],
  "urgency": <1-5>,
  "summary": "<one-line summary>",
  "recommended_action": "monitor" | "re-evaluate" | "execute_thesis"
}

Urgency scale: 1=noise/irrelevant, 2=low-impact, 3=moderate, 4=high-impact, 5=critical/breaking
recommended_action:
  "monitor"         — routine update, no action needed
  "re-evaluate"     — material development, re-run analysis
  "execute_thesis"  — critical breaking event, execute immediately

Only include strategy IDs that are genuinely affected by this signal.`

// Evaluate scores a signal event against the provided strategies.
// On LLM failure, returns a low-urgency fallback EvaluatedSignal rather than an error.
// Returns nil if strategies is empty.
func (e *Evaluator) Evaluate(ctx context.Context, event RawSignalEvent, strategies []StrategyContext) (*EvaluatedSignal, error) {
	if len(strategies) == 0 {
		return nil, nil
	}

	type strategyDesc struct {
		ID            string   `json:"id"`
		Ticker        string   `json:"ticker"`
		WatchTerms    []string `json:"watch_terms"`
		ThesisSummary string   `json:"thesis_summary,omitempty"`
	}
	descs := make([]strategyDesc, len(strategies))
	for i, s := range strategies {
		descs[i] = strategyDesc{
			ID:            s.ID.String(),
			Ticker:        s.Ticker,
			WatchTerms:    s.WatchTerms,
			ThesisSummary: s.ThesisSummary,
		}
	}
	stratJSON, _ := json.Marshal(descs)

	userMsg := fmt.Sprintf("Signal:\nSource: %s\nTitle: %s\nBody: %s\n\nActive strategies:\n%s",
		event.Source, event.Title, event.Body, string(stratJSON))

	resp, err := e.provider.Complete(ctx, llm.CompletionRequest{
		Model: e.model,
		Messages: []llm.Message{
			{Role: "system", Content: evaluatorSystemPrompt},
			{Role: "user", Content: userMsg},
		},
		Temperature:    0.1,
		MaxTokens:      512,
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		e.logger.Warn("signal evaluator LLM call failed, using fallback",
			slog.String("source", event.Source),
			slog.String("title", event.Title),
			slog.Any("error", err),
		)
		if e.metrics != nil {
			e.metrics.RecordSignalParseFailure()
		}
		return e.fallback(event, strategies), nil
	}

	return e.parseResponse(strings.TrimSpace(resp.Content), event, strategies)
}

type evaluatorOutput struct {
	AffectedStrategyIDs []string `json:"affected_strategy_ids"`
	Urgency             int      `json:"urgency"`
	Summary             string   `json:"summary"`
	RecommendedAction   string   `json:"recommended_action"`
}

func (e *Evaluator) parseResponse(content string, event RawSignalEvent, strategies []StrategyContext) (*EvaluatedSignal, error) {
	var out evaluatorOutput
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		e.logger.Warn("signal evaluator: failed to parse LLM output, using fallback",
			slog.String("content", content),
			slog.Any("error", err),
		)
		if e.metrics != nil {
			e.metrics.RecordSignalParseFailure()
		}
		return e.fallback(event, strategies), nil
	}

	// Build known-ID set to filter LLM hallucinations.
	idSet := make(map[uuid.UUID]struct{}, len(strategies))
	for _, s := range strategies {
		idSet[s.ID] = struct{}{}
	}

	affected := make([]uuid.UUID, 0, len(out.AffectedStrategyIDs))
	for _, idStr := range out.AffectedStrategyIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			continue
		}
		if _, ok := idSet[id]; ok {
			affected = append(affected, id)
		}
	}

	urgency := out.Urgency
	if urgency < 1 {
		urgency = 1
	} else if urgency > 5 {
		urgency = 5
	}

	action := out.RecommendedAction
	switch action {
	case "monitor", "re-evaluate", "execute_thesis":
	default:
		action = "monitor"
	}

	return &EvaluatedSignal{
		Raw:                event,
		AffectedStrategies: affected,
		Urgency:            urgency,
		Summary:            out.Summary,
		RecommendedAction:  action,
	}, nil
}

func (e *Evaluator) fallback(event RawSignalEvent, strategies []StrategyContext) *EvaluatedSignal {
	if e.fallbackMode == "legacy" {
		// Legacy: urgency=3, all registered strategies (old broken behaviour).
		ids := make([]uuid.UUID, len(strategies))
		for i, s := range strategies {
			ids[i] = s.ID
		}
		return &EvaluatedSignal{
			Raw:                event,
			AffectedStrategies: ids,
			Urgency:            3,
			Summary:            event.Title,
			RecommendedAction:  "monitor",
		}
	}
	// Drop mode (default): urgency=1, no affected strategies → hub drops it.
	return &EvaluatedSignal{
		Raw:                event,
		AffectedStrategies: nil,
		Urgency:            1,
		Summary:            event.Title,
		RecommendedAction:  "monitor",
	}
}
