package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var kalshiSettlementSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "15 * * * *"}

func (o *JobOrchestrator) registerKalshiSettlementJob() {
	if o.deps.KalshiCatalog == nil || o.deps.PredictionSettler == nil {
		return
	}
	o.Register("kalshi_settlement", "Cash-settle resolved Kalshi paper contracts and journal outcomes", kalshiSettlementSpec, o.kalshiSettlement)
}

func (o *JobOrchestrator) kalshiSettlement(ctx context.Context) error {
	var fetched, resolved, settled int
	cursor := ""
	for {
		markets, next, err := o.deps.KalshiCatalog.ListMarkets(ctx, kalshidiscovery.ListOptions{Limit: 100, Cursor: cursor, Status: "settled"})
		if err != nil {
			return fmt.Errorf("kalshi_settlement: list settled markets: %w", err)
		}
		fetched += len(markets)
		for _, market := range markets {
			winner := strings.ToUpper(strings.TrimSpace(market.Result))
			if winner != "YES" && winner != "NO" {
				continue
			}
			resolved++
			resolvedAt := time.Now().UTC()
			if market.CloseTime != nil && !market.CloseTime.IsZero() {
				resolvedAt = market.CloseTime.UTC()
			}
			count, err := o.deps.PredictionSettler.SettleMarket(ctx, domain.MarketTypeKalshi, market.Ticker, winner, resolvedAt)
			if err != nil {
				return fmt.Errorf("kalshi_settlement: settle %s: %w", market.Ticker, err)
			}
			settled += count
		}
		if strings.TrimSpace(next) == "" {
			break
		}
		cursor = next
	}
	o.SetLastSummary("kalshi_settlement", map[string]int{"fetched": fetched, "resolved": resolved, "settled": settled})
	return nil
}
