package signal

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

func TestSignalLifecycle_EndToEnd_RoutesInOrderAndCachesStrategies(t *testing.T) {
	t.Parallel()

	strategyAID := uuid.New()
	strategyBID := uuid.New()

	baseProvider := &countingLifecycleStrategyProvider{strategies: []StrategyWithThesis{
		{ID: strategyAID, Ticker: "AAPL", WatchTerms: []string{"apple"}},
		{ID: strategyBID, Ticker: "MSFT", WatchTerms: []string{"microsoft"}},
	}}
	provider := NewStrategyProviderWithCache(baseProvider, time.Hour)

	evaluator := &fakeLifecycleEvaluator{
		affected: []uuid.UUID{strategyBID, strategyAID},
	}

	triggerCh := make(chan TriggerEvent, 8)
	store := NewEventStore(8)
	source := &fakeLifecycleSource{events: []RawSignalEvent{{
		Source: "fake-source",
		Title:  "Apple and Microsoft rally",
		Body:   "apple microsoft earnings",
	}}}
	lifecycle := NewLifecycle(NewWatchIndex(), provider, evaluator, triggerCh, store, slog.Default())
	hub := NewSignalHub([]SignalSource{source}, lifecycle, slog.Default())

	loader := &fakeLifecycleStrategyLoader{strategies: map[uuid.UUID]*domain.Strategy{
		strategyAID: {ID: strategyAID},
		strategyBID: {ID: strategyBID},
	}}
	runner := &fakeLifecycleStrategyTriggerer{done: make(chan struct{})}
	handler := NewTriggerHandler(triggerCh, loader, nil, runner, store, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go handler.Run(ctx)
	if err := hub.Start(ctx); err != nil {
		t.Fatalf("hub.Start() error = %v", err)
	}

	select {
	case <-runner.done:
	case <-ctx.Done():
		t.Fatal("timed out waiting for trigger routing")
	}

	cancel()
	hub.Stop()

	if got := baseProvider.calls; got != 1 {
		t.Fatalf("strategy provider calls = %d, want 1 via cache", got)
	}
	if got := source.starts; got != 1 {
		t.Fatalf("source starts = %d, want 1", got)
	}
	if got := evaluator.calls; got != 1 {
		t.Fatalf("evaluator calls = %d, want 1", got)
	}
	if got := loader.calls; len(got) != 2 || got[0] != strategyBID || got[1] != strategyAID {
		t.Fatalf("strategy loader calls = %v, want [%s %s]", got, strategyBID, strategyAID)
	}
	if got := runner.calls; len(got) != 2 || got[0] != strategyBID || got[1] != strategyAID {
		t.Fatalf("runner calls = %v, want [%s %s]", got, strategyBID, strategyAID)
	}
	if got := len(store.ListSignals(0, 0, 0)); got != 1 {
		t.Fatalf("stored signals = %d, want 1", got)
	}
	if got := len(store.ListTriggers(0, 0)); got != 2 {
		t.Fatalf("stored triggers = %d, want 2", got)
	}
	if got := evaluator.received; len(got) != 1 || len(got[0]) != 2 {
		t.Fatalf("evaluator contexts = %v, want one matched pair", got)
	}
	if got := evaluator.received[0]; got[0].ID != strategyAID || got[1].ID != strategyBID {
		t.Fatalf("evaluator contexts order = %v, want [%s %s]", got, strategyAID, strategyBID)
	}
	if got := evaluator.sourceTitles; len(got) != 1 || got[0] != "Apple and Microsoft rally" {
		t.Fatalf("evaluator titles = %v, want source title", got)
	}
}

type fakeLifecycleSource struct {
	events []RawSignalEvent
	mu     sync.Mutex
	starts int
}

func (f *fakeLifecycleSource) Name() string { return "fake-source" }

func (f *fakeLifecycleSource) Start(ctx context.Context) (<-chan RawSignalEvent, error) {
	f.mu.Lock()
	f.starts++
	f.mu.Unlock()

	out := make(chan RawSignalEvent, len(f.events))
	go func() {
		defer close(out)
		for _, evt := range f.events {
			select {
			case out <- evt:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

type countingLifecycleStrategyProvider struct {
	mu         sync.Mutex
	calls      int
	strategies []StrategyWithThesis
}

func (p *countingLifecycleStrategyProvider) ListActiveWithThesis(context.Context) ([]StrategyWithThesis, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	out := make([]StrategyWithThesis, len(p.strategies))
	copy(out, p.strategies)
	return out, nil
}

type fakeLifecycleEvaluator struct {
	mu           sync.Mutex
	calls        int
	received     [][]StrategyContext
	sourceTitles []string
	affected     []uuid.UUID
	urgency      int
	summary      string
	action       string
	err          error
}

func (f *fakeLifecycleEvaluator) Evaluate(_ context.Context, evt RawSignalEvent, strategies []StrategyContext) (*EvaluatedSignal, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	copyStrategies := make([]StrategyContext, len(strategies))
	copy(copyStrategies, strategies)
	f.received = append(f.received, copyStrategies)
	f.sourceTitles = append(f.sourceTitles, evt.Title)
	if f.err != nil {
		return nil, f.err
	}
	urgency := f.urgency
	if urgency == 0 {
		urgency = 4
	}
	summary := f.summary
	if summary == "" {
		summary = "material signal"
	}
	action := f.action
	if action == "" {
		action = "re-evaluate"
	}
	return &EvaluatedSignal{
		Raw:                evt,
		AffectedStrategies: append([]uuid.UUID(nil), f.affected...),
		Urgency:            urgency,
		Summary:            summary,
		RecommendedAction:  action,
	}, nil
}

func TestSignalLifecycle_EvaluatorUnavailableOrFailedIsNotActionable(t *testing.T) {
	t.Parallel()

	event := RawSignalEvent{Source: "rss", Title: "headline"}
	strategies := []StrategyContext{{ID: uuid.New()}}
	tests := map[string]SignalEvaluator{
		"unavailable": nil,
		"failed":      &fakeLifecycleEvaluator{err: errors.New("provider failed")},
	}
	for name, evaluator := range tests {
		name, evaluator := name, evaluator
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lifecycle := NewLifecycle(nil, nil, evaluator, nil, nil, slog.Default())
			got := lifecycle.evaluate(context.Background(), event, strategies)
			if got == nil || got.Urgency != 1 || len(got.AffectedStrategies) != 0 {
				t.Fatalf("evaluate() = %+v, want non-actionable fallback", got)
			}
			if got.RecommendedAction != "monitor" {
				t.Fatalf("RecommendedAction = %q, want monitor", got.RecommendedAction)
			}
		})
	}
}

type fakeLifecycleStrategyLoader struct {
	mu         sync.Mutex
	calls      []uuid.UUID
	strategies map[uuid.UUID]*domain.Strategy
}

func (l *fakeLifecycleStrategyLoader) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, id)
	strat, ok := l.strategies[id]
	if !ok {
		return nil, errors.New("missing strategy")
	}
	return strat, nil
}

type fakeLifecycleStrategyTriggerer struct {
	mu    sync.Mutex
	calls []uuid.UUID
	done  chan struct{}
	once  sync.Once
}

func (r *fakeLifecycleStrategyTriggerer) TriggerStrategy(strategy domain.Strategy) {
	r.mu.Lock()
	r.calls = append(r.calls, strategy.ID)
	ready := len(r.calls) == 2
	r.mu.Unlock()
	if ready {
		r.once.Do(func() { close(r.done) })
	}
}
