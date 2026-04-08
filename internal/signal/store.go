package signal

import (
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	defaultStoreCapacity = 200
)

// StoredSignal is an evaluated signal captured for the signal intelligence UI.
type StoredSignal struct {
	ID         uuid.UUID      `json:"id"`
	ReceivedAt time.Time      `json:"received_at"`
	Source     string         `json:"source"`
	Title      string         `json:"title"`
	Body       string         `json:"body"`
	Urgency    int            `json:"urgency"`
	Summary    string         `json:"summary"`
	Action     string         `json:"recommended_action"`
	Strategies []uuid.UUID    `json:"affected_strategy_ids"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

// StoredTrigger is a TriggerEvent captured for the trigger log UI.
type StoredTrigger struct {
	ID         uuid.UUID `json:"id"`
	FiredAt    time.Time `json:"fired_at"`
	StrategyID uuid.UUID `json:"strategy_id"`
	Action     string    `json:"action"`
	Priority   int       `json:"priority"`
	SignalTitle string   `json:"signal_title"`
	SignalSummary string `json:"signal_summary"`
	Source     string    `json:"source"`
}

// EventStore is a bounded in-memory ring buffer that accumulates evaluated
// signals and trigger events for the signal intelligence UI. Concurrent-safe.
type EventStore struct {
	mu       sync.RWMutex
	signals  []StoredSignal
	triggers []StoredTrigger
	cap      int
}

// NewEventStore creates an EventStore with the given capacity per collection.
// If cap <= 0, defaultStoreCapacity (200) is used.
func NewEventStore(cap int) *EventStore {
	if cap <= 0 {
		cap = defaultStoreCapacity
	}
	return &EventStore{
		signals:  make([]StoredSignal, 0, cap),
		triggers: make([]StoredTrigger, 0, cap),
		cap:      cap,
	}
}

// RecordSignal appends an evaluated signal to the store. Oldest entry is
// dropped when the buffer is full.
func (s *EventStore) RecordSignal(sig EvaluatedSignal) {
	stored := StoredSignal{
		ID:         uuid.New(),
		ReceivedAt: sig.Raw.ReceivedAt,
		Source:     sig.Raw.Source,
		Title:      sig.Raw.Title,
		Body:       sig.Raw.Body,
		Urgency:    sig.Urgency,
		Summary:    sig.Summary,
		Action:     sig.RecommendedAction,
		Strategies: sig.AffectedStrategies,
		Metadata:   sig.Raw.Metadata,
	}
	s.mu.Lock()
	if len(s.signals) >= s.cap {
		s.signals = s.signals[1:]
	}
	s.signals = append(s.signals, stored)
	s.mu.Unlock()
}

// RecordTrigger appends a trigger event to the store.
func (s *EventStore) RecordTrigger(evt TriggerEvent) {
	stored := StoredTrigger{
		ID:            uuid.New(),
		FiredAt:       time.Now(),
		StrategyID:    evt.StrategyID,
		Action:        string(evt.Action),
		Priority:      evt.Priority,
		SignalTitle:   evt.Signal.Raw.Title,
		SignalSummary: evt.Signal.Summary,
		Source:        evt.Signal.Raw.Source,
	}
	s.mu.Lock()
	if len(s.triggers) >= s.cap {
		s.triggers = s.triggers[1:]
	}
	s.triggers = append(s.triggers, stored)
	s.mu.Unlock()
}

// ListSignals returns stored evaluated signals in reverse-chronological order.
func (s *EventStore) ListSignals(minUrgency, limit, offset int) []StoredSignal {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Build filtered reverse slice without allocating twice the slice.
	result := make([]StoredSignal, 0, len(s.signals))
	for i := len(s.signals) - 1; i >= 0; i-- {
		if s.signals[i].Urgency >= minUrgency {
			result = append(result, s.signals[i])
		}
	}
	return paginate(result, limit, offset)
}

// ListTriggers returns stored trigger events in reverse-chronological order.
func (s *EventStore) ListTriggers(limit, offset int) []StoredTrigger {
	s.mu.RLock()
	defer s.mu.RUnlock()

	rev := make([]StoredTrigger, len(s.triggers))
	for i, t := range s.triggers {
		rev[len(s.triggers)-1-i] = t
	}
	return paginate(rev, limit, offset)
}

func paginate[T any](slice []T, limit, offset int) []T {
	if offset >= len(slice) {
		return nil
	}
	slice = slice[offset:]
	if limit > 0 && limit < len(slice) {
		slice = slice[:limit]
	}
	return slice
}
