package automation

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

type portfolioAllocatorOpportunityRepo struct {
	items             []domain.Opportunity
	lastFilter        repository.OpportunityFilter
	lastLimit         int
	lastOffset        int
	updateStatusCalls int
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
func (r *portfolioAllocatorOpportunityRepo) Count(_ context.Context, filter repository.OpportunityFilter) (int, error) {
	items, _ := r.List(context.Background(), filter, 0, 0)
	return len(items), nil
}
func (r *portfolioAllocatorOpportunityRepo) UpdateStatus(context.Context, uuid.UUID, domain.OpportunityStatus, string) error {
	r.updateStatusCalls++
	return nil
}

type portfolioAllocatorDecisionRepo struct {
	created []*domain.AllocationDecision
}

func (r *portfolioAllocatorDecisionRepo) Create(_ context.Context, decision *domain.AllocationDecision) error {
	copy := *decision
	r.created = append(r.created, &copy)
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

func TestPortfolioAllocatorJobRegistrationWithNilDeps(t *testing.T) {
	t.Parallel()

	orch := NewJobOrchestrator(OrchestratorDeps{})
	orch.registerPortfolioAllocatorJobs()
	if _, ok := orch.jobs["portfolio_allocator"]; ok {
		t.Fatal("portfolio_allocator registered unexpectedly")
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
	if opportunityRepo.updateStatusCalls != 0 {
		t.Fatalf("UpdateStatus called %d times, want 0", opportunityRepo.updateStatusCalls)
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
