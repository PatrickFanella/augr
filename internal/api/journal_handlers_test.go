package api

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type tradeDecisionListResponse struct {
	Data   []domain.TradeDecision `json:"data"`
	Limit  int                    `json:"limit"`
	Offset int                    `json:"offset"`
}

type stubTradeDecisionJournalRepo struct {
	listResult []domain.TradeDecision
	getResult  *domain.TradeDecision
	getErr     error
	listErr    error

	lastFilter repository.TradeDecisionFilter
	lastLimit  int
	lastOffset int
	lastGetID  uuid.UUID
}

func (s *stubTradeDecisionJournalRepo) Create(context.Context, *domain.TradeDecision) error {
	return nil
}

func (s *stubTradeDecisionJournalRepo) Get(_ context.Context, id uuid.UUID) (*domain.TradeDecision, error) {
	s.lastGetID = id
	if s.getErr != nil {
		return nil, s.getErr
	}
	return s.getResult, nil
}

func (s *stubTradeDecisionJournalRepo) List(_ context.Context, filter repository.TradeDecisionFilter, limit, offset int) ([]domain.TradeDecision, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	return s.listResult, s.listErr
}

func (s *stubTradeDecisionJournalRepo) Count(context.Context, repository.TradeDecisionFilter) (int, error) {
	return len(s.listResult), nil
}

func (s *stubTradeDecisionJournalRepo) AttachPaperOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *stubTradeDecisionJournalRepo) AttachLiveOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func TestJournalRoutesReturnNotImplementedWithoutRepo(t *testing.T) {
	srv := newTestServer(t)

	for _, path := range []string{
		"/api/v1/journal/decisions",
		"/api/v1/journal/decisions/11111111-1111-1111-1111-111111111111",
	} {
		rr := doRequest(t, srv, http.MethodGet, path, nil)
		if rr.Code != http.StatusNotImplemented {
			t.Fatalf("GET %s status = %d, want %d; body: %s", path, rr.Code, http.StatusNotImplemented, rr.Body.String())
		}
		body := decodeJSON[ErrorResponse](t, rr)
		if body.Code != ErrCodeNotImplemented {
			t.Fatalf("GET %s code = %q, want %q", path, body.Code, ErrCodeNotImplemented)
		}
	}
}

func TestJournalRoutesListAndGet(t *testing.T) {
	strategyID := uuid.New()
	after := time.Date(2026, 6, 8, 12, 0, 0, 0, time.UTC)
	before := after.Add(24 * time.Hour)
	decisionID := uuid.New()
	repo := &stubTradeDecisionJournalRepo{
		listResult: []domain.TradeDecision{{ID: decisionID, Status: domain.TradeDecisionStatusPaper}},
		getResult:  &domain.TradeDecision{ID: decisionID, Status: domain.TradeDecisionStatusPaper},
	}
	deps := testDeps()
	deps.TradeDecisions = repo
	srv := newTestServerWithDeps(t, deps)

	params := url.Values{}
	params.Set("strategy_id", strategyID.String())
	params.Set("market_type", string(domain.MarketTypeStock))
	params.Set("status", string(domain.TradeDecisionStatusPaper))
	params.Set("created_after", after.Format(time.RFC3339))
	params.Set("created_before", before.Format(time.RFC3339))
	params.Set("limit", "7")
	params.Set("offset", "3")

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/journal/decisions?"+params.Encode(), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	body := decodeJSON[tradeDecisionListResponse](t, rr)
	if len(body.Data) != 1 || body.Data[0].ID != decisionID {
		t.Fatalf("unexpected list response: %+v", body)
	}
	if body.Limit != 7 || body.Offset != 3 {
		t.Fatalf("unexpected pagination response: %+v", body)
	}
	if repo.lastFilter.StrategyID == nil || *repo.lastFilter.StrategyID != strategyID {
		t.Fatalf("unexpected strategy filter: %+v", repo.lastFilter)
	}
	if repo.lastFilter.MarketType != domain.MarketTypeStock || repo.lastFilter.Status != domain.TradeDecisionStatusPaper {
		t.Fatalf("unexpected enum filters: %+v", repo.lastFilter)
	}
	if repo.lastFilter.CreatedAfter == nil || !repo.lastFilter.CreatedAfter.Equal(after) || repo.lastFilter.CreatedBefore == nil || !repo.lastFilter.CreatedBefore.Equal(before) {
		t.Fatalf("unexpected time filters: %+v", repo.lastFilter)
	}

	rr = doRequest(t, srv, http.MethodGet, "/api/v1/journal/decisions/"+decisionID.String(), nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	got := decodeJSON[domain.TradeDecision](t, rr)
	if got.ID != decisionID {
		t.Fatalf("unexpected get response: %+v", got)
	}
	if repo.lastGetID != decisionID {
		t.Fatalf("unexpected get id: %s", repo.lastGetID)
	}
}

func TestJournalRouteGetMapsNotFound(t *testing.T) {
	repo := &stubTradeDecisionJournalRepo{getErr: repository.ErrNotFound}
	deps := testDeps()
	deps.TradeDecisions = repo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/journal/decisions/11111111-1111-1111-1111-111111111111", nil)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusNotFound, rr.Body.String())
	}
	body := decodeJSON[ErrorResponse](t, rr)
	if body.Code != ErrCodeNotFound {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeNotFound)
	}
}
