package signal

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	SignalEvaluatedEventKind        = "signal.evaluated"
	SignalTriggerRequestedEventKind = "signal.trigger_requested"
	SignalTriggerOutcomeEventKind   = "signal.trigger_outcome"
	signalEventPersistenceTimeout   = 5 * time.Second
)

// EventRecorder durably records evaluated signals and scheduler trigger
// requests. When configured, callers fail closed if either write is lost.
type EventRecorder interface {
	RecordEvaluated(context.Context, EvaluatedSignal) error
	RecordTriggerRequest(context.Context, TriggerEvent) error
	RecordTriggerOutcome(context.Context, TriggerEvent, domain.StrategyTriggerOutcome) error
}

func (r *AgentEventRecorder) RecordTriggerOutcome(ctx context.Context, trigger TriggerEvent, outcome domain.StrategyTriggerOutcome) error {
	if r == nil || r.writer == nil {
		return fmt.Errorf("signal event recorder: writer unavailable")
	}
	metadata, err := json.Marshal(map[string]any{
		"received_at": trigger.Signal.Raw.ReceivedAt,
		"source":      trigger.Signal.Raw.Source,
		"urgency":     trigger.Signal.Urgency,
		"action":      trigger.Action,
		"priority":    trigger.Priority,
		"input_hash":  signalInputHash(trigger.Signal.Raw),
		"outcome":     outcome,
	})
	if err != nil {
		return fmt.Errorf("signal event recorder: marshal trigger outcome: %w", err)
	}
	strategyID := trigger.StrategyID
	event := &domain.AgentEvent{
		StrategyID: &strategyID,
		EventKind:  SignalTriggerOutcomeEventKind,
		Title:      trigger.Signal.Raw.Title,
		Summary:    trigger.Signal.Summary,
		Tags: []string{
			"signal",
			"source:" + trigger.Signal.Raw.Source,
			"outcome:" + string(outcome),
		},
		Metadata: metadata,
	}
	return r.create(ctx, event)
}

type agentEventWriter interface {
	Create(context.Context, *domain.AgentEvent) error
}

// AgentEventRecorder writes compact signal lineage into the existing
// partitioned agent_events ledger. Raw provider bodies and prompts are never
// persisted; an input hash provides correlation without duplicating content.
type AgentEventRecorder struct {
	writer agentEventWriter
}

func NewAgentEventRecorder(writer agentEventWriter) *AgentEventRecorder {
	return &AgentEventRecorder{writer: writer}
}

func (r *AgentEventRecorder) RecordEvaluated(ctx context.Context, signal EvaluatedSignal) error {
	if r == nil || r.writer == nil {
		return fmt.Errorf("signal event recorder: writer unavailable")
	}
	strategyIDs := make([]string, 0, len(signal.AffectedStrategies))
	for _, id := range signal.AffectedStrategies {
		strategyIDs = append(strategyIDs, id.String())
	}
	metadata, err := json.Marshal(map[string]any{
		"received_at":           signal.Raw.ReceivedAt,
		"source":                signal.Raw.Source,
		"urgency":               signal.Urgency,
		"recommended_action":    signal.RecommendedAction,
		"affected_strategy_ids": strategyIDs,
		"input_hash":            signalInputHash(signal.Raw),
	})
	if err != nil {
		return fmt.Errorf("signal event recorder: marshal evaluated event: %w", err)
	}
	event := &domain.AgentEvent{
		EventKind: SignalEvaluatedEventKind,
		Title:     signal.Raw.Title,
		Summary:   signal.Summary,
		Tags: []string{
			"signal",
			"source:" + signal.Raw.Source,
			"urgency:" + strconv.Itoa(signal.Urgency),
		},
		Metadata: metadata,
	}
	return r.create(ctx, event)
}

func (r *AgentEventRecorder) RecordTriggerRequest(ctx context.Context, trigger TriggerEvent) error {
	if r == nil || r.writer == nil {
		return fmt.Errorf("signal event recorder: writer unavailable")
	}
	metadata, err := json.Marshal(map[string]any{
		"received_at": trigger.Signal.Raw.ReceivedAt,
		"source":      trigger.Signal.Raw.Source,
		"urgency":     trigger.Signal.Urgency,
		"action":      trigger.Action,
		"priority":    trigger.Priority,
		"input_hash":  signalInputHash(trigger.Signal.Raw),
	})
	if err != nil {
		return fmt.Errorf("signal event recorder: marshal trigger event: %w", err)
	}
	strategyID := trigger.StrategyID
	event := &domain.AgentEvent{
		StrategyID: &strategyID,
		EventKind:  SignalTriggerRequestedEventKind,
		Title:      trigger.Signal.Raw.Title,
		Summary:    trigger.Signal.Summary,
		Tags: []string{
			"signal",
			"source:" + trigger.Signal.Raw.Source,
			"action:" + string(trigger.Action),
		},
		Metadata: metadata,
	}
	return r.create(ctx, event)
}

func (r *AgentEventRecorder) create(ctx context.Context, event *domain.AgentEvent) error {
	persistCtx, cancel := context.WithTimeout(ctx, signalEventPersistenceTimeout)
	defer cancel()
	if err := r.writer.Create(persistCtx, event); err != nil {
		return fmt.Errorf("signal event recorder: persist %s: %w", event.EventKind, err)
	}
	return nil
}

func signalInputHash(event RawSignalEvent) string {
	sum := sha256.Sum256([]byte(event.Source + "\n" + event.Title + "\n" + event.Body + "\n" + event.ReceivedAt.UTC().Format(time.RFC3339Nano)))
	return hex.EncodeToString(sum[:])
}
