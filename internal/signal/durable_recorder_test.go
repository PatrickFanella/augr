package signal

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type recordingAgentEventWriter struct {
	events []domain.AgentEvent
	err    error
}

func (w *recordingAgentEventWriter) Create(_ context.Context, event *domain.AgentEvent) error {
	if w.err != nil {
		return w.err
	}
	w.events = append(w.events, *event)
	return nil
}

func TestAgentEventRecorderPersistsSanitizedSignalLineage(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	writer := &recordingAgentEventWriter{}
	recorder := NewAgentEventRecorder(writer)
	receivedAt := time.Date(2026, time.August, 6, 15, 41, 43, 0, time.UTC)
	signal := EvaluatedSignal{
		Raw: RawSignalEvent{
			Source:     "rss:test",
			Title:      "Current headline",
			Body:       "provider body that must not be persisted",
			Metadata:   map[string]any{"feed": "test"},
			ReceivedAt: receivedAt,
		},
		AffectedStrategies: []uuid.UUID{strategyID},
		Urgency:            3,
		Summary:            "Material update",
		RecommendedAction:  "re-evaluate",
	}

	if err := recorder.RecordEvaluated(context.Background(), signal); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordTriggerRequest(context.Background(), TriggerEvent{
		Signal:     signal,
		StrategyID: strategyID,
		Action:     TriggerActionRunPipeline,
		Priority:   3,
	}); err != nil {
		t.Fatal(err)
	}
	if err := recorder.RecordTriggerOutcome(context.Background(), TriggerEvent{
		Signal:     signal,
		StrategyID: strategyID,
		Action:     TriggerActionRunPipeline,
		Priority:   3,
	}, domain.StrategyTriggerAdmitted); err != nil {
		t.Fatal(err)
	}

	if got := len(writer.events); got != 3 {
		t.Fatalf("events = %d, want 3", got)
	}
	if got := writer.events[0].EventKind; got != SignalEvaluatedEventKind {
		t.Fatalf("evaluated event kind = %q", got)
	}
	if got := writer.events[1].EventKind; got != SignalTriggerRequestedEventKind {
		t.Fatalf("trigger event kind = %q", got)
	}
	if got := writer.events[2].EventKind; got != SignalTriggerOutcomeEventKind {
		t.Fatalf("trigger outcome event kind = %q", got)
	}
	var outcomeMetadata map[string]any
	if err := json.Unmarshal(writer.events[2].Metadata, &outcomeMetadata); err != nil {
		t.Fatal(err)
	}
	if got := outcomeMetadata["outcome"]; got != string(domain.StrategyTriggerAdmitted) {
		t.Fatalf("trigger outcome = %v, want admitted", got)
	}
	if writer.events[1].StrategyID == nil || *writer.events[1].StrategyID != strategyID {
		t.Fatalf("trigger strategy = %v, want %s", writer.events[1].StrategyID, strategyID)
	}
	for _, event := range writer.events {
		if string(event.Metadata) == "" || !json.Valid(event.Metadata) {
			t.Fatalf("invalid metadata: %q", event.Metadata)
		}
		if string(event.Metadata) == "provider body that must not be persisted" {
			t.Fatal("raw provider body was persisted")
		}
		var metadata map[string]any
		if err := json.Unmarshal(event.Metadata, &metadata); err != nil {
			t.Fatal(err)
		}
		if _, ok := metadata["body"]; ok {
			t.Fatalf("raw body key persisted: %#v", metadata)
		}
		if metadata["input_hash"] == "" {
			t.Fatalf("input hash missing: %#v", metadata)
		}
	}
}

func TestSignalLifecycleDurableEvaluationFailureFailsClosed(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	provider := &countingLifecycleStrategyProvider{strategies: []StrategyWithThesis{{
		ID: strategyID, Ticker: "AAPL", WatchTerms: []string{"apple"},
	}}}
	index := NewWatchIndex()
	index.Rebuild(provider.strategies)
	triggerCh := make(chan TriggerEvent, 1)
	store := NewEventStore(1)
	recorder := &failingSignalEventRecorder{evaluatedErr: errors.New("database unavailable")}
	lifecycle := NewLifecycle(index, provider, &fakeLifecycleEvaluator{affected: []uuid.UUID{strategyID}}, triggerCh, store, nil).
		WithEventRecorder(recorder)

	lifecycle.Process(context.Background(), RawSignalEvent{Source: "rss:test", Title: "Apple update", Body: "apple"})

	select {
	case trigger := <-triggerCh:
		t.Fatalf("unexpected trigger after persistence failure: %+v", trigger)
	default:
	}
	if got := len(store.ListSignals(0, 0, 0)); got != 0 {
		t.Fatalf("in-memory signals = %d, want 0 after durable failure", got)
	}
}

func TestTriggerHandlerDurableRequestFailureDoesNotDispatch(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	runner := &fakeLifecycleStrategyTriggerer{}
	handler := NewTriggerHandler(
		nil,
		&fakeLifecycleStrategyLoader{strategies: map[uuid.UUID]*domain.Strategy{strategyID: {ID: strategyID}}},
		nil,
		runner,
		nil,
		nil,
	).WithEventRecorder(&failingSignalEventRecorder{triggerErr: errors.New("database unavailable")})

	handler.handle(context.Background(), TriggerEvent{
		Signal: EvaluatedSignal{
			Raw:                RawSignalEvent{Source: "rss:test", Title: "Apple update"},
			AffectedStrategies: []uuid.UUID{strategyID},
			Urgency:            3,
			Summary:            "Material update",
			RecommendedAction:  "re-evaluate",
		},
		StrategyID: strategyID,
		Action:     TriggerActionRunPipeline,
		Priority:   3,
	})

	if got := len(runner.calls); got != 0 {
		t.Fatalf("runner calls = %d, want 0 after durable failure", got)
	}
}

func TestTriggerHandlerDurableOutcomeFailureDoesNotDispatch(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	runner := &fakeLifecycleStrategyTriggerer{}
	handler := NewTriggerHandler(
		nil,
		&fakeLifecycleStrategyLoader{strategies: map[uuid.UUID]*domain.Strategy{strategyID: {ID: strategyID}}},
		nil,
		runner,
		nil,
		nil,
	).WithEventRecorder(&failingSignalEventRecorder{outcomeErr: errors.New("database unavailable")})

	handler.handle(context.Background(), TriggerEvent{
		Signal: EvaluatedSignal{
			Raw:                RawSignalEvent{Source: "rss:test", Title: "Apple update"},
			AffectedStrategies: []uuid.UUID{strategyID},
			Urgency:            3,
			Summary:            "Material update",
			RecommendedAction:  "re-evaluate",
		},
		StrategyID: strategyID,
		Action:     TriggerActionRunPipeline,
		Priority:   3,
	})

	if got := len(runner.calls); got != 0 {
		t.Fatalf("runner calls = %d, want 0 after outcome persistence failure", got)
	}
}

type failingSignalEventRecorder struct {
	evaluatedErr error
	triggerErr   error
	outcomeErr   error
}

func (r *failingSignalEventRecorder) RecordEvaluated(context.Context, EvaluatedSignal) error {
	return r.evaluatedErr
}

func (r *failingSignalEventRecorder) RecordTriggerRequest(context.Context, TriggerEvent) error {
	return r.triggerErr
}

func (r *failingSignalEventRecorder) RecordTriggerOutcome(context.Context, TriggerEvent, domain.StrategyTriggerOutcome) error {
	return r.outcomeErr
}
