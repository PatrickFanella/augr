package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	optdiscovery "github.com/PatrickFanella/get-rich-quick/internal/discovery/options"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func (o *JobOrchestrator) registerOvernightJobs() {
	o.Register("overnight_backtest", "Heavy 5-year backtests on promising candidates", overnightBacktestSpec, o.overnightBacktest, "history_refresh", "overnight_sweep")
	o.Register("overnight_sweep", "Parameter optimization on deployed strategies", overnightSweepSpec, o.overnightSweep, "history_refresh")
	o.Register("overnight_generate", "LLM generates new strategy ideas per index group", overnightGenerateSpec, o.overnightGenerate, "overnight_sweep", "overnight_backtest")
	o.Register("history_refresh", "Download latest OHLCV for all universe tickers", historyRefreshSpec, o.historyRefresh)
	o.Register("options_discovery", "Full options strategy discovery pipeline", optionsDiscoverySpec, o.optionsDiscovery, "overnight_generate")
}

var optionsDiscoverySpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "30 6 * * 2-6", SkipWeekends: false, SkipHolidays: false}

var (
	overnightBacktestSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "*/30 1-5 * * 2-6", SkipWeekends: false, SkipHolidays: false}
	overnightSweepSpec    = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "30 0 * * 2-6", SkipWeekends: false, SkipHolidays: false}
	overnightGenerateSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 6 * * 2-6", SkipWeekends: false, SkipHolidays: false}
	historyRefreshSpec    = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 0 * * 2-6", SkipWeekends: false, SkipHolidays: false}
)

const overnightBacktestWatchlistLimit = 20

var overnightIndexGroups = []string{"nasdaq", "nyse", "other"}

func (o *JobOrchestrator) overnightBacktest(ctx context.Context) error {
	o.logger.Info("overnight_backtest: chunk starting")
	chunker := newOvernightBacktestChunker(o.deps, o.logger)
	if err := chunker.RunChunk(ctx); err != nil {
		return fmt.Errorf("overnight_backtest: chunk failed: %w", err)
	}
	if o.deps.OvernightBacktestRuns != nil {
		runs, err := o.deps.OvernightBacktestRuns.ListLatest(ctx, 1)
		if err != nil {
			return fmt.Errorf("overnight_backtest: load chunk summary: %w", err)
		}
		if len(runs) > 0 {
			run := runs[0]
			o.SetLastSummary("overnight_backtest", map[string]int{
				"candidates":      run.Summary.Candidates,
				"generated":       run.Summary.Generated,
				"swept":           run.Summary.Swept,
				"validated":       run.Summary.Validated,
				"deployed":        run.Summary.Deployed,
				"created":         run.Summary.Created,
				"reused":          run.Summary.Reused,
				"errors":          len(run.Errors),
				"candidate_index": run.CandidateIndex,
			})
		}
	}
	o.logger.Info("overnight_backtest: chunk completed")
	return nil
}

func optionsDiscoveryCompletionError(errors []string) error {
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("options_discovery: completed with %d pipeline errors", len(errors))
}

// overnightSweep runs a heavy parameter sweep (50 variants) on all
// active strategies, logging recommendations when significant
// improvement is found.
func (o *JobOrchestrator) overnightSweep(ctx context.Context) error {
	o.logger.Info("overnight_sweep: starting")
	if o.deps.StrategyRepo == nil || o.deps.DataService == nil {
		return fmt.Errorf("overnight_sweep: strategy repository and data service are required")
	}

	ctx, cancel := context.WithTimeout(ctx, 3*time.Hour)
	defer cancel()

	strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{Status: domain.StrategyStatusActive, MarketType: domain.MarketTypeStock})
	if err != nil {
		return fmt.Errorf("overnight_sweep: list strategies: %w", err)
	}

	scoring := discovery.DefaultScoringConfig()
	now := time.Now()
	histFrom := now.AddDate(-1, 0, 0)

	var improved, total, swept, skipped, failed, insufficient, stale int
	defer func() {
		o.SetLastSummary("overnight_sweep", map[string]int{"strategies": total, "swept": swept, "improved": improved, "skipped": skipped, "failed": failed, "insufficient": insufficient, "stale": stale})
	}()
	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		total++

		rulesConfig, err := extractRulesConfig(strat.Config)
		if err != nil {
			failed++
			o.logger.Warn("overnight_sweep: bad config",
				slog.String("strategy", strat.Name),
				slog.Any("error", err),
			)
			continue
		}

		barsMap, err := o.deps.DataService.DownloadHistoricalOHLCV(
			ctx, strat.MarketType,
			[]string{strat.Ticker},
			data.Timeframe1d, histFrom, now, true,
		)
		if err != nil {
			failed++
			o.logger.Warn("overnight_sweep: download failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		bars := barsMap[strat.Ticker]
		if len(bars) < 50 {
			failed++
			insufficient++
			continue
		}
		if !dailyBarFresh(now, bars[len(bars)-1].Timestamp) {
			failed++
			stale++
			continue
		}

		sweepCfg := discovery.SweepConfig{
			Ticker:      strat.Ticker,
			MarketType:  strat.MarketType,
			Bars:        bars,
			StartDate:   bars[0].Timestamp,
			EndDate:     bars[len(bars)-1].Timestamp,
			InitialCash: 100_000,
			Variations:  50,
		}

		results, err := discovery.RunSweep(ctx, *rulesConfig, sweepCfg, scoring, o.logger)
		if err != nil {
			failed++
			o.logger.Warn("overnight_sweep: sweep failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		if len(results) == 0 {
			failed++
			continue
		}
		swept++

		var currentScore float64
		for _, r := range results {
			if r.Label == "base" {
				currentScore = r.Score
				break
			}
		}

		best := results[0]
		if currentScore > 0 && best.Score > currentScore*1.30 {
			improved++
			o.logger.Info("overnight_sweep: recommendation",
				slog.String("ticker", strat.Ticker),
				slog.String("strategy", strat.Name),
				slog.String("best_variant", best.Label),
				slog.Float64("current_score", currentScore),
				slog.Float64("best_score", best.Score),
				slog.Float64("improvement_pct", (best.Score-currentScore)/currentScore*100),
			)
		}
	}

	o.logger.Info("overnight_sweep: completed",
		slog.Int("strategies", total),
		slog.Int("improved", improved),
	)
	return overnightSweepCompletionError(failed)
}

func overnightSweepCompletionError(failed int) error {
	if failed <= 0 {
		return nil
	}
	return fmt.Errorf("overnight_sweep: %d strategies failed", failed)
}

// overnightGenerate uses the LLM to generate new strategy ideas for each
// distinct index group represented by the universe schema.
func (o *JobOrchestrator) overnightGenerate(ctx context.Context) error {
	o.logger.Info("overnight_generate: starting")

	if o.deps.Universe == nil {
		return fmt.Errorf("overnight_generate: universe not configured")
	}

	indexGroups := overnightIndexGroups
	summary := map[string]int{"groups": len(indexGroups), "groups_attempted": 0, "candidates": 0, "proposed": 0, "created": 0, "reused": 0, "deployed": 0, "pipeline_errors": 0, "errors": 0}
	defer func() { o.SetLastSummary("overnight_generate", summary) }()

	deps := discovery.DiscoveryDeps{
		DataService:     o.deps.DataService,
		LLMProvider:     o.deps.LLMProvider,
		Strategies:      o.deps.StrategyRepo,
		BacktestConfigs: o.deps.BacktestConfigRepo,
		Logger:          o.logger,
	}

	for _, indexGroup := range indexGroups {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		summary["groups_attempted"]++
		tickers, err := o.deps.Universe.GetActiveTickers(ctx, indexGroup, 5)
		if err != nil {
			summary["errors"]++
			o.logger.Warn("overnight_generate: failed to get tickers",
				slog.String("index_group", indexGroup),
				slog.Any("error", err),
			)
			continue
		}

		if len(tickers) == 0 {
			summary["errors"]++
			o.logger.Warn("overnight_generate: no tickers for index group",
				slog.String("index_group", indexGroup),
			)
			continue
		}

		cfg := discovery.DiscoveryConfig{
			Screener: discovery.ScreenerConfig{
				Tickers:    tickers,
				MarketType: domain.MarketTypeStock,
			},
			Generator:  discovery.GeneratorConfig{Model: o.deps.LLMQuickModel},
			MaxWinners: 2,
		}

		result, err := discovery.RunDiscovery(ctx, cfg, deps)
		if err != nil {
			summary["errors"]++
			o.logger.Warn("overnight_generate: discovery failed",
				slog.String("index_group", indexGroup),
				slog.Any("error", err),
			)
			continue
		}
		if len(result.Errors) > 0 {
			summary["errors"]++
			summary["pipeline_errors"] += len(result.Errors)
			o.logger.Warn("overnight_generate: discovery returned partial errors",
				slog.String("index_group", indexGroup),
				slog.Int("errors", len(result.Errors)),
			)
		}

		summary["candidates"] += result.Candidates
		summary["deployed"] += result.Deployed
		summary["proposed"] += result.Proposed
		summary["created"] += result.Created
		summary["reused"] += result.Reused
		o.logger.Info(fmt.Sprintf("overnight_generate: %s — %d candidates, %d proposed, %d created, %d reused",
			indexGroup, result.Candidates, result.Proposed, result.Created, result.Reused),
		)
	}

	o.logger.Info("overnight_generate: completed")
	return overnightGenerateCompletionError(summary["errors"])
}

func overnightGenerateCompletionError(errors int) error {
	if errors <= 0 {
		return nil
	}
	return fmt.Errorf("overnight_generate: %d index groups failed", errors)
}

// historyRefresh downloads latest OHLCV for all active tickers in
// the universe, batching 10 at a time with a 1-second pause for rate
// limiting.
func (o *JobOrchestrator) historyRefresh(ctx context.Context) error {
	summary := map[string]int{"tickers": 0, "updated": 0, "cache_revalidated": 0, "provider_requests": 0, "fresh_bars": 0, "empty": 0, "stale": 0, "failed": 0, "batches": 0}
	defer func() { o.SetLastSummary("history_refresh", summary) }()
	o.logger.Info("history_refresh: starting")

	if o.deps.Universe == nil {
		return fmt.Errorf("history_refresh: universe not configured")
	}
	if o.deps.DataService == nil {
		return fmt.Errorf("history_refresh: data service not configured")
	}

	allTickers, err := o.deps.Universe.GetActiveTickers(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("history_refresh: get active tickers: %w", err)
	}

	now := time.Now()
	summary["tickers"] = len(allTickers)
	if len(allTickers) == 0 {
		return fmt.Errorf("history_refresh: active universe empty")
	}
	histFrom := now.AddDate(-5, 0, 0)
	batchSize := 10
	processed := 0

	for i := 0; i < len(allTickers); i += batchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		end := i + batchSize
		if end > len(allTickers) {
			end = len(allTickers)
		}
		batch := allTickers[i:end]
		summary["batches"]++

		download, err := o.deps.DataService.DownloadHistoricalOHLCVWithStats(
			ctx, domain.MarketTypeStock,
			batch, data.Timeframe1d,
			histFrom, now, true,
		)
		if err != nil {
			summary["failed"] += len(batch)
			o.logger.Warn("history_refresh: batch download failed",
				slog.Int("batch_start", i),
				slog.Any("error", err),
			)
		} else {
			cacheOnly := make([]string, 0, len(batch))
			for _, ticker := range batch {
				if download.ProviderRequests[ticker] == 0 {
					cacheOnly = append(cacheOnly, ticker)
				}
			}
			revalidationFailed := make(map[string]struct{}, len(cacheOnly))
			if len(cacheOnly) > 0 {
				trailingFrom := now.AddDate(0, 0, -10)
				revalidated, refreshErr := o.deps.DataService.DownloadHistoricalOHLCVWithStats(
					ctx, domain.MarketTypeStock,
					cacheOnly, data.Timeframe1d,
					trailingFrom, now, false,
				)
				if refreshErr != nil {
					summary["failed"] += len(cacheOnly)
					for _, ticker := range cacheOnly {
						revalidationFailed[ticker] = struct{}{}
					}
					o.logger.Warn("history_refresh: cache revalidation failed",
						slog.Int("batch_start", i),
						slog.Int("tickers", len(cacheOnly)),
						slog.Any("error", refreshErr),
					)
				} else {
					for _, ticker := range cacheOnly {
						download.ProviderRequests[ticker] += revalidated.ProviderRequests[ticker]
						download.FreshBars[ticker] += revalidated.FreshBars[ticker]
						if len(revalidated.Bars[ticker]) > 0 {
							download.Bars[ticker] = revalidated.Bars[ticker]
						}
						if revalidated.FreshBars[ticker] > 0 {
							summary["cache_revalidated"]++
						}
					}
				}
			}

			for _, ticker := range batch {
				if _, failed := revalidationFailed[ticker]; failed {
					continue
				}
				summary["provider_requests"] += download.ProviderRequests[ticker]
				summary["fresh_bars"] += download.FreshBars[ticker]
				if download.ProviderRequests[ticker] == 0 {
					summary["failed"]++
					continue
				}
				if download.FreshBars[ticker] == 0 {
					summary["empty"]++
					continue
				}
				bars := download.Bars[ticker]
				if len(bars) == 0 {
					summary["empty"]++
					continue
				}
				if !dailyBarFresh(now, bars[len(bars)-1].Timestamp) {
					summary["stale"]++
					continue
				}
				summary["updated"]++
			}
		}

		processed += len(batch)
		o.logger.Info(fmt.Sprintf("history_refresh: %d/%d tickers processed", processed, len(allTickers)))

		// Rate limit pause between batches.
		if end < len(allTickers) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}
	}

	o.logger.Info("history_refresh: completed",
		slog.Int("tickers", summary["tickers"]),
		slog.Int("updated", summary["updated"]),
		slog.Int("cache_revalidated", summary["cache_revalidated"]),
		slog.Int("empty", summary["empty"]),
		slog.Int("failed", summary["failed"]),
	)
	return historyRefreshCompletionError(summary)
}

func historyRefreshCompletionError(summary map[string]int) error {
	if summary["failed"] == 0 && summary["empty"] == 0 && summary["stale"] == 0 {
		return nil
	}
	return fmt.Errorf("history_refresh: incomplete coverage: failed=%d empty=%d stale=%d tickers=%d",
		summary["failed"], summary["empty"], summary["stale"], summary["tickers"])
}

// optionsDiscovery runs the full options strategy discovery pipeline.
func (o *JobOrchestrator) optionsDiscovery(ctx context.Context) error {
	o.logger.Info("options_discovery: starting")
	summary := map[string]int{"candidates": 0, "scored": 0, "generated": 0, "swept": 0, "validated": 0, "deployed": 0, "proposed": 0, "created": 0, "reused": 0, "errors": 0, "winners": 0}
	defer func() { o.SetLastSummary("options_discovery", summary) }()

	if o.deps.OptionsProvider == nil {
		return fmt.Errorf("options_discovery: options provider not configured")
	}
	if o.deps.Universe == nil {
		return fmt.Errorf("options_discovery: universe not configured")
	}
	if o.deps.LLMProvider == nil {
		return fmt.Errorf("options_discovery: LLM provider not configured")
	}
	if o.deps.DataService == nil || o.deps.StrategyRepo == nil {
		return fmt.Errorf("options_discovery: data service and strategy repository are required")
	}
	if o.deps.DiscoveryRunRepo == nil {
		return fmt.Errorf("options_discovery: discovery run repository is required")
	}

	// Get tradeable watchlist candidates.
	watchlist, err := tradeableWatchlistTickers(ctx, o.logger, o.deps.Universe, o.deps.DataService, 500, 100)
	if err != nil {
		return fmt.Errorf("options_discovery: get watchlist: %w", err)
	}
	if len(watchlist) == 0 {
		return fmt.Errorf("options_discovery: no tradeable watchlist tickers")
	}
	tickers := make([]string, len(watchlist))
	for i, t := range watchlist {
		tickers[i] = t.Ticker
	}

	cfg := optdiscovery.OptionsDiscoveryConfig{
		Screener: optdiscovery.OptionsScreenerConfig{
			Tickers: tickers,
		},
		Scoring:     optdiscovery.DefaultOptionsScoringConfig(),
		Generator:   discovery.GeneratorConfig{Provider: o.deps.LLMProvider, Model: o.deps.LLMQuickModel, Metrics: o.deps.GeneratorMetrics},
		BacktestCfg: discovery.DefaultScoringConfig(),
		MaxWinners:  3,
	}

	deps := optdiscovery.OptionsDiscoveryDeps{
		DataService:     o.deps.DataService,
		OptionsProvider: o.deps.OptionsProvider,
		Strategies:      o.deps.StrategyRepo,
		Logger:          o.logger,
	}

	startedAt := time.Now().UTC()
	result, err := optdiscovery.RunOptionsDiscovery(ctx, cfg, deps)
	if err != nil {
		return fmt.Errorf("options_discovery: %w", err)
	}
	if err := optdiscovery.PersistRun(ctx, o.deps.DiscoveryRunRepo, cfg, result, startedAt); err != nil {
		return fmt.Errorf("options_discovery: %w", err)
	}

	summary["candidates"] = result.Candidates
	summary["scored"] = result.Scored
	summary["generated"] = result.Generated
	summary["swept"] = result.Swept
	summary["validated"] = result.Validated
	summary["deployed"] = result.Deployed
	summary["proposed"] = result.Proposed
	summary["created"] = result.Created
	summary["reused"] = result.Reused
	summary["errors"] = len(result.Errors)
	summary["winners"] = len(result.Winners)

	o.logger.Info("options_discovery: complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("scored", result.Scored),
		slog.Int("generated", result.Generated),
		slog.Int("swept", result.Swept),
		slog.Int("validated", result.Validated),
		slog.Int("deployed", result.Deployed),
		slog.Int("proposed", result.Proposed),
		slog.Int("created", result.Created),
		slog.Int("reused", result.Reused),
		slog.Int("errors", len(result.Errors)),
		slog.Duration("duration", result.Duration),
	)

	for _, w := range result.Winners {
		o.logger.Info("options_discovery: winner selected",
			slog.String("id", w.StrategyID.String()),
			slog.String("ticker", w.Ticker),
			slog.String("type", string(w.Config.StrategyType)),
			slog.Float64("score", w.Score),
		)
	}

	return optionsDiscoveryCompletionError(result.Errors)
}
