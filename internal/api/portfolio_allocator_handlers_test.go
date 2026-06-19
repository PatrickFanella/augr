package api

import (
	"context"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type portfolioDiagnosticsRunRepo struct {
	runs       []domain.PipelineRun
	lastFilter repository.PipelineRunFilter
	lastLimit  int
	lastOffset int
}

func (s *portfolioDiagnosticsRunRepo) Create(context.Context, *domain.PipelineRun) error { return nil }
func (s *portfolioDiagnosticsRunRepo) GetByID(context.Context, uuid.UUID) (*domain.PipelineRun, error) {
	return nil, repository.ErrNotFound
}
func (s *portfolioDiagnosticsRunRepo) Get(context.Context, uuid.UUID, time.Time) (*domain.PipelineRun, error) {
	return nil, repository.ErrNotFound
}
func (s *portfolioDiagnosticsRunRepo) List(_ context.Context, filter repository.PipelineRunFilter, limit, offset int) ([]domain.PipelineRun, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	return s.runs, nil
}
func (s *portfolioDiagnosticsRunRepo) Count(context.Context, repository.PipelineRunFilter) (int, error) {
	return len(s.runs), nil
}
func (s *portfolioDiagnosticsRunRepo) UpdateStatus(context.Context, uuid.UUID, time.Time, repository.PipelineRunStatusUpdate) error {
	return nil
}

type portfolioDiagnosticsTradeDecisionRepo struct {
	decisions  []domain.TradeDecision
	lastFilter repository.TradeDecisionFilter
	lastLimit  int
	lastOffset int
}

func (s *portfolioDiagnosticsTradeDecisionRepo) Create(context.Context, *domain.TradeDecision) error {
	return nil
}
func (s *portfolioDiagnosticsTradeDecisionRepo) Get(context.Context, uuid.UUID) (*domain.TradeDecision, error) {
	return nil, repository.ErrNotFound
}
func (s *portfolioDiagnosticsTradeDecisionRepo) List(_ context.Context, filter repository.TradeDecisionFilter, limit, offset int) ([]domain.TradeDecision, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	return s.decisions, nil
}
func (s *portfolioDiagnosticsTradeDecisionRepo) Count(context.Context, repository.TradeDecisionFilter) (int, error) {
	return len(s.decisions), nil
}
func (s *portfolioDiagnosticsTradeDecisionRepo) AttachPaperOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}
func (s *portfolioDiagnosticsTradeDecisionRepo) AttachLiveOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type portfolioDiagnosticsStrategyRepo struct {
	strategies []domain.Strategy
	calls      []repository.StrategyFilter
}

func (s *portfolioDiagnosticsStrategyRepo) Create(context.Context, *domain.Strategy) error {
	return nil
}
func (s *portfolioDiagnosticsStrategyRepo) Get(context.Context, uuid.UUID) (*domain.Strategy, error) {
	return nil, repository.ErrNotFound
}
func (s *portfolioDiagnosticsStrategyRepo) List(_ context.Context, filter repository.StrategyFilter, _, _ int) ([]domain.Strategy, error) {
	s.calls = append(s.calls, filter)
	if filter.Status == "" {
		return s.strategies, nil
	}
	out := make([]domain.Strategy, 0, len(s.strategies))
	for _, strategy := range s.strategies {
		if strategy.Status == filter.Status {
			out = append(out, strategy)
		}
	}
	return out, nil
}
func (s *portfolioDiagnosticsStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return len(s.strategies), nil
}
func (s *portfolioDiagnosticsStrategyRepo) Update(context.Context, *domain.Strategy) error {
	return nil
}
func (s *portfolioDiagnosticsStrategyRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (s *portfolioDiagnosticsStrategyRepo) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}
func (s *portfolioDiagnosticsStrategyRepo) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type portfolioDiagnosticsPositionRepo struct {
	positions  []domain.Position
	lastFilter repository.PositionFilter
	lastLimit  int
	lastOffset int
}

func (s *portfolioDiagnosticsPositionRepo) Create(context.Context, *domain.Position) error {
	return nil
}
func (s *portfolioDiagnosticsPositionRepo) Get(context.Context, uuid.UUID) (*domain.Position, error) {
	return nil, repository.ErrNotFound
}
func (s *portfolioDiagnosticsPositionRepo) List(context.Context, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}
func (s *portfolioDiagnosticsPositionRepo) Update(context.Context, *domain.Position) error {
	return nil
}
func (s *portfolioDiagnosticsPositionRepo) Delete(context.Context, uuid.UUID) error { return nil }
func (s *portfolioDiagnosticsPositionRepo) GetOpen(_ context.Context, filter repository.PositionFilter, limit, offset int) ([]domain.Position, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	return s.positions, nil
}
func (s *portfolioDiagnosticsPositionRepo) Count(context.Context, repository.PositionFilter) (int, error) {
	return len(s.positions), nil
}
func (s *portfolioDiagnosticsPositionRepo) CountOpen(context.Context, repository.PositionFilter) (int, error) {
	return len(s.positions), nil
}
func (s *portfolioDiagnosticsPositionRepo) GetByStrategy(context.Context, uuid.UUID, repository.PositionFilter, int, int) ([]domain.Position, error) {
	return nil, nil
}

func TestPortfolioAllocatorDiagnosticsReturnsSummary(t *testing.T) {
	t.Parallel()

	stockStrategyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	cryptoStrategyID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	unknownStrategyID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	runRepo := &portfolioDiagnosticsRunRepo{runs: []domain.PipelineRun{
		{Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalHold},
		{Status: domain.PipelineStatusFailed, Signal: domain.PipelineSignalBuy},
	}}
	decisionRepo := &portfolioDiagnosticsTradeDecisionRepo{decisions: []domain.TradeDecision{
		{Status: domain.TradeDecisionStatusRejected, Side: domain.OrderSideBuy, RiskReasons: []string{"risk_rejected"}},
		{Status: domain.TradeDecisionStatusCandidate, Side: domain.OrderSideSell},
	}}
	strategyRepo := &portfolioDiagnosticsStrategyRepo{strategies: []domain.Strategy{
		{ID: stockStrategyID, MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive},
		{ID: cryptoStrategyID, MarketType: domain.MarketTypeCrypto, Status: domain.StrategyStatusActive},
		{ID: unknownStrategyID, MarketType: domain.MarketTypePolymarket, Status: domain.StrategyStatusInactive},
	}}
	positionRepo := &portfolioDiagnosticsPositionRepo{positions: []domain.Position{
		{StrategyID: &stockStrategyID, Quantity: 5, CurrentPrice: floatPtr(12)},
		{StrategyID: &cryptoStrategyID, Quantity: 10, AvgEntry: 2},
		{Quantity: 3, CurrentPrice: floatPtr(7)},
	}}

	deps := testDeps()
	deps.Runs = runRepo
	deps.TradeDecisions = decisionRepo
	deps.Strategies = strategyRepo
	deps.Positions = positionRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/portfolio/allocator/diagnostics", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	got := decodeJSON[portfolio.DiagnosticsSummary](t, rr)
	wantActive := map[string]int{"stock": 1, "crypto": 1}
	if !reflect.DeepEqual(got.ActiveStrategiesByMarket, wantActive) {
		t.Fatalf("active strategies = %#v, want %#v", got.ActiveStrategiesByMarket, wantActive)
	}
	wantOpen := map[string]int{"stock": 1, "crypto": 1, "unknown": 1}
	if !reflect.DeepEqual(got.OpenPositionsByMarket, wantOpen) {
		t.Fatalf("open positions = %#v, want %#v", got.OpenPositionsByMarket, wantOpen)
	}
	wantRunSignals := map[string]int{"hold": 1, "buy": 1}
	if !reflect.DeepEqual(got.RunCountsBySignal, wantRunSignals) {
		t.Fatalf("run counts by signal = %#v, want %#v", got.RunCountsBySignal, wantRunSignals)
	}
	wantDecisionStatus := map[string]int{"rejected": 1, "candidate": 1}
	if !reflect.DeepEqual(got.DecisionCountsByStatus, wantDecisionStatus) {
		t.Fatalf("decision counts by status = %#v, want %#v", got.DecisionCountsByStatus, wantDecisionStatus)
	}
	if got.TargetGrossExposurePct != 0.35 {
		t.Fatalf("target gross exposure pct = %v, want 0.35", got.TargetGrossExposurePct)
	}
	if got.GrossExposurePct != 1 {
		t.Fatalf("gross exposure pct = %v, want 1", got.GrossExposurePct)
	}
	if got.BuyingPowerUtilizationPct != 1 {
		t.Fatalf("buying power utilization pct = %v, want 1", got.BuyingPowerUtilizationPct)
	}
	if !containsWarning(got.Warnings, portfolioDiagnosticsWarningAccountBal) || !containsWarning(got.Warnings, portfolioDiagnosticsWarningUnknownOpen) {
		t.Fatalf("warnings = %#v, want account balance and unknown open warnings", got.Warnings)
	}
	if len(strategyRepo.calls) != 2 || strategyRepo.calls[0].Status != domain.StrategyStatusActive || strategyRepo.calls[1].Status != "" {
		t.Fatalf("unexpected strategy repo calls: %#v", strategyRepo.calls)
	}
	if runRepo.lastLimit != portfolioDiagnosticsRunsLimit || decisionRepo.lastLimit != portfolioDiagnosticsDecisionsLimit || positionRepo.lastLimit != portfolioDiagnosticsPositionsLimit {
		t.Fatalf("unexpected repo limits: runs=%d decisions=%d positions=%d", runRepo.lastLimit, decisionRepo.lastLimit, positionRepo.lastLimit)
	}
}

func TestPortfolioAllocatorDiagnosticsWarningsWhenReposMissing(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t)
	srv.runs = nil
	srv.tradeDecisions = nil
	srv.strategies = nil
	srv.positions = nil

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/portfolio/allocator/diagnostics", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}

	got := decodeJSON[portfolio.DiagnosticsSummary](t, rr)
	wantWarnings := []string{
		portfolioDiagnosticsWarningRuns,
		portfolioDiagnosticsWarningDecisions,
		portfolioDiagnosticsWarningStrategies,
		portfolioDiagnosticsWarningPositions,
		portfolioDiagnosticsWarningAccountBal,
		"equity_non_positive",
	}
	for _, want := range wantWarnings {
		if !containsWarning(got.Warnings, want) {
			t.Fatalf("warnings = %#v, want %q", got.Warnings, want)
		}
	}
	if len(got.RunCountsBySignal) != 0 || len(got.OpenPositionsByMarket) != 0 {
		t.Fatalf("unexpected counts: %+v", got)
	}
}

func containsWarning(warnings []string, want string) bool {
	for _, warning := range warnings {
		if warning == want {
			return true
		}
	}
	return false
}

func floatPtr(v float64) *float64 { return &v }
