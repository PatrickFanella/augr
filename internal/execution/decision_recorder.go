package execution

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// DecisionRecorder captures pre-order decisions and later attaches order IDs.
type DecisionRecorder interface {
	RecordDecision(ctx context.Context, decision *domain.TradeDecision) error
	AttachPaperOrder(ctx context.Context, decisionID, orderID uuid.UUID) error
	AttachLiveOrder(ctx context.Context, decisionID, orderID uuid.UUID) error
}

// ReplayDecisionRecorder extends decision persistence with ordered lifecycle
// evidence. Callers use this optional seam so non-durable test recorders remain
// compatible while the production recorder writes the replay ledger.
type ReplayDecisionRecorder interface {
	DecisionRecorder
	RecordReplayEvent(ctx context.Context, decisionID uuid.UUID, eventType domain.ReplayEventType, source string, payload any, occurredAt time.Time) error
}

type tradeDecisionJournalRecorder struct {
	repo       repository.TradeDecisionJournalRepository
	replayRepo repository.ReplayEventRepository
}

// NewTradeDecisionJournalRecorder adapts the Phase 2 repository to the execution seam.
func NewTradeDecisionJournalRecorder(repo repository.TradeDecisionJournalRepository, replayRepos ...repository.ReplayEventRepository) DecisionRecorder {
	if repo == nil {
		return nil
	}
	var replayRepo repository.ReplayEventRepository
	if len(replayRepos) > 0 {
		replayRepo = replayRepos[0]
	}
	return &tradeDecisionJournalRecorder{repo: repo, replayRepo: replayRepo}
}

func (r *tradeDecisionJournalRecorder) RecordDecision(ctx context.Context, decision *domain.TradeDecision) error {
	if r == nil || r.repo == nil || decision == nil {
		return nil
	}
	if err := r.repo.Create(ctx, decision); err != nil {
		return err
	}
	if r.replayRepo == nil {
		return nil
	}
	if err := r.RecordReplayEvent(ctx, decision.ID, domain.ReplayEventTypeDecisionCreated, "decision_journal", decision, decision.CreatedAt); err != nil {
		return err
	}
	return r.RecordReplayEvent(ctx, decision.ID, domain.ReplayEventTypeRiskReviewed, "risk_engine", map[string]any{
		"status": decision.RiskStatus, "reasons": decision.RiskReasons,
		"proposed_size": decision.ProposedSize, "approved_size": decision.ApprovedSize,
	}, decision.UpdatedAt)
}

func (r *tradeDecisionJournalRecorder) AttachPaperOrder(ctx context.Context, decisionID, orderID uuid.UUID) error {
	if r == nil || r.repo == nil {
		return nil
	}
	if err := r.repo.AttachPaperOrder(ctx, decisionID, orderID); err != nil {
		return err
	}
	return r.RecordReplayEvent(ctx, decisionID, domain.ReplayEventTypePaperOrdered, "order_manager", map[string]any{"order_id": orderID}, time.Now().UTC())
}

func (r *tradeDecisionJournalRecorder) AttachLiveOrder(ctx context.Context, decisionID, orderID uuid.UUID) error {
	if r == nil || r.repo == nil {
		return nil
	}
	if err := r.repo.AttachLiveOrder(ctx, decisionID, orderID); err != nil {
		return err
	}
	return r.RecordReplayEvent(ctx, decisionID, domain.ReplayEventTypeLiveOrdered, "order_manager", map[string]any{"order_id": orderID}, time.Now().UTC())
}

func (r *tradeDecisionJournalRecorder) RecordReplayEvent(ctx context.Context, decisionID uuid.UUID, eventType domain.ReplayEventType, source string, payload any, occurredAt time.Time) error {
	if r == nil || r.replayRepo == nil {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("decision recorder: marshal %s replay payload: %w", eventType, err)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return r.replayRepo.CreateReplayEvent(ctx, &domain.ReplayEvent{
		TradeDecisionID: decisionID, EventType: eventType, Source: source,
		Payload: raw, OccurredAt: occurredAt,
	})
}
