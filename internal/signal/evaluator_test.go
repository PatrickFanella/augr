package signal

import (
	"context"
	"errors"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/google/uuid"
)

type stubEvaluatorProvider struct {
	response *llm.CompletionResponse
	err      error
}

func (s stubEvaluatorProvider) Complete(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

type scriptedEvaluatorResult struct {
	response *llm.CompletionResponse
	err      error
}

type scriptedEvaluatorProvider struct {
	results []scriptedEvaluatorResult
	calls   int
}

func (s *scriptedEvaluatorProvider) Complete(context.Context, llm.CompletionRequest) (*llm.CompletionResponse, error) {
	s.calls++
	if len(s.results) == 0 {
		return nil, errors.New("no scripted evaluator results")
	}
	idx := s.calls - 1
	if idx >= len(s.results) {
		idx = len(s.results) - 1
	}
	result := s.results[idx]
	if result.err != nil {
		return nil, result.err
	}
	return result.response, nil
}

// stubMetrics counts RecordSignalParseFailure calls.
type stubMetrics struct {
	parseFailures int
}

func (m *stubMetrics) RecordSignalParseFailure() {
	m.parseFailures++
}

func TestEvaluatorEvaluate_ProviderErrorFallsBackToLowUrgency(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	evaluator := NewEvaluator(stubEvaluatorProvider{err: errors.New("boom")}, "quick", nil)

	got, err := evaluator.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"}, []StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got == nil {
		t.Fatal("Evaluate() = nil")
	}
	if got.Urgency != 1 {
		t.Fatalf("Urgency = %d, want 1", got.Urgency)
	}
	if len(got.AffectedStrategies) != 0 {
		t.Fatalf("AffectedStrategies = %v, want empty", got.AffectedStrategies)
	}
	if got.RecommendedAction != "monitor" {
		t.Fatalf("RecommendedAction = %q, want monitor", got.RecommendedAction)
	}
}

func TestEvaluatorParseResponse_InvalidJSONFallsBackToLowUrgency(t *testing.T) {
	t.Parallel()

	evaluator := NewEvaluator(stubEvaluatorProvider{response: &llm.CompletionResponse{Content: "not-json"}}, "quick", nil)

	got, err := evaluator.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"}, []StrategyContext{{ID: uuid.New()}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got == nil {
		t.Fatal("Evaluate() = nil")
	}
	if got.Urgency != 1 {
		t.Fatalf("Urgency = %d, want 1", got.Urgency)
	}
	if len(got.AffectedStrategies) != 0 {
		t.Fatalf("AffectedStrategies = %v, want empty", got.AffectedStrategies)
	}
}

// TestEvaluatorFallback_DropMode verifies urgency=1 and no affected strategies in drop mode.
func TestEvaluatorFallback_DropMode(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID := uuid.New()
	e := NewEvaluator(stubEvaluatorProvider{err: errors.New("boom")}, "quick", nil).
		WithMetrics(m)

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 1 {
		t.Fatalf("drop mode: Urgency = %d, want 1", got.Urgency)
	}
	if len(got.AffectedStrategies) != 0 {
		t.Fatalf("drop mode: AffectedStrategies = %v, want empty", got.AffectedStrategies)
	}
	if m.parseFailures != 1 {
		t.Fatalf("drop mode: parseFailures = %d, want 1", m.parseFailures)
	}
}

// TestEvaluatorFallback_CannotBeConfiguredActionable verifies failure is always fail-closed.
func TestEvaluatorFallback_CannotBeConfiguredActionable(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID1 := uuid.New()
	strategyID2 := uuid.New()
	e := NewEvaluator(stubEvaluatorProvider{err: errors.New("boom")}, "quick", nil).
		WithMetrics(m)

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID1}, {ID: strategyID2}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 1 {
		t.Fatalf("Urgency = %d, want 1", got.Urgency)
	}
	if len(got.AffectedStrategies) != 0 {
		t.Fatalf("AffectedStrategies = %v, want empty", got.AffectedStrategies)
	}
	if m.parseFailures != 1 {
		t.Fatalf("legacy mode: parseFailures = %d, want 1", m.parseFailures)
	}
}

// TestEvaluatorFallback_ParseFailureIsNotActionable verifies malformed JSON stays fail-closed.
func TestEvaluatorFallback_ParseFailureIsNotActionable(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID := uuid.New()
	e := NewEvaluator(stubEvaluatorProvider{response: &llm.CompletionResponse{Content: "bad-json"}}, "quick", nil).
		WithMetrics(m)

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 1 {
		t.Fatalf("Urgency = %d, want 1", got.Urgency)
	}
	if len(got.AffectedStrategies) != 0 {
		t.Fatalf("AffectedStrategies = %v, want empty", got.AffectedStrategies)
	}
	if m.parseFailures != 1 {
		t.Fatalf("parse fail legacy: parseFailures = %d, want 1", m.parseFailures)
	}
}

// TestEvaluatorSuccess_NoMetricFired verifies successful eval doesn't fire parse-failure metric.
func TestEvaluatorSuccess_NoMetricFired(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID := uuid.New()
	validJSON := `{"affected_strategy_ids":["` + strategyID.String() + `"],"urgency":3,"summary":"test","recommended_action":"monitor"}`
	e := NewEvaluator(stubEvaluatorProvider{response: &llm.CompletionResponse{Content: validJSON}}, "quick", nil).
		WithMetrics(m)

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 3 {
		t.Fatalf("success: Urgency = %d, want 3", got.Urgency)
	}
	if m.parseFailures != 0 {
		t.Fatalf("success: parseFailures = %d, want 0 (metric must not fire on success)", m.parseFailures)
	}
}

func TestEvaluatorParseResponse_ExtractsJSONFromMarkdownFence(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID := uuid.New()
	content := "Here is the evaluation:\n```json\n{" +
		"\"affected_strategy_ids\":[\"" + strategyID.String() + "\"]," +
		"\"urgency\":4,\"summary\":\"material update\",\"recommended_action\":\"re-evaluate\"}" +
		"\n```"
	e := NewEvaluator(stubEvaluatorProvider{response: &llm.CompletionResponse{Content: content}}, "quick", nil).
		WithMetrics(m)

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 4 {
		t.Fatalf("Urgency = %d, want 4", got.Urgency)
	}
	if len(got.AffectedStrategies) != 1 || got.AffectedStrategies[0] != strategyID {
		t.Fatalf("AffectedStrategies = %v, want [%s]", got.AffectedStrategies, strategyID)
	}
	if m.parseFailures != 0 {
		t.Fatalf("parseFailures = %d, want 0", m.parseFailures)
	}
}

// TestEvaluatorFallback_NilMetrics_NoPanic verifies nil metrics doesn't panic.
func TestEvaluatorFallback_NilMetrics_NoPanic(t *testing.T) {
	t.Parallel()

	e := NewEvaluator(stubEvaluatorProvider{err: errors.New("boom")}, "quick", nil)
	// metrics is nil — must not panic
	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: uuid.New()}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got == nil {
		t.Fatal("Evaluate() = nil")
	}
}

func TestEvaluatorEvaluate_NilProviderResponseFallsBack(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	e := NewEvaluator(stubEvaluatorProvider{}, "quick", nil).WithMetrics(m)
	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: uuid.New()}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got == nil || got.Urgency != 1 || len(got.AffectedStrategies) != 0 {
		t.Fatalf("Evaluate() = %+v, want non-actionable fallback", got)
	}
	if m.parseFailures != 1 {
		t.Fatalf("parseFailures = %d, want 1", m.parseFailures)
	}
}

func TestEvaluatorEvaluate_InvalidSchemaFallsBack(t *testing.T) {
	t.Parallel()

	knownID := uuid.New()
	unknownID := uuid.New()
	tests := map[string]string{
		"missing affected strategies": `{"urgency":4,"summary":"material","recommended_action":"re-evaluate"}`,
		"urgency above range":         `{"affected_strategy_ids":["` + knownID.String() + `"],"urgency":6,"summary":"material","recommended_action":"execute_thesis"}`,
		"blank summary":               `{"affected_strategy_ids":["` + knownID.String() + `"],"urgency":4,"summary":" ","recommended_action":"re-evaluate"}`,
		"invalid action":              `{"affected_strategy_ids":["` + knownID.String() + `"],"urgency":4,"summary":"material","recommended_action":"trade_now"}`,
		"malformed strategy id":       `{"affected_strategy_ids":["not-a-uuid"],"urgency":4,"summary":"material","recommended_action":"re-evaluate"}`,
		"unknown strategy id":         `{"affected_strategy_ids":["` + unknownID.String() + `"],"urgency":4,"summary":"material","recommended_action":"re-evaluate"}`,
		"unknown field":               `{"affected_strategy_ids":[],"urgency":1,"summary":"noise","recommended_action":"monitor","execute":true}`,
	}
	for name, content := range tests {
		name, content := name, content
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := &stubMetrics{}
			e := NewEvaluator(stubEvaluatorProvider{response: &llm.CompletionResponse{Content: content}}, "quick", nil).WithMetrics(m)
			got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
				[]StrategyContext{{ID: knownID}})
			if err != nil {
				t.Fatalf("Evaluate() error = %v", err)
			}
			if got == nil || got.Urgency != 1 || len(got.AffectedStrategies) != 0 {
				t.Fatalf("Evaluate() = %+v, want non-actionable fallback", got)
			}
			if m.parseFailures != 1 {
				t.Fatalf("parseFailures = %d, want 1", m.parseFailures)
			}
		})
	}
}

func TestEvaluatorEvaluate_RetriesOnceOnBrokerConnectionClosed(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	validJSON := `{"affected_strategy_ids":["` + strategyID.String() + `"],"urgency":4,"summary":"material update","recommended_action":"re-evaluate"}`
	provider := &scriptedEvaluatorProvider{results: []scriptedEvaluatorResult{
		{err: errors.New(`ollama: complete request: POST "http://host.docker.internal:11434/v1/chat/completions": 502 Bad Gateway {"message":"broker connection closed before response was received","type":"broker_error","code":"broker_error"}`)},
		{response: &llm.CompletionResponse{Content: validJSON}},
	}}
	evaluator := NewEvaluator(provider, "quick", nil)

	got, err := evaluator.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"}, []StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got == nil {
		t.Fatal("Evaluate() = nil")
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if got.Urgency != 4 {
		t.Fatalf("Urgency = %d, want 4", got.Urgency)
	}
	if got.RecommendedAction != "re-evaluate" {
		t.Fatalf("RecommendedAction = %q, want re-evaluate", got.RecommendedAction)
	}
	if len(got.AffectedStrategies) != 1 || got.AffectedStrategies[0] != strategyID {
		t.Fatalf("AffectedStrategies = %v, want [%s]", got.AffectedStrategies, strategyID)
	}
}

func TestEvaluatorEvaluate_RetriesOnceOnMessageDeadlineExceeded(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	validJSON := `{"affected_strategy_ids":["` + strategyID.String() + `"],"urgency":2,"summary":"watch closely","recommended_action":"monitor"}`
	provider := &scriptedEvaluatorProvider{results: []scriptedEvaluatorResult{
		{err: errors.New("ollama: complete request: context deadline exceeded")},
		{response: &llm.CompletionResponse{Content: validJSON}},
	}}
	evaluator := NewEvaluator(provider, "quick", nil)

	got, err := evaluator.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"}, []StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got == nil {
		t.Fatal("Evaluate() = nil")
	}
	if provider.calls != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.calls)
	}
	if got.Urgency != 2 {
		t.Fatalf("Urgency = %d, want 2", got.Urgency)
	}
	if got.RecommendedAction != "monitor" {
		t.Fatalf("RecommendedAction = %q, want monitor", got.RecommendedAction)
	}
}
