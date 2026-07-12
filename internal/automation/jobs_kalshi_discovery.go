package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
		// Kalshi's public catalog is shared by multiple jobs and installations.
		// A provider throttle should preserve the last good catalog and allow the
		// next hourly run to retry, rather than tripping the orchestrator's
		// permanent auto-disable threshold.
		if isKalshiRateLimit(err) {
			o.logger.Warn("kalshi_discovery: provider rate limited; retaining current catalog")
			return nil
		}
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

func isKalshiRateLimit(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "status=429") || strings.Contains(message, "too_many_requests")
}
