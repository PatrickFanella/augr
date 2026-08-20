package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestHandleGetKalshiSummaryReturnsRealData(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)
	server := &Server{
		strategies: &kalshiSummaryStrategyRepoStub{count: 2},
		kalshiWatchedRepo: &kalshiSummaryWatchedRepoStub{markets: []domain.KalshiWatchedMarket{
			{Ticker: "KX-ONE", EventTicker: "EVT-ONE", Title: "One?", Enabled: true},
		}},
		kalshiSnapshotsRepo: &kalshiSummarySnapshotsRepoStub{snapshots: []domain.KalshiMarketSnapshot{
			{ID: uuid.New(), Ticker: "KX-ONE", Title: "One?", YesBid: 0.41, YesAsk: 0.43, CapturedAt: now},
		}},
		kalshiDiscoveryRuns: &kalshiSummaryDiscoveryRunsRepoStub{runs: []domain.KalshiDiscoveryRun{
			{ID: uuid.New(), Status: domain.KalshiDiscoveryStatusCompleted, Result: domain.KalshiDiscoveryResult{Fetched: 3, Screened: 2, Proposed: 1, Deployed: 1}, StartedAt: now},
		}},
	}

	rr := httptest.NewRecorder()
	server.handleGetKalshiSummary(rr, httptest.NewRequest(http.MethodGet, "/api/v1/kalshi/summary", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}
	var body KalshiSummaryResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(body.WatchedMarkets) != 1 || body.WatchedMarkets[0].Ticker != "KX-ONE" {
		t.Fatalf("watched_markets = %#v", body.WatchedMarkets)
	}
	if len(body.LatestSnapshots) != 1 || body.LatestSnapshots[0].Ticker != "KX-ONE" {
		t.Fatalf("latest_snapshots = %#v", body.LatestSnapshots)
	}
	if body.Discovery.Status != domain.KalshiDiscoveryStatusCompleted || body.Discovery.LastRun == nil {
		t.Fatalf("discovery = %#v", body.Discovery)
	}
	if body.Strategies.ActivePaper != 2 {
		t.Fatalf("active_paper = %d, want 2", body.Strategies.ActivePaper)
	}
}

func TestHandleGetKalshiSummaryMissingDeps(t *testing.T) {
	t.Parallel()

	rr := httptest.NewRecorder()
	(&Server{}).handleGetKalshiSummary(rr, httptest.NewRequest(http.MethodGet, "/api/v1/kalshi/summary", nil))

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusServiceUnavailable)
	}
	var body ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Code != ErrCodeNotImplemented {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeNotImplemented)
	}
}

type kalshiSummaryStrategyRepoStub struct{ count int }

func (s *kalshiSummaryStrategyRepoStub) Create(context.Context, *domain.Strategy) error { return nil }

func (s *kalshiSummaryStrategyRepoStub) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}

func (s *kalshiSummaryStrategyRepoStub) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return nil, nil
}

func (s *kalshiSummaryStrategyRepoStub) Count(_ context.Context, filter repository.StrategyFilter) (int, error) {
	if filter.MarketType != domain.MarketTypeKalshi || filter.Status != domain.StrategyStatusActive || filter.IsPaper == nil || !*filter.IsPaper {
		return 0, nil
	}
	return s.count, nil
}

func (s *kalshiSummaryStrategyRepoStub) Update(context.Context, *domain.Strategy) error { return nil }

func (s *kalshiSummaryStrategyRepoStub) Delete(context.Context, uuid.UUID) error { return nil }

func (s *kalshiSummaryStrategyRepoStub) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}

func (s *kalshiSummaryStrategyRepoStub) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type kalshiSummaryWatchedRepoStub struct{ markets []domain.KalshiWatchedMarket }

func (s *kalshiSummaryWatchedRepoStub) Upsert(context.Context, *domain.KalshiWatchedMarket) error {
	return nil
}

func (s *kalshiSummaryWatchedRepoStub) SetEnabled(context.Context, string, bool) error { return nil }

func (s *kalshiSummaryWatchedRepoStub) ListEnabled(context.Context) ([]domain.KalshiWatchedMarket, error) {
	return s.markets, nil
}

type kalshiSummarySnapshotsRepoStub struct{ snapshots []domain.KalshiMarketSnapshot }

func (s *kalshiSummarySnapshotsRepoStub) Create(context.Context, *domain.KalshiMarketSnapshot) error {
	return nil
}

func (s *kalshiSummarySnapshotsRepoStub) ListLatestByTicker(context.Context, string, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

func (s *kalshiSummarySnapshotsRepoStub) ListRecent(context.Context, int) ([]domain.KalshiMarketSnapshot, error) {
	return s.snapshots, nil
}

type kalshiSummaryDiscoveryRunsRepoStub struct{ runs []domain.KalshiDiscoveryRun }

func (s *kalshiSummaryDiscoveryRunsRepoStub) Create(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}

func (s *kalshiSummaryDiscoveryRunsRepoStub) GetActive(context.Context) (*domain.KalshiDiscoveryRun, error) {
	return nil, repository.ErrNotFound
}

func (s *kalshiSummaryDiscoveryRunsRepoStub) Finish(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}

func (s *kalshiSummaryDiscoveryRunsRepoStub) ListLatest(context.Context, int) ([]domain.KalshiDiscoveryRun, error) {
	return s.runs, nil
}
