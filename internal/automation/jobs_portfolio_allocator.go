package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/portfolio"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/google/uuid"
)

var portfolioAllocatorSpec = scheduler.ScheduleSpec{
	Type:         scheduler.ScheduleTypeAfterHours,
	Cron:         "15,45 * * * *",
	SkipWeekends: true,
	SkipHolidays: true,
}

func (o *JobOrchestrator) registerPortfolioAllocatorJobs() {
	if o.deps.OpportunityRepo == nil || o.deps.AllocationDecisionRepo == nil {
		o.logger.Info("portfolio_allocator: skipped — repositories not configured")
		return
	}

	o.Register("portfolio_allocator", "Shadow portfolio allocator", portfolioAllocatorSpec, o.runPortfolioAllocator)
}

func (o *JobOrchestrator) runPortfolioAllocator(ctx context.Context) error {
	if o.deps.OpportunityRepo == nil || o.deps.AllocationDecisionRepo == nil {
		return fmt.Errorf("portfolio_allocator: repositories not configured")
	}
	mode := o.portfolioAllocatorMode()

	opportunities, err := o.deps.OpportunityRepo.List(ctx, repository.OpportunityFilter{Status: domain.OpportunityStatusQueued}, 100, 0)
	if err != nil {
		return fmt.Errorf("portfolio_allocator: list opportunities: %w", err)
	}

	state, warnings, err := o.buildPortfolioAllocatorState(ctx)
	if err != nil {
		return err
	}

	allocCfg := portfolio.DefaultAllocatorConfig()
	allocCfg.Mode = mode
	result := portfolio.AllocateShadow(opportunities, state, allocCfg)
	opportunityByID := make(map[uuid.UUID]domain.Opportunity, len(opportunities))
	for _, opportunity := range opportunities {
		opportunityByID[opportunity.ID] = opportunity
	}

	for i := range result.Decisions {
		decision := result.Decisions[i]
		decision.Mode = domain.AllocationDecisionMode(mode)

		if mode == portfolio.AllocatorModePaper && decision.Action == domain.AllocationDecisionActionShadowSelected {
			decision = o.executePaperAllocatorDecision(ctx, decision, opportunityByID)
		}

		if err := o.deps.AllocationDecisionRepo.Create(ctx, &decision); err != nil {
			return fmt.Errorf("portfolio_allocator: persist decision: %w", err)
		}
		if mode == portfolio.AllocatorModePaper {
			if err := o.updatePaperOpportunityStatus(ctx, decision); err != nil {
				return err
			}
		}
	}

	summary := map[string]int{
		"evaluated":           result.Summary.Evaluated,
		"eligible":            result.Summary.Eligible,
		"selected":            result.Summary.Selected,
		"rejected":            result.Summary.Rejected,
		"persisted_decisions": len(result.Decisions),
	}
	o.SetLastSummary("portfolio_allocator", summary)

	fields := []any{
		slog.Int("queued_opportunities", len(opportunities)),
		slog.Int("evaluated", result.Summary.Evaluated),
		slog.Int("eligible", result.Summary.Eligible),
		slog.Int("selected", result.Summary.Selected),
		slog.Int("rejected", result.Summary.Rejected),
		slog.Int("persisted_decisions", len(result.Decisions)),
	}
	for _, warning := range warnings {
		fields = append(fields, slog.String("warning", warning))
	}
	o.logger.Info("portfolio_allocator: completed", fields...)
	return nil
}

func (o *JobOrchestrator) updatePaperOpportunityStatus(ctx context.Context, decision domain.AllocationDecision) error {
	if o.deps.OpportunityRepo == nil || decision.OpportunityID == nil {
		return nil
	}
	switch decision.Action {
	case domain.AllocationDecisionActionExecuted:
		if err := o.deps.OpportunityRepo.UpdateStatus(ctx, *decision.OpportunityID, domain.OpportunityStatusExecuted, ""); err != nil {
			return fmt.Errorf("portfolio_allocator: mark opportunity executed: %w", err)
		}
	case domain.AllocationDecisionActionExecutionRejected:
		if err := o.deps.OpportunityRepo.UpdateStatus(ctx, *decision.OpportunityID, domain.OpportunityStatusRejected, strings.Join(decision.Reasons, "; ")); err != nil {
			return fmt.Errorf("portfolio_allocator: mark opportunity rejected: %w", err)
		}
	}
	return nil
}

func (o *JobOrchestrator) portfolioAllocatorMode() portfolio.AllocatorMode {
	mode := o.deps.PortfolioAllocatorMode
	if mode == "" {
		return portfolio.AllocatorModeShadow
	}
	return mode
}

func (o *JobOrchestrator) executePaperAllocatorDecision(ctx context.Context, decision domain.AllocationDecision, opportunities map[uuid.UUID]domain.Opportunity) domain.AllocationDecision {
	decision.Mode = domain.AllocationDecisionModePaper
	decision.Action = domain.AllocationDecisionActionPaperOrderIntent

	if decision.OpportunityID == nil {
		return paperAllocatorRejected(decision, "missing_opportunity_id")
	}
	opportunity, ok := opportunities[*decision.OpportunityID]
	if !ok {
		return paperAllocatorRejected(decision, "missing_opportunity")
	}
	if o.deps.StrategyRepo == nil {
		return paperAllocatorRejected(decision, "missing_strategy_repo")
	}
	strategy, err := o.deps.StrategyRepo.Get(ctx, opportunity.StrategyID)
	if err != nil || strategy == nil {
		return paperAllocatorRejected(decision, "missing_strategy")
	}

	executor := portfolio.NewPaperExecutor(portfolio.PaperExecutorDeps{Processor: o.deps.PortfolioPaperProcessor})
	result, err := executor.ExecutePaperDecision(ctx, opportunity, decision, *strategy)
	if err != nil {
		return paperAllocatorRejected(decision, "paper_execution_error")
	}
	if result.Action == domain.AllocationDecisionActionExecutionRejected {
		return paperAllocatorRejected(decision, result.Reason)
	}
	decision.Action = result.Action
	decision.CreatedOrderID = result.OrderID
	decision.Reasons = append([]string(nil), decision.Reasons...)
	if result.Reason != "" {
		decision.Reasons = append(decision.Reasons, result.Reason)
	}
	return decision
}

func paperAllocatorRejected(decision domain.AllocationDecision, reason string) domain.AllocationDecision {
	decision.Action = domain.AllocationDecisionActionExecutionRejected
	decision.Reasons = append([]string(nil), decision.Reasons...)
	if reason != "" {
		decision.Reasons = append(decision.Reasons, reason)
	}
	return decision
}

func (o *JobOrchestrator) buildPortfolioAllocatorState(ctx context.Context) (portfolio.PortfolioState, []string, error) {
	state := portfolio.PortfolioState{
		MarketExposure: map[domain.MarketType]float64{},
		OpenTickers:    map[string]bool{},
	}
	warnings := make([]string, 0, 2)

	positions := make([]domain.Position, 0)
	if o.deps.PositionRepo != nil {
		items, err := o.deps.PositionRepo.GetOpen(ctx, repository.PositionFilter{}, 100, 0)
		if err != nil {
			return state, warnings, fmt.Errorf("portfolio_allocator: load open positions: %w", err)
		}
		positions = items
	} else {
		warnings = append(warnings, "positions_unavailable")
	}

	var grossExposure float64
	for _, position := range positions {
		exposure := portfolioPositionExposure(position)
		grossExposure += exposure
		state.MarketExposure[position.MarketType] += exposure
		if ticker := strings.TrimSpace(position.Ticker); ticker != "" {
			state.OpenTickers[ticker] = true
			state.OpenTickers[strings.ToUpper(ticker)] = true
			state.OpenTickers[strings.ToLower(ticker)] = true
		}
	}

	state.Equity = 100000
	state.BuyingPower = maxFloat(100000-grossExposure, 0)
	warnings = append(warnings, "paper_account_balance_fallback")
	state.GrossExposure = grossExposure
	return state, warnings, nil
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func portfolioPositionExposure(position domain.Position) float64 {
	price := position.CurrentPrice
	if price == nil || *price <= 0 {
		if position.AvgEntry > 0 {
			p := position.AvgEntry
			price = &p
		}
	}
	if price == nil || *price <= 0 || position.Quantity <= 0 {
		return 0
	}
	return position.Quantity * *price
}
