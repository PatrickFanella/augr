package execution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type decisionJournalStub struct{ created *domain.TradeDecision }

func (s *decisionJournalStub) Create(_ context.Context, decision *domain.TradeDecision) error {
	s.created = decision
	return nil
}

func (*decisionJournalStub) Get(context.Context, uuid.UUID) (*domain.TradeDecision, error) {
	return nil, nil
}

func (*decisionJournalStub) List(context.Context, repository.TradeDecisionFilter, int, int) ([]domain.TradeDecision, error) {
	return nil, nil
}

func (*decisionJournalStub) Count(context.Context, repository.TradeDecisionFilter) (int, error) {
	return 0, nil
}

func (*decisionJournalStub) AttachPaperOrder(context.Context, uuid.UUID, uuid.UUID) error { return nil }

func (*decisionJournalStub) AttachLiveOrder(context.Context, uuid.UUID, uuid.UUID) error { return nil }

type replayEventStub struct{ events []domain.ReplayEvent }

func (s *replayEventStub) CreateReplayEvent(_ context.Context, event *domain.ReplayEvent) error {
	s.events = append(s.events, *event)
	return nil
}

func (*replayEventStub) ListReplayEvents(context.Context, uuid.UUID) ([]domain.ReplayEvent, error) {
	return nil, nil
}

func TestTradeDecisionJournalRecorderWritesReplayLifecycle(t *testing.T) {
	journal := &decisionJournalStub{}
	replay := &replayEventStub{}
	recorder := NewTradeDecisionJournalRecorder(journal, replay)
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	decision := &domain.TradeDecision{
		ID: uuid.New(), MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST",
		RiskStatus: domain.RiskDecisionApproved, Status: domain.TradeDecisionStatusCandidate,
		CreatedAt: now, UpdatedAt: now,
	}

	if err := recorder.RecordDecision(context.Background(), decision); err != nil {
		t.Fatalf("RecordDecision() error = %v", err)
	}
	orderID := uuid.New()
	if err := recorder.AttachPaperOrder(context.Background(), decision.ID, orderID); err != nil {
		t.Fatalf("AttachPaperOrder() error = %v", err)
	}

	if journal.created != decision {
		t.Fatal("decision was not persisted")
	}
	want := []domain.ReplayEventType{domain.ReplayEventTypeDecisionCreated, domain.ReplayEventTypeRiskReviewed, domain.ReplayEventTypePaperOrdered}
	if len(replay.events) != len(want) {
		t.Fatalf("events = %d, want %d", len(replay.events), len(want))
	}
	for i := range want {
		if replay.events[i].EventType != want[i] {
			t.Fatalf("events[%d] = %q, want %q", i, replay.events[i].EventType, want[i])
		}
		if replay.events[i].TradeDecisionID != decision.ID {
			t.Fatalf("events[%d] decision id mismatch", i)
		}
	}
}
