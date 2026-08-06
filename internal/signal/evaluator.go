package signal

import (
	"context"
	"encoding/json"
	"errors"
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
	provider llm.Provider
	model    string
	logger   *slog.Logger
	metrics  SignalEvalMetrics // nil-safe
}

// SignalEvaluator is the adapter used by the signal lifecycle.
type SignalEvaluator interface {
	Evaluate(context.Context, RawSignalEvent, []StrategyContext) (*EvaluatedSignal, error)
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

	resp, err := e.completeWithTransientRetry(ctx, llm.CompletionRequest{
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
		return e.fallback(event), nil
	}

	if resp == nil {
		e.logger.Warn("signal evaluator: LLM returned nil response, dropping signal",
			slog.String("source", event.Source),
			slog.String("title", event.Title),
		)
		if e.metrics != nil {
			e.metrics.RecordSignalParseFailure()
		}
		return e.fallback(event), nil
	}

	content := strings.TrimSpace(resp.Content)
	if content == "" {
		e.logger.Warn("signal evaluator: LLM returned empty response, using fallback",
			slog.String("source", event.Source),
			slog.String("title", event.Title),
		)
		if e.metrics != nil {
			e.metrics.RecordSignalParseFailure()
		}
		return e.fallback(event), nil
	}

	return e.parseResponse(content, event, strategies)
}

type evaluatorOutput struct {
	AffectedStrategyIDs *[]string `json:"affected_strategy_ids"`
	Urgency             *int      `json:"urgency"`
	Summary             *string   `json:"summary"`
	RecommendedAction   *string   `json:"recommended_action"`
}

func (e *Evaluator) parseResponse(content string, event RawSignalEvent, strategies []StrategyContext) (*EvaluatedSignal, error) {
	var out evaluatorOutput
	jsonContent := extractJSONObject(content)
	decoder := json.NewDecoder(strings.NewReader(jsonContent))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&out); err != nil {
		e.logger.Warn("signal evaluator: failed to parse LLM output, dropping signal",
			slog.Any("error", err),
		)
		if e.metrics != nil {
			e.metrics.RecordSignalParseFailure()
		}
		return e.fallback(event), nil
	}
	if out.AffectedStrategyIDs == nil || out.Urgency == nil || out.Summary == nil || out.RecommendedAction == nil {
		return e.invalidOutput(event, "missing_required_field")
	}
	if *out.Urgency < 1 || *out.Urgency > 5 {
		return e.invalidOutput(event, "urgency_out_of_range")
	}
	if strings.TrimSpace(*out.Summary) == "" {
		return e.invalidOutput(event, "missing_summary")
	}
	switch *out.RecommendedAction {
	case "monitor", "re-evaluate", "execute_thesis":
	default:
		return e.invalidOutput(event, "invalid_recommended_action")
	}

	// Build known-ID set to filter LLM hallucinations.
	idSet := make(map[uuid.UUID]struct{}, len(strategies))
	for _, s := range strategies {
		idSet[s.ID] = struct{}{}
	}

	affected := make([]uuid.UUID, 0, len(*out.AffectedStrategyIDs))
	seen := make(map[uuid.UUID]struct{}, len(*out.AffectedStrategyIDs))
	for _, idStr := range *out.AffectedStrategyIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return e.invalidOutput(event, "invalid_strategy_id")
		}
		if _, ok := idSet[id]; ok {
			if _, duplicate := seen[id]; !duplicate {
				affected = append(affected, id)
				seen[id] = struct{}{}
			}
		} else {
			return e.invalidOutput(event, "unknown_strategy_id")
		}
	}

	return &EvaluatedSignal{
		Raw:                event,
		AffectedStrategies: affected,
		Urgency:            *out.Urgency,
		Summary:            strings.TrimSpace(*out.Summary),
		RecommendedAction:  *out.RecommendedAction,
	}, nil
}

func (e *Evaluator) invalidOutput(event RawSignalEvent, reason string) (*EvaluatedSignal, error) {
	e.logger.Warn("signal evaluator: invalid LLM output, dropping signal",
		slog.String("source", event.Source),
		slog.String("title", event.Title),
		slog.String("reason", reason),
	)
	if e.metrics != nil {
		e.metrics.RecordSignalParseFailure()
	}
	return e.fallback(event), nil
}

func extractJSONObject(content string) string {
	trimmed := strings.TrimSpace(content)
	trimmed = strings.TrimPrefix(trimmed, "```json")
	trimmed = strings.TrimPrefix(trimmed, "```")
	trimmed = strings.TrimSuffix(trimmed, "```")
	trimmed = strings.TrimSpace(trimmed)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		return trimmed
	}

	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}

func (e *Evaluator) completeWithTransientRetry(ctx context.Context, request llm.CompletionRequest) (*llm.CompletionResponse, error) {
	resp, err := e.provider.Complete(ctx, request)
	if err != nil {
		if !shouldRetrySignalEvalError(err) {
			return resp, err
		}
		return e.provider.Complete(ctx, request)
	}
	// Treat empty content as a transient failure (broker dedup cache hit,
	// upstream model produced no tokens under JSON-object mode, etc.) and
	// retry once before falling back.
	if resp != nil && strings.TrimSpace(resp.Content) == "" {
		if ctx.Err() != nil {
			return resp, nil
		}
		retryResp, retryErr := e.provider.Complete(ctx, request)
		if retryErr != nil {
			// Surface the retry error so the caller logs it instead of
			// silently using the empty fallback.
			return retryResp, retryErr
		}
		return retryResp, nil
	}
	return resp, nil
}

func shouldRetrySignalEvalError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "context deadline exceeded") ||
		strings.Contains(errText, "broker connection closed before response was received")
}

func (e *Evaluator) fallback(event RawSignalEvent) *EvaluatedSignal {
	// Evaluator failure must never create actionable work. Urgency 1 and no
	// affected strategies make the lifecycle retain observability while dropping
	// the signal before trigger emission.
	return &EvaluatedSignal{
		Raw:                event,
		AffectedStrategies: nil,
		Urgency:            1,
		Summary:            event.Title,
		RecommendedAction:  "monitor",
	}
}
