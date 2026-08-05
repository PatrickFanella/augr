package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/polymarketdiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestHandleGetEventMarketsSummaryReturnsConfiguredProviders(t *testing.T) {
	prev := polymarketdiscovery.LastResult()
	t.Cleanup(func() {
		polymarketdiscovery.StoreLastResult(prev)
	})
	polymarketdiscovery.StoreLastResult(&polymarketdiscovery.Result{
		StartedAt: time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC),
		Duration:  time.Minute,
		DryRun:    false,
	})

	deps := testDeps()
	deps.Strategies = &eventMarketSummaryStrategyRepoStub{counts: map[domain.MarketType]int{
		domain.MarketTypeKalshi:     2,
		domain.MarketTypePolymarket: 3,
	}}
	deps.KalshiWatchedRepo = &eventMarketSummaryKalshiWatchedRepoStub{markets: []domain.KalshiWatchedMarket{
		{Ticker: "KX-ONE", EventTicker: "EVT-ONE", Title: "One?", Enabled: true},
	}}
	deps.KalshiDiscoveryRuns = &eventMarketSummaryKalshiDiscoveryRunsRepoStub{runs: []domain.KalshiDiscoveryRun{
		{ID: uuid.New(), Status: domain.KalshiDiscoveryStatusCompleted, StartedAt: time.Date(2026, 6, 18, 11, 0, 0, 0, time.UTC)},
	}}
	deps.KalshiSnapshotsRepo = &kalshiSummarySnapshotsRepoStub{snapshots: []domain.KalshiMarketSnapshot{{
		Provider: "kalshi", Environment: "demo", Ticker: "KX-ONE", CapturedAt: time.Now().UTC(),
	}}}
	deps.PolymarketWatchedRepo = &eventMarketSummaryPolymarketWatchedRepoStub{markets: []domain.PolymarketWatchedMarket{
		{Slug: "will-example-happen", Enabled: true},
	}}

	srv := newTestServerWithDeps(t, deps)
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/event-markets/summary", nil)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusOK, rr.Body.String())
	}

	body := decodeJSON[EventMarketsSummaryResponse](t, rr)
	if len(body.Providers) != 2 {
		t.Fatalf("providers = %#v, want 2 entries", body.Providers)
	}
	providers := map[string]EventMarketProviderSummary{}
	for _, provider := range body.Providers {
		providers[provider.Provider] = provider
	}

	kalshi, ok := providers["kalshi"]
	if !ok {
		t.Fatal("missing kalshi provider summary")
	}
	if kalshi.WatchedMarkets != 1 || kalshi.ActivePaper != 2 || kalshi.LastRunStatus != domain.KalshiDiscoveryStatusCompleted {
		t.Fatalf("kalshi summary = %#v", kalshi)
	}
	if kalshi.LiveTradingReady {
		t.Fatalf("kalshi live_trading_ready = true, want false")
	}
	if kalshi.DataEnvironment != "demo" || kalshi.DataStatus != "current" {
		t.Fatalf("kalshi data provenance = %#v", kalshi)
	}

	polymarket, ok := providers["polymarket"]
	if !ok {
		t.Fatal("missing polymarket provider summary")
	}
	if polymarket.WatchedMarkets != 1 || polymarket.ActivePaper != 3 || polymarket.LastRunStatus != "completed" {
		t.Fatalf("polymarket summary = %#v", polymarket)
	}
	if polymarket.LiveTradingReady {
		t.Fatalf("polymarket live_trading_ready = true, want false")
	}
}

func TestHandleGetEventMarketsSummaryMissingDeps(t *testing.T) {
	srv := newTestServerWithDeps(t, testDeps())
	rr := doRequest(t, srv, http.MethodGet, "/api/v1/event-markets/summary", nil)

	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d; body=%s", rr.Code, http.StatusServiceUnavailable, rr.Body.String())
	}

	var body ErrorResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if body.Code != ErrCodeNotImplemented {
		t.Fatalf("code = %q, want %q", body.Code, ErrCodeNotImplemented)
	}
}

type eventMarketSummaryStrategyRepoStub struct {
	counts map[domain.MarketType]int
}

func (s *eventMarketSummaryStrategyRepoStub) Create(context.Context, *domain.Strategy) error {
	return nil
}

func (s *eventMarketSummaryStrategyRepoStub) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}

func (s *eventMarketSummaryStrategyRepoStub) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	return nil, nil
}

func (s *eventMarketSummaryStrategyRepoStub) Count(_ context.Context, filter repository.StrategyFilter) (int, error) {
	if filter.Status != domain.StrategyStatusActive || filter.IsPaper == nil || !*filter.IsPaper {
		return 0, nil
	}
	return s.counts[filter.MarketType], nil
}

func (s *eventMarketSummaryStrategyRepoStub) Update(context.Context, *domain.Strategy) error {
	return nil
}
func (s *eventMarketSummaryStrategyRepoStub) Delete(context.Context, uuid.UUID) error { return nil }
func (s *eventMarketSummaryStrategyRepoStub) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}

func (s *eventMarketSummaryStrategyRepoStub) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type eventMarketSummaryKalshiWatchedRepoStub struct {
	markets []domain.KalshiWatchedMarket
}

func (s *eventMarketSummaryKalshiWatchedRepoStub) Upsert(context.Context, *domain.KalshiWatchedMarket) error {
	return nil
}

func (s *eventMarketSummaryKalshiWatchedRepoStub) SetEnabled(context.Context, string, bool) error {
	return nil
}

func (s *eventMarketSummaryKalshiWatchedRepoStub) ListEnabled(context.Context) ([]domain.KalshiWatchedMarket, error) {
	return s.markets, nil
}

type eventMarketSummaryKalshiDiscoveryRunsRepoStub struct {
	runs []domain.KalshiDiscoveryRun
}

func (s *eventMarketSummaryKalshiDiscoveryRunsRepoStub) Create(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}

func (s *eventMarketSummaryKalshiDiscoveryRunsRepoStub) GetActive(context.Context) (*domain.KalshiDiscoveryRun, error) {
	return nil, repository.ErrNotFound
}

func (s *eventMarketSummaryKalshiDiscoveryRunsRepoStub) Finish(context.Context, *domain.KalshiDiscoveryRun) error {
	return nil
}

func (s *eventMarketSummaryKalshiDiscoveryRunsRepoStub) ListLatest(context.Context, int) ([]domain.KalshiDiscoveryRun, error) {
	return s.runs, nil
}

type eventMarketSummaryPolymarketWatchedRepoStub struct {
	markets []domain.PolymarketWatchedMarket
}

func (s *eventMarketSummaryPolymarketWatchedRepoStub) List(context.Context, bool) ([]domain.PolymarketWatchedMarket, error) {
	return s.markets, nil
}

func (s *eventMarketSummaryPolymarketWatchedRepoStub) Add(context.Context, *domain.PolymarketWatchedMarket) error {
	return nil
}

func (s *eventMarketSummaryPolymarketWatchedRepoStub) Remove(context.Context, string) error {
	return nil
}

func (s *eventMarketSummaryPolymarketWatchedRepoStub) SetEnabled(context.Context, string, bool) error {
	return nil
}
