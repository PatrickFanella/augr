package api

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type stubReplayEventRepo struct {
	listResult []domain.ReplayEvent
	listErr    error
}

func (s *stubReplayEventRepo) CreateReplayEvent(context.Context, *domain.ReplayEvent) error {
	return nil
}

func (s *stubReplayEventRepo) ListReplayEvents(context.Context, uuid.UUID) ([]domain.ReplayEvent, error) {
	return s.listResult, s.listErr
}

type stubReplayTradeDecisionRepo struct {
	getResult *domain.TradeDecision
	getErr    error
	getID     uuid.UUID
	getCalled bool
}

func (s *stubReplayTradeDecisionRepo) Create(context.Context, *domain.TradeDecision) error {
	return nil
}

func (s *stubReplayTradeDecisionRepo) Get(_ context.Context, id uuid.UUID) (*domain.TradeDecision, error) {
	s.getID = id
	s.getCalled = true
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getResult, nil
}

func (s *stubReplayTradeDecisionRepo) List(context.Context, repository.TradeDecisionFilter, int, int) ([]domain.TradeDecision, error) {
	return nil, nil
}

func (s *stubReplayTradeDecisionRepo) Count(context.Context, repository.TradeDecisionFilter) (int, error) {
	return 0, nil
}

func (s *stubReplayTradeDecisionRepo) AttachPaperOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubReplayTradeDecisionRepo) AttachLiveOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func TestReplayRouteReturnsNotImplementedWithoutDeps(t *testing.T) {
	srv := newTestServer(t)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/replay/decisions/11111111-1111-1111-1111-111111111111", nil)
	if rr.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusNotImplemented, rr.Body.String())
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeNotImplemented {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeNotImplemented)
	}
}

func TestReplayRouteReturnsWorkbench(t *testing.T) {
	decisionID := uuid.New()
	repo := &stubReplayTradeDecisionRepo{getResult: &domain.TradeDecision{
		ID:           decisionID,
		ApprovedSize: 12,
		NetEV:        2.5,
		Status:       domain.TradeDecisionStatusPaper,
		RiskReasons:  []string{"low confidence"},
	}}
	now := time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC)
	eventsRepo := &stubReplayEventRepo{listResult: []domain.ReplayEvent{{
		ID:              uuid.New(),
		TradeDecisionID: decisionID,
		EventType:       domain.ReplayEventTypePaperOrdered,
		OccurredAt:      now,
		CreatedAt:       now,
	}}}
	deps := testDeps()
	deps.TradeDecisions = repo
	deps.ReplayEvents = eventsRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/replay/decisions/"+decisionID.String(), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeJSON[domain.ReplayWorkbenchResponse](t, rr)
	if body.Source.ID != decisionID {
		t.Fatalf("unexpected source: %+v", body.Source)
	}
	if body.Summary.EventCount != 1 || !body.Summary.HasPaperOrder {
		t.Fatalf("unexpected summary: %+v", body.Summary)
	}
	if !repo.getCalled || repo.getID != decisionID {
		t.Fatalf("unexpected trade decision lookup: called=%v id=%s", repo.getCalled, repo.getID)
	}
	if len(body.Events) != 1 || body.Events[0].EventType != domain.ReplayEventTypePaperOrdered {
		t.Fatalf("unexpected events: %+v", body.Events)
	}
}

func TestReplayRouteMapsErrors(t *testing.T) {
	decisionID := uuid.New()
	deps := testDeps()
	deps.TradeDecisions = &stubReplayTradeDecisionRepo{getErr: repository.ErrNotFound}
	deps.ReplayEvents = &stubReplayEventRepo{listErr: errors.New("boom")}
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/replay/decisions/invalid-uuid", nil)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid UUID status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	rr = doRequest(t, srv, http.MethodGet, "/api/v1/replay/decisions/"+decisionID.String(), nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing decision status = %d, want %d", rr.Code, http.StatusNotFound)
	}

	deps.TradeDecisions = &stubReplayTradeDecisionRepo{getResult: &domain.TradeDecision{ID: decisionID}}
	deps.ReplayEvents = &stubReplayEventRepo{listErr: errors.New("boom")}
	srv = newTestServerWithDeps(t, deps)
	rr = doRequest(t, srv, http.MethodGet, "/api/v1/replay/decisions/"+decisionID.String(), nil)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("event repo error status = %d, want %d", rr.Code, http.StatusInternalServerError)
	}
}
