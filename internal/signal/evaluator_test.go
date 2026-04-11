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
