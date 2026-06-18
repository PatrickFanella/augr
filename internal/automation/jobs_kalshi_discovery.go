package automation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/PatrickFanella/get-rich-quick/internal/kalshidiscovery"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var kalshiDiscoverySpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "15 * * * *"}

var kalshiDiscoveryRun = kalshidiscovery.Run

func (o *JobOrchestrator) registerKalshiDiscoveryJob() {
	if o.deps.KalshiCatalog == nil || o.deps.StrategyRepo == nil || o.deps.KalshiWatchedRepo == nil || o.deps.KalshiMarketSnapshotsRepo == nil {
		return
	}
	o.Register("kalshi_discovery",
		"Auto-generate Kalshi paper strategies from open markets",
		kalshiDiscoverySpec, o.kalshiDiscovery)
}

func (o *JobOrchestrator) kalshiDiscovery(ctx context.Context) error {
	if o.deps.KalshiCatalog == nil {
		return fmt.Errorf("kalshi_discovery: catalog client not configured")
	}

	res, err := kalshiDiscoveryRun(ctx, kalshidiscovery.Config{
		DryRun:         false,
		FetchLimit:     50,
		MaxDeployments: 1,
		MinConviction:  0.70,
		Screener:       kalshidiscovery.DefaultScreenerConfig(),
	}, kalshidiscovery.Deps{
		Catalog:       o.deps.KalshiCatalog,
		Strategies:    o.deps.StrategyRepo,
		Watched:       o.deps.KalshiWatchedRepo,
		Snapshots:     o.deps.KalshiMarketSnapshotsRepo,
		DiscoveryRuns: o.deps.KalshiDiscoveryRuns,
		Logger:        o.logger,
	})
	if err != nil {
		return err
	}

	if res != nil {
		o.logger.Info("kalshi_discovery: run complete",
			slog.Int("fetched", res.FetchedAll),
			slog.Int("screened", res.Screened),
			slog.Int("proposed", res.Proposed),
			slog.Int("skipped", res.Skipped),
			slog.Int("deployed", len(res.Deployed)),
			slog.Bool("dry_run", res.DryRun),
		)
	}
	return nil
}
