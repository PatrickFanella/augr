package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type portfolioAllocatorBalanceStub struct {
	balance execution.Balance
	err     error
}

func (s portfolioAllocatorBalanceStub) GetAccountBalance(context.Context) (execution.Balance, error) {
	return s.balance, s.err
}

func paperAllocatorStateDeps() (repository.PositionRepository, PortfolioAccountBalanceSource) {
	return newRecordingPositionRepo(), portfolioAllocatorBalanceStub{balance: execution.Balance{Currency: "USD", Cash: 100000, BuyingPower: 100000, Equity: 100000}}
}

type portfolioAllocatorOpportunityRepo struct {
	items             []domain.Opportunity
	lastFilter        repository.OpportunityFilter
	lastLimit         int
	lastOffset        int
	lastAsOf          time.Time
	expireCalls       int
	updateStatusCalls int
	lastStatus        domain.OpportunityStatus
	lastRejectReason  string
	statusHistory     []domain.OpportunityStatus
}

func (r *portfolioAllocatorOpportunityRepo) Create(context.Context, *domain.Opportunity) error {
	return nil
}

func (r *portfolioAllocatorOpportunityRepo) UpsertQueuedByDedupeKey(context.Context, *domain.Opportunity) error {
	return nil
}

func (r *portfolioAllocatorOpportunityRepo) Get(context.Context, uuid.UUID) (*domain.Opportunity, error) {
	return nil, repository.ErrNotFound
}

func (r *portfolioAllocatorOpportunityRepo) List(_ context.Context, filter repository.OpportunityFilter, limit, offset int) ([]domain.Opportunity, error) {
	r.lastFilter = filter
	r.lastLimit = limit
	r.lastOffset = offset
	if filter.Status == "" {
		return append([]domain.Opportunity(nil), r.items...), nil
	}
	out := make([]domain.Opportunity, 0, len(r.items))
	for _, item := range r.items {
		if item.Status == filter.Status {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *portfolioAllocatorOpportunityRepo) ExpireQueuedBefore(_ context.Context, before time.Time) (int64, error) {
	r.expireCalls++
	r.lastAsOf = before
	count := int64(0)
	for i := range r.items {
		if r.items[i].Status == domain.OpportunityStatusQueued && !r.items[i].ExpiresAt.After(before) {
			r.items[i].Status = domain.OpportunityStatusExpired
			r.items[i].RejectReason = "expired_before_allocation"
			count++
		}
	}
	return count, nil
}

func (r *portfolioAllocatorOpportunityRepo) ListQueuedForAllocation(_ context.Context, asOf time.Time) ([]domain.Opportunity, error) {
	r.lastAsOf = asOf
	out := make([]domain.Opportunity, 0, len(r.items))
	for _, item := range r.items {
		if item.Status == domain.OpportunityStatusQueued && item.ExpiresAt.After(asOf) {
			out = append(out, item)
		}
	}
	return out, nil
}

func (r *portfolioAllocatorOpportunityRepo) Count(_ context.Context, filter repository.OpportunityFilter) (int, error) {
	items, _ := r.List(context.Background(), filter, 0, 0)
	return len(items), nil
}

func (r *portfolioAllocatorOpportunityRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status domain.OpportunityStatus, rejectReason string) error {
	r.updateStatusCalls++
	r.lastStatus = status
	r.lastRejectReason = rejectReason
	r.statusHistory = append(r.statusHistory, status)
	return nil
}

type portfolioAllocatorDecisionRepo struct {
	created []*domain.AllocationDecision
}

type portfolioAllocatorRunRepo struct {
	runs map[uuid.UUID]domain.PipelineRun
	err  error
}

func (*portfolioAllocatorRunRepo) Create(context.Context, *domain.PipelineRun) error { return nil }

func (r *portfolioAllocatorRunRepo) GetByID(_ context.Context, id uuid.UUID) (*domain.PipelineRun, error) {
	if r.err != nil {
		return nil, r.err
	}
	run, ok := r.runs[id]
	if !ok {
		return nil, repository.ErrNotFound
	}
	return &run, nil
}

func (r *portfolioAllocatorRunRepo) Get(ctx context.Context, id uuid.UUID, _ time.Time) (*domain.PipelineRun, error) {
	return r.GetByID(ctx, id)
}

func (*portfolioAllocatorRunRepo) List(context.Context, repository.PipelineRunFilter, int, int) ([]domain.PipelineRun, error) {
	return nil, nil
}

func (*portfolioAllocatorRunRepo) Count(context.Context, repository.PipelineRunFilter) (int, error) {
	return 0, nil
}

func (*portfolioAllocatorRunRepo) UpdateStatus(context.Context, uuid.UUID, time.Time, repository.PipelineRunStatusUpdate) error {
	return nil
}

func (r *portfolioAllocatorDecisionRepo) Create(_ context.Context, decision *domain.AllocationDecision) error {
	cloned := *decision
	r.created = append(r.created, &cloned)
	return nil
}

func (r *portfolioAllocatorDecisionRepo) List(_ context.Context, filter repository.AllocationDecisionFilter, limit, offset int) ([]domain.AllocationDecision, error) {
	_ = limit
	_ = offset
	out := make([]domain.AllocationDecision, 0, len(r.created))
	for _, decision := range r.created {
		if filter.Mode != "" && decision.Mode != filter.Mode {
			continue
		}
		out = append(out, *decision)
	}
	return out, nil
}

func (r *portfolioAllocatorDecisionRepo) Count(_ context.Context, filter repository.AllocationDecisionFilter) (int, error) {
	decisions, _ := r.List(context.Background(), filter, 0, 0)
	return len(decisions), nil
}

type portfolioAllocatorStrategyRepo struct {
	strategy *domain.Strategy
	getCalls int
}

func (r *portfolioAllocatorStrategyRepo) Create(context.Context, *domain.Strategy) error { return nil }

func (r *portfolioAllocatorStrategyRepo) Get(_ context.Context, id uuid.UUID) (*domain.Strategy, error) {
	r.getCalls++
	if r.strategy == nil || r.strategy.ID != id {
		return nil, repository.ErrNotFound
	}
	cloned := *r.strategy
	return &cloned, nil
}

func (r *portfolioAllocatorStrategyRepo) List(context.Context, repository.StrategyFilter, int, int) ([]domain.Strategy, error) {
	if r.strategy == nil {
		return nil, nil
	}
	return []domain.Strategy{*r.strategy}, nil
}

func (r *portfolioAllocatorStrategyRepo) Count(context.Context, repository.StrategyFilter) (int, error) {
	return 0, nil
}

func (r *portfolioAllocatorStrategyRepo) Update(context.Context, *domain.Strategy) error { return nil }

func (r *portfolioAllocatorStrategyRepo) Delete(context.Context, uuid.UUID) error { return nil }

func (r *portfolioAllocatorStrategyRepo) UpdateThesis(context.Context, uuid.UUID, json.RawMessage) error {
	return nil
}

func (r *portfolioAllocatorStrategyRepo) GetThesisRaw(context.Context, uuid.UUID) (json.RawMessage, error) {
	return nil, nil
}

type portfolioPaperProcessorStub struct {
	called     int
	signal     execution.FinalSignal
	plan       execution.TradingPlan
	strategyID uuid.UUID
}

func (p *portfolioPaperProcessorStub) ProcessPaperOrder(_ context.Context, req portfolio.PaperOrderRequest) (portfolio.PaperOrderResult, error) {
	p.called++
	p.signal = req.Signal
	p.plan = req.Plan
	p.strategyID = req.StrategyID
	id := uuid.New()
	return portfolio.PaperOrderResult{OrderID: &id, Status: domain.OrderStatusFilled}, nil
}

type preclaimFailOpportunityRepo struct {
	portfolioAllocatorOpportunityRepo
}

func (r *preclaimFailOpportunityRepo) UpdateStatus(_ context.Context, _ uuid.UUID, status domain.OpportunityStatus, rejectReason string) error {
	r.updateStatusCalls++
	r.lastStatus = status
	r.lastRejectReason = rejectReason
	r.statusHistory = append(r.statusHistory, status)
	if r.updateStatusCalls == 1 {
		return fmt.Errorf("preclaim failed")
	}
	return nil
}

func TestPortfolioAllocatorJobRegistrationWithNilDeps(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.registerPortfolioAllocatorJobs()
	if _, ok := orch.jobs["portfolio_allocator"]; ok {
		t.Fatal("portfolio_allocator registered unexpectedly")
	}
}

func TestValidatePortfolioOpportunitySourcesRejectsFailedAndMismatchedRuns(t *testing.T) {
	t.Parallel()

	strategyID := uuid.New()
	validRunID := uuid.New()
	failedRunID := uuid.New()
	mismatchedRunID := uuid.New()
	runRepo := &portfolioAllocatorRunRepo{runs: map[uuid.UUID]domain.PipelineRun{
		validRunID:      {ID: validRunID, StrategyID: strategyID, Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalBuy},
		failedRunID:     {ID: failedRunID, StrategyID: strategyID, Status: domain.PipelineStatusFailed, Signal: domain.PipelineSignalHold},
		mismatchedRunID: {ID: mismatchedRunID, StrategyID: strategyID, Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalSell},
	}}
	orch := NewJobOrchestrator(OrchestratorDeps{RunRepo: runRepo})
	opportunities := []domain.Opportunity{
		{ID: uuid.New(), StrategyID: strategyID, PipelineRunID: &validRunID, Signal: domain.PipelineSignalBuy},
		{ID: uuid.New(), StrategyID: strategyID, PipelineRunID: &failedRunID, Signal: domain.PipelineSignalBuy},
		{ID: uuid.New(), StrategyID: strategyID, PipelineRunID: &mismatchedRunID, Signal: domain.PipelineSignalBuy},
		{ID: uuid.New(), StrategyID: strategyID, Signal: domain.PipelineSignalBuy},
	}

	valid, rejected, err := orch.validatePortfolioOpportunitySources(context.Background(), opportunities, portfolio.AllocatorModeShadow)
	if err != nil {
		t.Fatalf("validatePortfolioOpportunitySources() error = %v", err)
	}
	if len(valid) != 1 || valid[0].ID != opportunities[0].ID {
		t.Fatalf("valid opportunities = %#v, want only completed matching source", valid)
	}
	if len(rejected) != 3 {
		t.Fatalf("rejected decisions = %#v, want three", rejected)
	}
	wantReasons := []string{"source_run_not_completed", "source_signal_mismatch", "source_run_missing"}
	for i, want := range wantReasons {
		if len(rejected[i].Reasons) != 1 || rejected[i].Reasons[0] != want {
			t.Fatalf("rejected[%d] reasons = %v, want %q", i, rejected[i].Reasons, want)
		}
	}
}

func TestValidatePortfolioOpportunitySourcesFailsClosedOnRunRepositoryError(t *testing.T) {
	t.Parallel()

	runID := uuid.New()
	orch := NewJobOrchestrator(OrchestratorDeps{RunRepo: &portfolioAllocatorRunRepo{err: errors.New("run store unavailable")}})
	_, _, err := orch.validatePortfolioOpportunitySources(context.Background(), []domain.Opportunity{{ID: uuid.New(), PipelineRunID: &runID}}, portfolio.AllocatorModeShadow)
	if err == nil || !strings.Contains(err.Error(), "load source run") {
		t.Fatalf("validatePortfolioOpportunitySources() error = %v, want repository failure", err)
	}
}

func TestPortfolioAllocatorJobPersistsShadowDecisions(t *testing.T) {
	t.Parallel()

	now := time.Now()
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{
		{
			ID:                uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			StrategyID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			Status:            domain.OpportunityStatusQueued,
			MarketType:        domain.MarketTypeStock,
			Ticker:            "AAPL",
			Side:              domain.OrderSideBuy,
			Signal:            domain.PipelineSignalBuy,
			Confidence:        1,
			EdgePct:           0.05,
			ExpectedReturnPct: 0.1,
			MaxLossPct:        0.01,
			LiquidityUSD:      5_000_000,
			MarketCapUSD:      10_000_000_000,
			SpreadPct:         0.001,
			ProposedNotional:  2_000,
			Reason:            "strong shadow opportunity",
			ExpiresAt:         now.Add(24 * time.Hour),
			CreatedAt:         now.Add(-time.Hour),
			DedupeKey:         "aapl-shadow-1",
		},
	}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	orch := NewJobOrchestrator(OrchestratorDeps{
		OpportunityRepo:        opportunityRepo,
		AllocationDecisionRepo: decisionRepo,
	})
	orch.registerPortfolioAllocatorJobs()
	job, ok := orch.jobs["portfolio_allocator"]
	if !ok {
		t.Fatal("portfolio_allocator job not registered")
	}

	if err := job.Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	if opportunityRepo.updateStatusCalls != 1 || opportunityRepo.lastStatus != domain.OpportunityStatusSelected {
		t.Fatalf("UpdateStatus calls/status = %d/%s, want selected once", opportunityRepo.updateStatusCalls, opportunityRepo.lastStatus)
	}
	if len(decisionRepo.created) == 0 {
		t.Fatal("expected shadow decisions to be persisted")
	}
	if decisionRepo.created[0].Mode != domain.AllocationDecisionModeShadow {
		t.Fatalf("decision mode = %s, want shadow", decisionRepo.created[0].Mode)
	}
	if decisionRepo.created[0].Action != domain.AllocationDecisionActionShadowSelected {
		t.Fatalf("decision action = %s, want shadow_selected", decisionRepo.created[0].Action)
	}
	status := orch.Status()
	if len(status) == 0 || status[0].LastSummary == nil {
		t.Fatal("expected orchestrator to record last summary")
	}
}

func TestPortfolioAllocatorJobSecondShadowRunDoesNotRepeatSelectedOpportunity(t *testing.T) {
	t.Parallel()

	now := time.Now()
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{{ID: uuid.MustParse("11111111-1111-1111-1111-111111111111"), StrategyID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 1, EdgePct: 0.05, ExpectedReturnPct: 0.1, MaxLossPct: 0.01, LiquidityUSD: 5_000_000, MarketCapUSD: 10_000_000_000, SpreadPct: 0.001, ProposedNotional: 2_000, Reason: "strong shadow opportunity", ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now.Add(-time.Hour), DedupeKey: "aapl-shadow-1"}}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	orch := NewJobOrchestrator(OrchestratorDeps{OpportunityRepo: opportunityRepo, AllocationDecisionRepo: decisionRepo})
	orch.registerPortfolioAllocatorJobs()
	job := orch.jobs["portfolio_allocator"]
	if job == nil {
		t.Fatal("portfolio_allocator job not registered")
	}
	if err := job.Fn(context.Background()); err != nil {
		t.Fatalf("first run error = %v", err)
	}
	if len(decisionRepo.created) != 1 || decisionRepo.created[0].Action != domain.AllocationDecisionActionShadowSelected {
		t.Fatalf("unexpected first run decisions: %+v", decisionRepo.created)
	}
	opportunityRepo.items[0].Status = domain.OpportunityStatusSelected
	decisionRepo.created = nil
	if err := job.Fn(context.Background()); err != nil {
		t.Fatalf("second run error = %v", err)
	}
	if len(decisionRepo.created) != 0 {
		t.Fatalf("expected no repeat decisions, got %+v", decisionRepo.created)
	}
}

func TestPortfolioAllocatorJobUpdatesShadowStatuses(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{{ID: uuid.New(), StrategyID: uuid.New(), Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 1, EdgePct: 0.05, ExpectedReturnPct: 0.1, MaxLossPct: 0.01, LiquidityUSD: 5_000_000, MarketCapUSD: 10_000_000_000, SpreadPct: 0.001, ProposedNotional: 2_000, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), DedupeKey: "selected"}, {ID: uuid.New(), StrategyID: uuid.New(), Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "MSFT", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 0.1, EdgePct: 0.001, ExpectedReturnPct: 0.1, MaxLossPct: 0.01, LiquidityUSD: 1, MarketCapUSD: 1, SpreadPct: 0.2, ProposedNotional: 1, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), DedupeKey: "rejected"}}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	orch := NewJobOrchestrator(OrchestratorDeps{OpportunityRepo: opportunityRepo, AllocationDecisionRepo: decisionRepo})
	orch.registerPortfolioAllocatorJobs()
	if err := orch.jobs["portfolio_allocator"].Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	if opportunityRepo.updateStatusCalls != 2 {
		t.Fatalf("update calls = %d, want 2", opportunityRepo.updateStatusCalls)
	}
	if opportunityRepo.statusHistory[0] != domain.OpportunityStatusSelected || opportunityRepo.statusHistory[1] != domain.OpportunityStatusRejected {
		t.Fatalf("status history = %+v", opportunityRepo.statusHistory)
	}
}

func TestPortfolioAllocatorJobExpiresDueBeforeAllocation(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	opp := domain.Opportunity{ID: uuid.New(), StrategyID: uuid.New(), Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 1, EdgePct: 0.05, ExpectedReturnPct: 0.1, MaxLossPct: 0.01, LiquidityUSD: 1, MarketCapUSD: 1, SpreadPct: 0.001, ProposedNotional: 1, ExpiresAt: now.Add(-time.Minute), CreatedAt: now.Add(-time.Hour), DedupeKey: "expired-queued"}
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{opp}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	orch := NewJobOrchestrator(OrchestratorDeps{OpportunityRepo: opportunityRepo, AllocationDecisionRepo: decisionRepo})
	orch.registerPortfolioAllocatorJobs()
	if err := orch.jobs["portfolio_allocator"].Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	if opportunityRepo.expireCalls != 1 || opportunityRepo.items[0].Status != domain.OpportunityStatusExpired {
		t.Fatalf("expire behavior unexpected: %+v", opportunityRepo)
	}
	if len(decisionRepo.created) != 0 {
		t.Fatalf("expected no decisions, got %d", len(decisionRepo.created))
	}
	if got := orch.Status()[0].LastSummary; got == nil || got["expired"] != 1 || got["queued_loaded"] != 0 {
		t.Fatalf("summary = %+v", got)
	}
}

func TestPortfolioAllocatorJobLoadsCompleteSnapshot(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	items := make([]domain.Opportunity, 205)
	for i := range items {
		items[i] = domain.Opportunity{ID: uuid.New(), StrategyID: uuid.New(), Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 1, EdgePct: 0.05, ExpectedReturnPct: 0.1, MaxLossPct: 0.01, LiquidityUSD: 1, MarketCapUSD: 1, SpreadPct: 0.001, ProposedNotional: 1, ExpiresAt: now.Add(time.Duration(i+1) * time.Minute), CreatedAt: now.Add(-time.Duration(i) * time.Minute), DedupeKey: uuid.NewString()}
	}
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: items}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	orch := NewJobOrchestrator(OrchestratorDeps{OpportunityRepo: opportunityRepo, AllocationDecisionRepo: decisionRepo})
	orch.registerPortfolioAllocatorJobs()
	if err := orch.jobs["portfolio_allocator"].Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	if opportunityRepo.lastLimit != 0 || len(decisionRepo.created) != 205 {
		t.Fatalf("snapshot/decisions unexpected: limit=%d decisions=%d", opportunityRepo.lastLimit, len(decisionRepo.created))
	}
}

func TestPortfolioAllocatorJobReportsLifecycleCounts(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{{ID: uuid.New(), StrategyID: uuid.New(), Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 1, EdgePct: 0.05, ExpectedReturnPct: 0.1, MaxLossPct: 0.01, LiquidityUSD: 1, MarketCapUSD: 1, SpreadPct: 0.001, ProposedNotional: 1, ExpiresAt: now.Add(time.Hour), CreatedAt: now.Add(-time.Hour), DedupeKey: "queued"}}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	orch := NewJobOrchestrator(OrchestratorDeps{OpportunityRepo: opportunityRepo, AllocationDecisionRepo: decisionRepo})
	orch.registerPortfolioAllocatorJobs()
	if err := orch.jobs["portfolio_allocator"].Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	summary := orch.Status()[0].LastSummary
	for _, key := range []string{"expired", "queued_loaded", "evaluated", "persisted_decisions"} {
		if _, ok := summary[key]; !ok {
			t.Fatalf("missing summary key %q in %#v", key, summary)
		}
	}
}

func TestPortfolioAllocatorJobPaperModeExecutesPaperIntent(t *testing.T) {
	t.Parallel()

	now := time.Now()
	runID := uuid.New()
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{
		{
			ID:                uuid.MustParse("11111111-1111-1111-1111-111111111111"),
			StrategyID:        uuid.MustParse("22222222-2222-2222-2222-222222222222"),
			PipelineRunID:     &runID,
			Status:            domain.OpportunityStatusQueued,
			MarketType:        domain.MarketTypeStock,
			Ticker:            "AAPL",
			Side:              domain.OrderSideBuy,
			Signal:            domain.PipelineSignalBuy,
			Confidence:        1,
			EdgePct:           0.05,
			ExpectedReturnPct: 0.1,
			MaxLossPct:        0.05,
			EntryPrice:        100,
			LiquidityUSD:      5_000_000,
			MarketCapUSD:      10_000_000_000,
			SpreadPct:         0.001,
			ProposedNotional:  2_000,
			Reason:            "strong paper opportunity",
			ExpiresAt:         now.Add(24 * time.Hour),
			CreatedAt:         now.Add(-time.Hour),
			DedupeKey:         "aapl-paper-1",
		},
	}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	strategyRepo := &portfolioAllocatorStrategyRepo{strategy: &domain.Strategy{
		ID:         opportunityRepo.items[0].StrategyID,
		Name:       "paper-aapl",
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    true,
	}}
	processor := &portfolioPaperProcessorStub{}
	positionRepo, accountBalance := paperAllocatorStateDeps()
	orch := NewJobOrchestrator(OrchestratorDeps{
		OpportunityRepo:         opportunityRepo,
		AllocationDecisionRepo:  decisionRepo,
		StrategyRepo:            strategyRepo,
		PortfolioAllocatorMode:  portfolio.AllocatorModePaper,
		PortfolioPaperProcessor: processor,
		PositionRepo:            positionRepo,
		PortfolioAccountBalance: accountBalance,
		RunRepo: &portfolioAllocatorRunRepo{runs: map[uuid.UUID]domain.PipelineRun{
			runID: {ID: runID, StrategyID: opportunityRepo.items[0].StrategyID, Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalBuy},
		}},
	})
	orch.registerPortfolioAllocatorJobs()
	job := orch.jobs["portfolio_allocator"]
	if job == nil {
		t.Fatal("portfolio_allocator job not registered")
	}

	if err := job.Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	if processor.called != 1 {
		t.Fatalf("processor called %d times, want 1", processor.called)
	}
	if len(opportunityRepo.statusHistory) != 2 || opportunityRepo.statusHistory[0] != domain.OpportunityStatusSelected || opportunityRepo.statusHistory[1] != domain.OpportunityStatusExecuted {
		t.Fatalf("status history = %+v, want preclaim then final executed", opportunityRepo.statusHistory)
	}
	if len(decisionRepo.created) == 0 {
		t.Fatal("expected paper decision to be persisted")
	}
	decision := decisionRepo.created[0]
	if decision.Mode != domain.AllocationDecisionModePaper {
		t.Fatalf("decision mode = %s, want paper", decision.Mode)
	}
	if decision.Action != domain.AllocationDecisionActionExecuted {
		t.Fatalf("decision action = %s, want executed", decision.Action)
	}
	if decision.CreatedOrderID == nil {
		t.Fatal("CreatedOrderID = nil, want created paper order id")
	}
	if decisionRepo.created[0].OpportunityID == nil || *decisionRepo.created[0].OpportunityID != opportunityRepo.items[0].ID {
		t.Fatalf("unexpected opportunity id on decision: %+v", decisionRepo.created[0])
	}
	if status := orch.Status()[0].LastSummary; status["executed"] != 1 || status["execution_rejected"] != 0 {
		t.Fatalf("summary counts = %#v, want executed=1 execution_rejected=0", status)
	}
	if opportunityRepo.updateStatusCalls != 2 || opportunityRepo.lastStatus != domain.OpportunityStatusExecuted {
		t.Fatalf("UpdateStatus calls/status = %d/%s, want preclaim+executed", opportunityRepo.updateStatusCalls, opportunityRepo.lastStatus)
	}
	if processor.plan.EntryType != "market" || processor.plan.EntryPrice != 100 || math.Abs(processor.plan.StopLoss-95) > 1e-9 {
		t.Fatalf("unexpected paper plan: %+v", processor.plan)
	}
}

func TestPortfolioAllocatorJobPaperModeRejectsWithoutStrategyRepo(t *testing.T) {
	t.Parallel()

	now := time.Now()
	runID := uuid.New()
	opportunityRepo := &portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{{
		ID:                uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		StrategyID:        uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		PipelineRunID:     &runID,
		Status:            domain.OpportunityStatusQueued,
		MarketType:        domain.MarketTypeStock,
		Ticker:            "MSFT",
		Side:              domain.OrderSideBuy,
		Signal:            domain.PipelineSignalBuy,
		Confidence:        1,
		EdgePct:           0.05,
		ExpectedReturnPct: 0.1,
		MaxLossPct:        0.05,
		EntryPrice:        100,
		LiquidityUSD:      5_000_000,
		MarketCapUSD:      10_000_000_000,
		SpreadPct:         0.001,
		ProposedNotional:  2_000,
		Reason:            "paper opportunity without strategy repo",
		ExpiresAt:         now.Add(24 * time.Hour),
		CreatedAt:         now.Add(-time.Hour),
		DedupeKey:         "msft-paper-1",
	}}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	positionRepo, accountBalance := paperAllocatorStateDeps()
	orch := NewJobOrchestrator(OrchestratorDeps{
		OpportunityRepo:         opportunityRepo,
		AllocationDecisionRepo:  decisionRepo,
		PortfolioAllocatorMode:  portfolio.AllocatorModePaper,
		PositionRepo:            positionRepo,
		PortfolioAccountBalance: accountBalance,
		RunRepo: &portfolioAllocatorRunRepo{runs: map[uuid.UUID]domain.PipelineRun{
			runID: {ID: runID, StrategyID: opportunityRepo.items[0].StrategyID, Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalBuy},
		}},
	})
	orch.registerPortfolioAllocatorJobs()
	job := orch.jobs["portfolio_allocator"]
	if job == nil {
		t.Fatal("portfolio_allocator job not registered")
	}

	if err := job.Fn(context.Background()); err != nil {
		t.Fatalf("job run error = %v", err)
	}
	if len(decisionRepo.created) == 0 {
		t.Fatal("expected rejection decision to be persisted")
	}
	if decisionRepo.created[0].Action != domain.AllocationDecisionActionExecutionRejected {
		t.Fatalf("decision action = %s, want execution_rejected", decisionRepo.created[0].Action)
	}
	if got := strings.Join(decisionRepo.created[0].Reasons, ";"); !strings.Contains(got, "missing_strategy_repo") {
		t.Fatalf("expected missing_strategy_repo reason, got %q", got)
	}
}

func TestPortfolioAllocatorJobPaperModeStopsWhenPreclaimFails(t *testing.T) {
	t.Parallel()

	now := time.Now()
	runID := uuid.New()
	opportunityRepo := &preclaimFailOpportunityRepo{portfolioAllocatorOpportunityRepo{items: []domain.Opportunity{{ID: uuid.New(), StrategyID: uuid.New(), PipelineRunID: &runID, Status: domain.OpportunityStatusQueued, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, Signal: domain.PipelineSignalBuy, Confidence: 1, EdgePct: 0.05, ExpectedReturnPct: 0.1, MaxLossPct: 0.05, EntryPrice: 100, LiquidityUSD: 5_000_000, MarketCapUSD: 10_000_000_000, SpreadPct: 0.001, ProposedNotional: 2_000, ExpiresAt: now.Add(24 * time.Hour), CreatedAt: now.Add(-time.Hour), DedupeKey: "aapl-paper-1"}}}}
	decisionRepo := &portfolioAllocatorDecisionRepo{}
	strategyRepo := &portfolioAllocatorStrategyRepo{strategy: &domain.Strategy{ID: opportunityRepo.items[0].StrategyID, Name: "paper-aapl", Ticker: "AAPL", MarketType: domain.MarketTypeStock, Status: domain.StrategyStatusActive, IsPaper: true}}
	processor := &portfolioPaperProcessorStub{}
	positionRepo, accountBalance := paperAllocatorStateDeps()
	orch := NewJobOrchestrator(OrchestratorDeps{OpportunityRepo: opportunityRepo, AllocationDecisionRepo: decisionRepo, StrategyRepo: strategyRepo, PortfolioAllocatorMode: portfolio.AllocatorModePaper, PortfolioPaperProcessor: processor, PositionRepo: positionRepo, PortfolioAccountBalance: accountBalance, RunRepo: &portfolioAllocatorRunRepo{runs: map[uuid.UUID]domain.PipelineRun{runID: {ID: runID, StrategyID: opportunityRepo.items[0].StrategyID, Status: domain.PipelineStatusCompleted, Signal: domain.PipelineSignalBuy}}}})
	orch.registerPortfolioAllocatorJobs()
	err := orch.jobs["portfolio_allocator"].Fn(context.Background())
	if err == nil || !strings.Contains(err.Error(), "preclaim opportunity selected") {
		t.Fatalf("expected preclaim failure, got %v", err)
	}
	if processor.called != 0 {
		t.Fatalf("processor called %d times, want 0", processor.called)
	}
	if len(decisionRepo.created) != 0 {
		t.Fatalf("expected no persisted decision, got %+v", decisionRepo.created)
	}
}

func TestPortfolioAllocatorPaperModeRequiresCompleteFinancialState(t *testing.T) {
	t.Parallel()

	base := func() OrchestratorDeps {
		return OrchestratorDeps{
			OpportunityRepo:        &portfolioAllocatorOpportunityRepo{},
			AllocationDecisionRepo: &portfolioAllocatorDecisionRepo{},
			PortfolioAllocatorMode: portfolio.AllocatorModePaper,
		}
	}
	tests := map[string]func() OrchestratorDeps{
		"missing positions": base,
		"missing balance": func() OrchestratorDeps {
			deps := base()
			deps.PositionRepo = newRecordingPositionRepo()
			return deps
		},
		"balance error": func() OrchestratorDeps {
			deps := base()
			deps.PositionRepo = newRecordingPositionRepo()
			deps.PortfolioAccountBalance = portfolioAllocatorBalanceStub{err: errors.New("balance unavailable")}
			return deps
		},
		"invalid balance": func() OrchestratorDeps {
			deps := base()
			deps.PositionRepo = newRecordingPositionRepo()
			deps.PortfolioAccountBalance = portfolioAllocatorBalanceStub{balance: execution.Balance{Equity: 0, BuyingPower: 100}}
			return deps
		},
	}
	for name, makeDeps := range tests {
		name, makeDeps := name, makeDeps
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			deps := makeDeps()
			orch := NewJobOrchestrator(deps)
			orch.registerPortfolioAllocatorJobs()
			if err := orch.jobs["portfolio_allocator"].Fn(context.Background()); err == nil {
				t.Fatalf("paper allocator error = nil, want fail-closed financial state error")
			}
		})
	}
}
