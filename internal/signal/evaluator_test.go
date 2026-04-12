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
		WithMetrics(m).
		WithFallbackMode("drop")

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

// TestEvaluatorFallback_LegacyMode verifies urgency=3 and all strategies in legacy mode.
func TestEvaluatorFallback_LegacyMode(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID1 := uuid.New()
	strategyID2 := uuid.New()
	e := NewEvaluator(stubEvaluatorProvider{err: errors.New("boom")}, "quick", nil).
		WithMetrics(m).
		WithFallbackMode("legacy")

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID1}, {ID: strategyID2}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 3 {
		t.Fatalf("legacy mode: Urgency = %d, want 3", got.Urgency)
	}
	if len(got.AffectedStrategies) != 2 {
		t.Fatalf("legacy mode: AffectedStrategies len = %d, want 2", len(got.AffectedStrategies))
	}
	if m.parseFailures != 1 {
		t.Fatalf("legacy mode: parseFailures = %d, want 1", m.parseFailures)
	}
}

// TestEvaluatorFallback_ParseFailure_LegacyMode verifies JSON parse failure also triggers metric + legacy.
func TestEvaluatorFallback_ParseFailure_LegacyMode(t *testing.T) {
	t.Parallel()

	m := &stubMetrics{}
	strategyID := uuid.New()
	e := NewEvaluator(stubEvaluatorProvider{response: &llm.CompletionResponse{Content: "bad-json"}}, "quick", nil).
		WithMetrics(m).
		WithFallbackMode("legacy")

	got, err := e.Evaluate(context.Background(), RawSignalEvent{Source: "rss", Title: "headline"},
		[]StrategyContext{{ID: strategyID}})
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}
	if got.Urgency != 3 {
		t.Fatalf("parse fail legacy: Urgency = %d, want 3", got.Urgency)
	}
	if len(got.AffectedStrategies) != 1 {
		t.Fatalf("parse fail legacy: AffectedStrategies len = %d, want 1", len(got.AffectedStrategies))
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

func TestSignalHubProcess_DropsLowUrgencyFallback(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	triggerCh := make(chan TriggerEvent, 1)
	hub := NewSignalHub(
		nil,
		NewEvaluator(stubEvaluatorProvider{err: errors.New("boom")}, "quick", nil),
		NewWatchIndex(),
		stubStrategyProvider{strategies: []StrategyWithThesis{{ID: strategyID, Ticker: "AAPL", WatchTerms: []string{"apple"}}}},
		triggerCh,
		nil,
		nil,
	)
	hub.watchIndex.Rebuild([]StrategyWithThesis{{ID: strategyID, Ticker: "AAPL", WatchTerms: []string{"apple"}}})

	hub.process(context.Background(), RawSignalEvent{Source: "rss", Title: "Apple jumps", Body: "apple rally"})

	select {
	case trigger := <-triggerCh:
		t.Fatalf("unexpected trigger emitted: %+v", trigger)
	default:
	}
}

type stubStrategyProvider struct {
	strategies []StrategyWithThesis
	err        error
}

func (s stubStrategyProvider) ListActiveWithThesis(context.Context) ([]StrategyWithThesis, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.strategies, nil
}
