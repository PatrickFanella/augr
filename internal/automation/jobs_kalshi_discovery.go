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
	summary := map[string]int{"fetched": 0, "screened": 0, "proposed": 0, "skipped": 0, "deployed": 0, "created": 0, "reused": 0, "errors": 0, "dry_run": 0}
	defer func() { o.SetLastSummary("kalshi_discovery", summary) }()
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
		if isKalshiRateLimit(err) {
			o.logger.Warn("kalshi_discovery: provider rate limited; retaining current catalog")
			return fmt.Errorf("kalshi_discovery: provider rate limited: %w", err)
		}
		return err
	}

	if res != nil {
		summary["fetched"] = res.FetchedAll
		summary["screened"] = res.Screened
		summary["proposed"] = res.Proposed
		summary["skipped"] = res.Skipped
		summary["deployed"] = len(res.Deployed)
		for _, deployed := range res.Deployed {
			if deployed.Reused {
				summary["reused"]++
			} else {
				summary["created"]++
			}
			o.logger.Info("kalshi_discovery: strategy selected",
				slog.String("strategy_id", deployed.StrategyID.String()),
				slog.String("ticker", deployed.Ticker),
				slog.String("direction", deployed.Direction),
				slog.Float64("conviction", deployed.Conviction),
				slog.Bool("reused", deployed.Reused),
			)
		}
		summary["errors"] = len(res.Errors)
		if res.DryRun {
			summary["dry_run"] = 1
		}
		o.logger.Info("kalshi_discovery: run complete",
			slog.Int("fetched", res.FetchedAll),
			slog.Int("screened", res.Screened),
			slog.Int("proposed", res.Proposed),
			slog.Int("skipped", res.Skipped),
			slog.Int("deployed", len(res.Deployed)),
			slog.Int("created", summary["created"]),
			slog.Int("reused", summary["reused"]),
			slog.Bool("dry_run", res.DryRun),
		)
	}
	return kalshiDiscoveryCompletionError(res != nil, summary["errors"])
}

func kalshiDiscoveryCompletionError(resultPresent bool, errors int) error {
	if !resultPresent {
		return fmt.Errorf("kalshi_discovery: runner returned no result")
	}
	if errors > 0 {
		return fmt.Errorf("kalshi_discovery: completed with %d domain errors", errors)
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
