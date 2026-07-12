package automation

import (
	"context"
	"fmt"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var optionsExpirySpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "0 23 * * 1-5", SkipWeekends: true, SkipHolidays: false}

func (o *JobOrchestrator) registerOptionsLifecycleJobs() {
	if o.deps.PositionRepo == nil || o.deps.TradeRepo == nil || o.deps.DataService == nil {
		return
	}
	o.Register("options_expiry_settlement", "Cash-settle expired paper option positions", optionsExpirySpec, o.optionsExpirySettlement)
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
