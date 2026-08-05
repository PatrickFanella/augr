package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var (
	optionsExpirySpec    = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "0 23 * * 1-5", SkipWeekends: true, SkipHolidays: false}
	optionsReconcileSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "30 23 * * 1-5", SkipWeekends: true, SkipHolidays: false}
)

func (o *JobOrchestrator) registerOptionsLifecycleJobs() {
	if o.deps.PositionRepo != nil && o.deps.TradeRepo != nil && o.deps.DataService != nil {
		o.Register("options_expiry_settlement", "Cash-settle expired paper option positions", optionsExpirySpec, o.optionsExpirySettlement)
	}
	if o.deps.OrderRepo != nil && o.deps.PositionRepo != nil && o.deps.TradeRepo != nil {
		o.Register("options_lifecycle_reconcile", "Audit durable option order, position, trade, and leg-group state", optionsReconcileSpec, o.optionsLifecycleReconcile, "options_expiry_settlement")
	}
}

func (o *JobOrchestrator) optionsLifecycleReconcile(ctx context.Context) error {
	orders, err := listAllOptionOrders(ctx, o.deps.OrderRepo)
	if err != nil {
		return fmt.Errorf("options_lifecycle_reconcile: list orders: %w", err)
	}
	positions, err := listAllPositions(ctx, o.deps.PositionRepo)
	if err != nil {
		return fmt.Errorf("options_lifecycle_reconcile: list positions: %w", err)
	}
	trades, err := listAllOptionTrades(ctx, o.deps.TradeRepo)
	if err != nil {
		return fmt.Errorf("options_lifecycle_reconcile: list trades: %w", err)
	}
	result := execution.ReconcileOptionsLifecycle(orders, positions, trades)
	o.SetLastSummary("options_lifecycle_reconcile", map[string]int{"orders": result.OptionOrders, "positions": result.OptionPositions, "trades": result.OptionTrades, "leg_groups": result.LegGroups, "findings": len(result.Findings)})
	if !result.Healthy() {
		return fmt.Errorf("options_lifecycle_reconcile: %s", strings.Join(result.Findings, "; "))
	}
	return nil
}

func (o *JobOrchestrator) optionsExpirySettlement(ctx context.Context) error {
	now := time.Now().UTC()
	positions, err := listAllOpenPositions(ctx, o.deps.PositionRepo)
	if err != nil {
		return fmt.Errorf("options_expiry_settlement: list positions: %w", err)
	}
	underlyings := make(map[string]struct{})
	for _, position := range positions {
		if position.AssetClass == domain.AssetClassOption && position.Expiry != nil && !position.Expiry.After(now) && position.UnderlyingTicker != "" {
			underlyings[position.UnderlyingTicker] = struct{}{}
		}
	}
	prices := make(map[string]float64, len(underlyings))
	for underlying := range underlyings {
		bars, err := o.deps.DataService.GetOHLCV(ctx, domain.MarketTypeStock, underlying, data.Timeframe1d, now.Add(-7*24*time.Hour), now)
		if err != nil || len(bars) == 0 || bars[len(bars)-1].Close <= 0 {
			return fmt.Errorf("options_expiry_settlement: closing price unavailable for %s: %w", underlying, err)
		}
		prices[underlying] = bars[len(bars)-1].Close
	}
	summary, err := execution.SettleExpiredOptionPositions(ctx, positions, prices, now, o.deps.PositionRepo, o.deps.TradeRepo)
	if err != nil {
		return err
	}
	o.SetLastSummary("options_expiry_settlement", map[string]int{"expired_worthless": summary.ExpiredWorthless, "cash_settled": summary.CashSettled})
	return nil
}

func listAllOpenPositions(ctx context.Context, repo repository.PositionRepository) ([]domain.Position, error) {
	const pageSize = 250
	var all []domain.Position
	for offset := 0; ; offset += pageSize {
		page, err := repo.GetOpen(ctx, repository.PositionFilter{}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

func listAllPositions(ctx context.Context, repo repository.PositionRepository) ([]domain.Position, error) {
	const pageSize = 250
	var all []domain.Position
	for offset := 0; ; offset += pageSize {
		page, err := repo.List(ctx, repository.PositionFilter{}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

func listAllOptionOrders(ctx context.Context, repo repository.OrderRepository) ([]domain.Order, error) {
	const pageSize = 250
	var all []domain.Order
	for offset := 0; ; offset += pageSize {
		page, err := repo.List(ctx, repository.OrderFilter{MarketType: domain.MarketTypeOptions}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}

func listAllOptionTrades(ctx context.Context, repo repository.TradeRepository) ([]domain.Trade, error) {
	const pageSize = 250
	var all []domain.Trade
	for offset := 0; ; offset += pageSize {
		page, err := repo.List(ctx, repository.TradeFilter{}, pageSize, offset)
		if err != nil {
			return nil, err
		}
		all = append(all, page...)
		if len(page) < pageSize {
			return all, nil
		}
	}
}
