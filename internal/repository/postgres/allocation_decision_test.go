package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildAllocationDecisionQuery(t *testing.T) {
	strategyID := uuid.New()
	opportunityID := uuid.New()
	createdAfter := time.Date(2026, 6, 18, 12, 0, 0, 0, time.UTC)

	query, args := buildAllocationDecisionListQuery(repository.AllocationDecisionFilter{
		Mode:          domain.AllocationDecisionModeShadow,
		Action:        domain.AllocationDecisionActionShadowSelected,
		StrategyID:    &strategyID,
		OpportunityID: &opportunityID,
		CreatedAfter:  &createdAfter,
	}, 25, 50)

	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %#v", len(args), args)
	}
	assertContains(t, query, "mode = $1")
	assertContains(t, query, "action = $2")
	assertContains(t, query, "strategy_id = $3")
	assertContains(t, query, "opportunity_id = $4")
	assertContains(t, query, "created_at >= $5")
	assertContains(t, query, "LIMIT $6 OFFSET $7")
}

func TestAllocationDecisionRepoIntegration_CreateListAndCount(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newOpportunityIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAllocationDecisionRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	opportunityRepo := NewOpportunityRepo(pool)
	opportunity := &domain.Opportunity{
		StrategyID:        strategyID,
		MarketType:        domain.MarketTypeStock,
		Ticker:            "AAPL",
		Side:              domain.OrderSideBuy,
		Signal:            domain.PipelineSignalBuy,
		Status:            domain.OpportunityStatusSelected,
		Confidence:        0.9,
		EdgePct:           3.1,
		ExpectedReturnPct: 5.0,
		MaxLossPct:        1.2,
		LiquidityUSD:      1500000,
		SpreadPct:         0.11,
		ProposedNotional:  5000,
		SelectedNotional:  5000,
		Reason:            "allocator selected",
		ExpiresAt:         time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC),
		DedupeKey:         "alloc-AAPL-1",
	}
	if err := opportunityRepo.Create(ctx, opportunity); err != nil {
		t.Fatalf("Create(opportunity) error = %v", err)
	}

	createdOrderID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id) VALUES ($1)`, createdOrderID); err != nil {
		t.Fatalf("create order fixture: %v", err)
	}
	decision := &domain.AllocationDecision{
		OpportunityID:  &opportunity.ID,
		StrategyID:     &strategyID,
		Mode:           domain.AllocationDecisionModeShadow,
		Action:         domain.AllocationDecisionActionShadowSelected,
		Score:          91.5,
		NotionalUSD:    5000,
		Quantity:       25,
		Reasons:        []string{"edge", "liquidity"},
		CreatedOrderID: &createdOrderID,
	}
	if err := repo.Create(ctx, decision); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if decision.ID == uuid.Nil || decision.CreatedAt.IsZero() {
		t.Fatal("expected Create() to populate id/timestamp")
	}

	listed, err := repo.List(ctx, repository.AllocationDecisionFilter{Mode: domain.AllocationDecisionModeShadow}, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != decision.ID {
		t.Fatalf("unexpected list result: %+v", listed)
	}
	if listed[0].CreatedOrderID == nil || *listed[0].CreatedOrderID != createdOrderID {
		t.Fatalf("unexpected created order id: %+v", listed[0])
	}

	count, err := repo.Count(ctx, repository.AllocationDecisionFilter{Action: domain.AllocationDecisionActionShadowSelected})
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}
}
