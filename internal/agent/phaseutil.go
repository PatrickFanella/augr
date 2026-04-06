package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// snapshotPersistTimeout is the deadline for persisting pipeline snapshots.
// It is independent of the phase timeout so that slow data fetching doesn't
// cause snapshot writes to fail. Overridden in tests.
var snapshotPersistTimeout = 10 * time.Second

// PhaseHelper encapsulates event emission, structured event persistence, and
// snapshot persistence shared by both Pipeline and Runner. Both orchestrators
// embed a PhaseHelper and delegate to it instead of carrying duplicate copies of
// these methods.
type PhaseHelper struct {
	persister DecisionPersister
	events    chan<- PipelineEvent
	logger    *slog.Logger
	clock     func() time.Time
}

func newPhaseHelper(persister DecisionPersister, events chan<- PipelineEvent, logger *slog.Logger, clock func() time.Time) PhaseHelper {
	if logger == nil {
		logger = slog.Default()
	}
	if clock == nil {
		clock = time.Now
	}
	return PhaseHelper{
		persister: persister,
		events:    events,
		logger:    logger,
		clock:     clock,
	}
}

func (h *PhaseHelper) emitEvent(event PipelineEvent) {
	if h.events == nil {
		return
	}
	select {
	case h.events <- event:
	default:
		h.logger.Debug("agent: event dropped; events channel full", slog.String("type", string(event.Type)))
	}
}

func (h *PhaseHelper) persistStructuredEvent(ctx context.Context, event *domain.AgentEvent) {
	if event == nil {
		return
	}
	if err := h.persister.PersistEvent(ctx, event); err != nil {
		h.logger.Warn("agent: failed to persist structured event", slog.String("event_kind", event.EventKind), slog.Any("error", err))
	}
}

func (h *PhaseHelper) persistStructuredTerminalEvent(event *domain.AgentEvent) {
	dbCtx, cancel := context.WithTimeout(context.Background(), statusUpdateTimeout)
	defer cancel()
	h.persistStructuredEvent(dbCtx, event)
}

func (h *PhaseHelper) newStructuredEvent(runID, strategyID uuid.UUID, kind AgentEventKind, agentRole AgentRole, title, summary string, metadata map[string]any, tags []string) *domain.AgentEvent {
	event := &domain.AgentEvent{
		PipelineRunID: &runID,
		StrategyID:    &strategyID,
		EventKind:     kind.String(),
		Title:         title,
		Summary:       summary,
		Tags:          append([]string(nil), tags...),
		Metadata:      h.marshalStructuredEventMetadata(metadata),
	}
	if agentRole != "" {
		event.AgentRole = agentRole
	}
	return event
}

func (h *PhaseHelper) marshalStructuredEventMetadata(metadata map[string]any) json.RawMessage {
	if len(metadata) == 0 {
		return nil
	}
	payload, err := json.Marshal(metadata)
	if err != nil {
		h.logger.Warn("agent: failed to marshal structured event metadata", slog.Any("error", err))
		return nil
	}
	return payload
}

func (h *PhaseHelper) emitCacheStats(state *PipelineState, collector *llm.CacheStatsCollector, runID, strategyID uuid.UUID, ticker string) {
	stats := collector.Snapshot()
	if state != nil {
		state.LLMCacheStats = stats
	}
	payload, err := json.Marshal(stats)
	if err != nil {
		h.logger.Warn("agent: failed to marshal LLM cache stats", slog.Any("error", err))
		return
	}
	h.emitEvent(PipelineEvent{
		Type:          LLMCacheStatsReported,
		PipelineRunID: runID,
		StrategyID:    strategyID,
		Ticker:        ticker,
		Payload:       payload,
		OccurredAt:    h.clock().UTC(),
	})
}

func (h *PhaseHelper) persistAnalysisSnapshots(ctx context.Context, state *PipelineState) error {
	if !h.persister.SupportsSnapshots() {
		return nil
	}
	snapshots := []struct {
		dataType string
		payload  any
	}{
		{dataType: "market", payload: state.Market},
		{dataType: "news", payload: state.News},
		{dataType: "fundamentals", payload: state.Fundamentals},
		{dataType: "social", payload: state.Social},
	}
	for _, snapshotData := range snapshots {
		payload, err := json.Marshal(snapshotData.payload)
		if err != nil {
			return fmt.Errorf("agent: marshal %s snapshot: %w", snapshotData.dataType, err)
		}
		// Use a fresh context for DB writes so they don't fail when the
		// analysis context is nearly exhausted from data fetching.
		persistCtx, persistCancel := context.WithTimeout(context.WithoutCancel(ctx), snapshotPersistTimeout)
		err = h.persister.PersistSnapshot(persistCtx, &domain.PipelineRunSnapshot{
			PipelineRunID: state.PipelineRunID,
			DataType:      snapshotData.dataType,
			Payload:       payload,
		})
		persistCancel()
		if err != nil {
			return fmt.Errorf("agent/pipeline: persist snapshot %s: %w", snapshotData.dataType, err)
		}
	}
	return nil
}
