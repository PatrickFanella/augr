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

	opportunities, err := o.deps.OpportunityRepo.List(ctx, repository.OpportunityFilter{Status: domain.OpportunityStatusQueued}, 100, 0)
	if err != nil {
		return fmt.Errorf("portfolio_allocator: list opportunities: %w", err)
	}

	state, warnings, err := o.buildPortfolioAllocatorState(ctx)
	if err != nil {
		return err
	}

	result := portfolio.AllocateShadow(opportunities, state, portfolio.DefaultAllocatorConfig())
	for i := range result.Decisions {
		decision := result.Decisions[i]
		if err := o.deps.AllocationDecisionRepo.Create(ctx, &decision); err != nil {
			return fmt.Errorf("portfolio_allocator: persist decision: %w", err)
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

	if grossExposure <= 0 {
		state.Equity = 100000
		state.BuyingPower = 100000
		warnings = append(warnings, "equity_non_positive")
	} else {
		state.Equity = grossExposure
		state.BuyingPower = grossExposure
	}
	state.GrossExposure = grossExposure
	return state, warnings, nil
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
