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
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type portfolioDiagnosticsBalanceSource struct {
	balance execution.Balance
	err     error
}

func (s portfolioDiagnosticsBalanceSource) GetAccountBalance(context.Context) (execution.Balance, error) {
	return s.balance, s.err
}

type portfolioDiagnosticsRunRepo struct {
	runs       []domain.PipelineRun
	total      int
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
	return s.total, nil
}

func (s *portfolioDiagnosticsRunRepo) CountBySignal(context.Context, repository.PipelineRunFilter) (map[domain.PipelineSignal]int, error) {
	counts := make(map[domain.PipelineSignal]int)
	for _, run := range s.runs {
		counts[run.Signal]++
	}
	return counts, nil
}

func (s *portfolioDiagnosticsRunRepo) CountByStatus(context.Context, repository.PipelineRunFilter) (map[domain.PipelineStatus]int, error) {
	counts := make(map[domain.PipelineStatus]int)
	for _, run := range s.runs {
		counts[run.Status]++
	}
	return counts, nil
}

func (s *portfolioDiagnosticsRunRepo) UpdateStatus(context.Context, uuid.UUID, time.Time, repository.PipelineRunStatusUpdate) error {
	return nil
}

type portfolioDiagnosticsTradeDecisionRepo struct {
	decisions  []domain.TradeDecision
	total      int
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
	return s.total, nil
}

func (s *portfolioDiagnosticsTradeDecisionRepo) CountByStatus(context.Context, repository.TradeDecisionFilter) (map[domain.TradeDecisionStatus]int, error) {
	counts := make(map[domain.TradeDecisionStatus]int)
	for _, d := range s.decisions {
		counts[d.Status]++
	}
	return counts, nil
}

func (s *portfolioDiagnosticsTradeDecisionRepo) CountByNoActionReason(context.Context, repository.TradeDecisionFilter) (map[string]int, error) {
	return map[string]int{string(portfolio.NoActionReasonRiskRejected): 1}, nil
}

func (s *portfolioDiagnosticsTradeDecisionRepo) AttachPaperOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

func (s *portfolioDiagnosticsTradeDecisionRepo) AttachLiveOrder(context.Context, uuid.UUID, uuid.UUID) error {
	return nil
}

type portfolioDiagnosticsStrategyRepo struct {
	strategies []domain.Strategy
	total      int
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
	return s.total, nil
}

func (s *portfolioDiagnosticsStrategyRepo) CountByMarket(context.Context, repository.StrategyFilter) (map[domain.MarketType]int, error) {
	counts := make(map[domain.MarketType]int)
	for _, strategy := range s.strategies {
		if strategy.Status == domain.StrategyStatusActive {
			counts[strategy.MarketType]++
		}
	}
	return counts, nil
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
	totalOpen  int
	markets    map[uuid.UUID]domain.MarketType
	lastFilter repository.PositionFilter
	lastLimit  int
	lastOffset int
}

func (s *portfolioDiagnosticsPositionRepo) Create(context.Context, *domain.Position) error {
	return nil
}

func (s *portfolioDiagnosticsPositionRepo) CreateAlpacaOwned(context.Context, *domain.Position) error {
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
	return s.totalOpen, nil
}

func (s *portfolioDiagnosticsPositionRepo) ListOpenAlpacaOwned(context.Context, int, int) ([]domain.Position, error) {
	return append([]domain.Position(nil), s.positions...), nil
}

func (s *portfolioDiagnosticsPositionRepo) CountOpenByMarket(context.Context, repository.PositionFilter) (map[domain.MarketType]int, error) {
	counts := make(map[domain.MarketType]int)
	for _, pos := range s.positions {
		market := domain.MarketType("")
		if pos.StrategyID != nil {
			if m, ok := s.markets[*pos.StrategyID]; ok {
				market = m
			}
		}
		counts[market]++
	}
	return counts, nil
}

func (s *portfolioDiagnosticsPositionRepo) GrossExposureOpen(context.Context, repository.PositionFilter) (float64, error) {
	var total float64
	for _, pos := range s.positions {
		price := pos.CurrentPrice
		if price == nil {
			price = &pos.AvgEntry
		}
		total += pos.Quantity * *price
	}
	return total, nil
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
	}, total: 101}
	decisionRepo := &portfolioDiagnosticsTradeDecisionRepo{decisions: []domain.TradeDecision{
		{Status: domain.TradeDecisionStatusRejected, Side: domain.OrderSideBuy, RiskReasons: []string{"risk_rejected"}},
		{Status: domain.TradeDecisionStatusCandidate, Side: domain.OrderSideSell},
	}, total: 202}
	strategyRepo := &portfolioDiagnosticsStrategyRepo{strategies: []domain.Strategy{
		{ID: stockStrategyID, MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive},
		{ID: cryptoStrategyID, MarketType: domain.MarketTypeCrypto, Status: domain.StrategyStatusActive},
		{ID: unknownStrategyID, MarketType: domain.MarketTypePolymarket, Status: domain.StrategyStatusInactive},
	}, total: 303}
	positionRepo := &portfolioDiagnosticsPositionRepo{positions: []domain.Position{
		{StrategyID: &stockStrategyID, Quantity: 5, CurrentPrice: floatPtr(12)},
		{StrategyID: &cryptoStrategyID, Quantity: 10, AvgEntry: 2},
		{Quantity: 3, CurrentPrice: floatPtr(7)},
	}, totalOpen: 404, markets: map[uuid.UUID]domain.MarketType{stockStrategyID: domain.MarketTypeStock, cryptoStrategyID: domain.MarketTypeCrypto}}

	deps := testDeps()
	deps.Runs = runRepo
	deps.TradeDecisions = decisionRepo
	deps.Strategies = strategyRepo
	deps.Positions = positionRepo
	deps.AccountBalance = portfolioDiagnosticsBalanceSource{balance: execution.Balance{BuyingPower: 0, Equity: 101}}
	paperProfile, err := domain.NewPaperEvaluationProfile(domain.PaperEvaluationModeScored, 100_000, 2, 5, 0.0001)
	if err != nil {
		t.Fatal(err)
	}
	deps.PaperEvaluation = &paperProfile
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
	if got.TotalStrategyRuns != 101 || got.TotalTradeDecisions != 202 || got.TotalStrategies != 303 || got.TotalOpenPositions != 404 {
		t.Fatalf("unexpected totals: %+v", got)
	}
	if got.SampleStrategyRuns != 2 || got.SampleTradeDecisions != 2 || got.SampleStrategies != 2 || got.SampleOpenPositions != 3 {
		t.Fatalf("unexpected sample sizes: %+v", got)
	}
	if got.GrossExposurePct != 1 {
		t.Fatalf("gross exposure pct = %v, want 1", got.GrossExposurePct)
	}
	if got.BuyingPowerUtilizationPct != 1 {
		t.Fatalf("buying power utilization pct = %v, want 1", got.BuyingPowerUtilizationPct)
	}
	if got.PaperEvaluation.Mode != string(domain.PaperEvaluationModeScored) || !got.PaperEvaluation.PromotionEligible {
		t.Fatalf("paper evaluation = %+v, want scored promotion evidence", got.PaperEvaluation)
	}
	if got.PaperEvaluation.ResultsIsolated || !containsWarning(got.Warnings, portfolioDiagnosticsWarningPaperScope) {
		t.Fatalf("paper evaluation = %+v warnings = %#v, want explicit unscoped legacy boundary", got.PaperEvaluation, got.Warnings)
	}
	if containsWarning(got.Warnings, portfolioDiagnosticsWarningAccountBal) || !containsWarning(got.Warnings, portfolioDiagnosticsWarningUnknownOpen) {
		t.Fatalf("warnings = %#v, want unknown open warning only", got.Warnings)
	}
	if len(strategyRepo.calls) != 1 || strategyRepo.calls[0].Status != domain.StrategyStatusActive {
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
		portfolioDiagnosticsWarningPaperProfile,
		portfolioDiagnosticsWarningPaperScope,
	}
	for _, want := range wantWarnings {
		if !containsWarning(got.Warnings, want) {
			t.Fatalf("warnings = %#v, want %q", got.Warnings, want)
		}
	}
	if len(got.RunCountsBySignal) != 0 || len(got.OpenPositionsByMarket) != 0 {
		t.Fatalf("unexpected counts: %+v", got)
	}
	if got.PaperEvaluation.Mode != "unlabelled" || got.PaperEvaluation.PromotionEligible {
		t.Fatalf("paper evaluation = %+v, want fail-closed legacy label", got.PaperEvaluation)
	}
}

type portfolioAllocatorOpportunityRepo struct {
	items      []domain.Opportunity
	lastFilter repository.OpportunityFilter
	lastLimit  int
	lastOffset int
}

func (s *portfolioAllocatorOpportunityRepo) Create(context.Context, *domain.Opportunity) error {
	return nil
}

func (s *portfolioAllocatorOpportunityRepo) UpsertQueuedByDedupeKey(context.Context, *domain.Opportunity) error {
	return nil
}

func (s *portfolioAllocatorOpportunityRepo) Get(context.Context, uuid.UUID) (*domain.Opportunity, error) {
	return nil, repository.ErrNotFound
}

func (s *portfolioAllocatorOpportunityRepo) List(_ context.Context, filter repository.OpportunityFilter, limit, offset int) ([]domain.Opportunity, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	return filterOpportunities(s.items, filter), nil
}

func (s *portfolioAllocatorOpportunityRepo) Count(_ context.Context, filter repository.OpportunityFilter) (int, error) {
	return len(filterOpportunities(s.items, filter)), nil
}

func (s *portfolioAllocatorOpportunityRepo) ExpireQueuedBefore(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func (s *portfolioAllocatorOpportunityRepo) ListQueuedForAllocation(context.Context, time.Time) ([]domain.Opportunity, error) {
	return append([]domain.Opportunity(nil), s.items...), nil
}

func (s *portfolioAllocatorOpportunityRepo) UpdateStatus(context.Context, uuid.UUID, domain.OpportunityStatus, string) error {
	return nil
}

type portfolioAllocatorDecisionRepo struct {
	items      []domain.AllocationDecision
	lastFilter repository.AllocationDecisionFilter
	lastLimit  int
	lastOffset int
}

func (s *portfolioAllocatorDecisionRepo) Create(context.Context, *domain.AllocationDecision) error {
	return nil
}

func (s *portfolioAllocatorDecisionRepo) List(_ context.Context, filter repository.AllocationDecisionFilter, limit, offset int) ([]domain.AllocationDecision, error) {
	s.lastFilter = filter
	s.lastLimit = limit
	s.lastOffset = offset
	return filterAllocationDecisions(s.items, filter), nil
}

func (s *portfolioAllocatorDecisionRepo) Count(_ context.Context, filter repository.AllocationDecisionFilter) (int, error) {
	return len(filterAllocationDecisions(s.items, filter)), nil
}

type portfolioAllocatorOpportunityListResponse struct {
	Data   []domain.Opportunity `json:"data"`
	Total  int                  `json:"total"`
	Limit  int                  `json:"limit"`
	Offset int                  `json:"offset"`
}

type portfolioAllocatorDecisionListResponse struct {
	Data   []domain.AllocationDecision `json:"data"`
	Total  int                         `json:"total"`
	Limit  int                         `json:"limit"`
	Offset int                         `json:"offset"`
}

func TestPortfolioAllocatorListAndSummaryRoutes(t *testing.T) {
	t.Parallel()

	queuedStrategyID := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	selectedStrategyID := uuid.MustParse("55555555-5555-5555-5555-555555555555")
	oppRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{
		{ID: uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa"), StrategyID: queuedStrategyID, Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL"},
		{ID: uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"), StrategyID: selectedStrategyID, Status: domain.OpportunityStatusSelected, MarketType: domain.MarketTypeStock, Ticker: "MSFT"},
	}}
	decRepo := &portfolioAllocatorDecisionRepo{items: []domain.AllocationDecision{
		{ID: uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc"), Mode: domain.AllocationDecisionModeShadow, Action: domain.AllocationDecisionActionShadowSelected, Score: 91.2},
		{ID: uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd"), Mode: domain.AllocationDecisionModeShadow, Action: domain.AllocationDecisionActionShadowRejected, Score: 12.3},
	}}
	deps := testDeps()
	deps.OpportunityRepo = oppRepo
	deps.AllocationDecisionRepo = decRepo
	srv := newTestServerWithDeps(t, deps)

	rr := doRequest(t, srv, http.MethodGet, "/api/v1/portfolio/allocator/opportunities?status=queued", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	opps := decodeJSON[portfolioAllocatorOpportunityListResponse](t, rr)
	if len(opps.Data) != 1 || opps.Data[0].Status != domain.OpportunityStatusQueued {
		t.Fatalf("opportunities = %+v, want one queued item", opps.Data)
	}
	if opps.Total != 1 || oppRepo.lastFilter.Status != domain.OpportunityStatusQueued {
		t.Fatalf("unexpected opportunity list metadata: total=%d filter=%+v", opps.Total, oppRepo.lastFilter)
	}

	rr = doRequest(t, srv, http.MethodGet, "/api/v1/portfolio/allocator/decisions?mode=shadow", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	decisions := decodeJSON[portfolioAllocatorDecisionListResponse](t, rr)
	if len(decisions.Data) != 2 || decisions.Total != 2 || decRepo.lastFilter.Mode != domain.AllocationDecisionModeShadow {
		t.Fatalf("decisions = %+v, filter=%+v", decisions.Data, decRepo.lastFilter)
	}

	rr = doRequest(t, srv, http.MethodGet, "/api/v1/portfolio/allocator/summary", nil)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusOK, rr.Body.String())
	}
	summary := decodeJSON[portfolioAllocatorSummaryResponse](t, rr)
	if summary.OpportunityCountsByStatus[domain.OpportunityStatusQueued.String()] != 1 || summary.OpportunityCountsByStatus[domain.OpportunityStatusSelected.String()] != 1 {
		t.Fatalf("summary counts = %+v", summary.OpportunityCountsByStatus)
	}
	if len(summary.RecentDecisions) != 2 {
		t.Fatalf("recent decisions = %+v, want 2", summary.RecentDecisions)
	}
	if len(summary.Warnings) != 0 {
		t.Fatalf("warnings = %+v, want none", summary.Warnings)
	}
}

func filterOpportunities(items []domain.Opportunity, filter repository.OpportunityFilter) []domain.Opportunity {
	if filter.Status == "" {
		return append([]domain.Opportunity(nil), items...)
	}
	out := make([]domain.Opportunity, 0, len(items))
	for _, item := range items {
		if item.Status == filter.Status {
			out = append(out, item)
		}
	}
	return out
}

func filterAllocationDecisions(items []domain.AllocationDecision, filter repository.AllocationDecisionFilter) []domain.AllocationDecision {
	if filter.Mode == "" {
		return append([]domain.AllocationDecision(nil), items...)
	}
	out := make([]domain.AllocationDecision, 0, len(items))
	for _, item := range items {
		if item.Mode == filter.Mode {
			out = append(out, item)
		}
	}
	return out
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
