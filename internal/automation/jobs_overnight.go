package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func (o *JobOrchestrator) registerOvernightJobs() {
	o.Register("overnight_backtest", "Heavy 5-year backtests on promising candidates", overnightBacktestSpec, o.overnightBacktest, "history_refresh")
	o.Register("overnight_sweep", "Parameter optimization on deployed strategies", overnightSweepSpec, o.overnightSweep, "overnight_backtest")
	o.Register("overnight_generate", "LLM generates new strategy ideas per sector", overnightGenerateSpec, o.overnightGenerate, "overnight_sweep")
	o.Register("history_refresh", "Download latest OHLCV for all universe tickers", historyRefreshSpec, o.historyRefresh)
}

var overnightBacktestSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 1 * * 2-6", SkipWeekends: false, SkipHolidays: false}
var overnightSweepSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 2 * * 2-6", SkipWeekends: false, SkipHolidays: false}
var overnightGenerateSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 3 * * 2-6", SkipWeekends: false, SkipHolidays: false}
var historyRefreshSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeCron, Cron: "0 4 * * 2-6", SkipWeekends: false, SkipHolidays: false}

// overnightBacktest runs the full discovery pipeline on the top 50
// watchlist tickers with 5 years of history.
func (o *JobOrchestrator) overnightBacktest(ctx context.Context) error {
	o.logger.Info("overnight_backtest: starting")

	ctx, cancel := context.WithTimeout(ctx, 4*time.Hour)
	defer cancel()

	if o.deps.Universe == nil {
		o.logger.Info("overnight_backtest: skipped — Universe not configured")
		return nil
	}

	watchlist, err := o.deps.Universe.GetWatchlist(ctx, 50)
	if err != nil {
		return fmt.Errorf("overnight_backtest: get watchlist: %w", err)
	}

	tickers := make([]string, len(watchlist))
	for i, t := range watchlist {
		tickers[i] = t.Ticker
	}

	// Download 5 years of history for all tickers.
	now := time.Now()
	histFrom := now.AddDate(-5, 0, 0)
	_, err = o.deps.DataService.DownloadHistoricalOHLCV(
		ctx, domain.MarketTypeStock,
		tickers, data.Timeframe1d,
		histFrom, now, true,
	)
	if err != nil {
		o.logger.Warn("overnight_backtest: history download had errors", slog.Any("error", err))
		// Continue — partial data is fine.
	}

	// Run full discovery pipeline.
	cfg := discovery.DiscoveryConfig{
		Screener: discovery.ScreenerConfig{
			Tickers:    tickers,
			MarketType: domain.MarketTypeStock,
		},
		MaxWinners: 5,
	}
	deps := discovery.DiscoveryDeps{
		DataService: o.deps.DataService,
		LLMProvider: o.deps.LLMProvider,
		Strategies:  o.deps.StrategyRepo,
		Logger:      o.logger,
	}

	result, err := discovery.RunDiscovery(ctx, cfg, deps)
	if err != nil {
		return fmt.Errorf("overnight_backtest: discovery failed: %w", err)
	}

	o.logger.Info("overnight_backtest: completed",
		slog.Int("candidates", result.Candidates),
		slog.Int("deployed", result.Deployed),
		slog.Duration("duration", result.Duration),
	)

	for _, w := range result.Winners {
		o.logger.Info("overnight_backtest: top scorer",
			slog.String("ticker", w.Ticker),
			slog.Float64("score", w.Score),
			slog.Float64("sharpe", w.InSample.SharpeRatio),
		)
	}

	return nil
}

// overnightSweep runs a heavy parameter sweep (50 variants) on all
// active strategies, logging recommendations when significant
// improvement is found.
func (o *JobOrchestrator) overnightSweep(ctx context.Context) error {
	o.logger.Info("overnight_sweep: starting")

	ctx, cancel := context.WithTimeout(ctx, 3*time.Hour)
	defer cancel()

	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{Status: "active"}, 100, 0)
	if err != nil {
		return fmt.Errorf("overnight_sweep: list strategies: %w", err)
	}

	scoring := discovery.DefaultScoringConfig()
	now := time.Now()
	histFrom := now.AddDate(-1, 0, 0)

	var improved, total int
	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		total++

		rulesConfig, err := extractRulesConfig(strat.Config)
		if err != nil {
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
			o.logger.Warn("overnight_sweep: download failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		bars := barsMap[strat.Ticker]
		if len(bars) < 50 {
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
			o.logger.Warn("overnight_sweep: sweep failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		if len(results) == 0 {
			continue
		}

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
	return nil
}

// overnightGenerate uses the LLM to generate new strategy ideas for
// each sector, running discovery on a per-sector ticker subset.
func (o *JobOrchestrator) overnightGenerate(ctx context.Context) error {
	o.logger.Info("overnight_generate: starting")

	if o.deps.Universe == nil {
		o.logger.Info("overnight_generate: skipped — Universe not configured")
		return nil
	}

	sectors := []struct {
		name       string
		indexGroup string
	}{
		{name: "tech", indexGroup: "nasdaq"},
		{name: "finance", indexGroup: "nyse"},
		{name: "healthcare", indexGroup: "nyse"},
		{name: "energy", indexGroup: "nyse"},
		{name: "consumer", indexGroup: "nasdaq"},
	}

	deps := discovery.DiscoveryDeps{
		DataService: o.deps.DataService,
		LLMProvider: o.deps.LLMProvider,
		Strategies:  o.deps.StrategyRepo,
		Logger:      o.logger,
	}

	for _, sector := range sectors {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		tickers, err := o.deps.Universe.GetActiveTickers(ctx, sector.indexGroup, 5)
		if err != nil {
			o.logger.Warn("overnight_generate: failed to get tickers",
				slog.String("sector", sector.name),
				slog.Any("error", err),
			)
			continue
		}

		if len(tickers) == 0 {
			o.logger.Info("overnight_generate: no tickers for sector",
				slog.String("sector", sector.name),
			)
			continue
		}

		cfg := discovery.DiscoveryConfig{
			Screener: discovery.ScreenerConfig{
				Tickers:    tickers,
				MarketType: domain.MarketTypeStock,
			},
			MaxWinners: 2,
		}

		result, err := discovery.RunDiscovery(ctx, cfg, deps)
		if err != nil {
			o.logger.Warn("overnight_generate: discovery failed",
				slog.String("sector", sector.name),
				slog.Any("error", err),
			)
			continue
		}

		o.logger.Info(fmt.Sprintf("overnight_generate: %s — %d candidates, %d deployed",
			sector.name, result.Candidates, result.Deployed),
		)
	}

	o.logger.Info("overnight_generate: completed")
	return nil
}

// historyRefresh downloads latest OHLCV for all active tickers in
// the universe, batching 10 at a time with a 1-second pause for rate
// limiting.
func (o *JobOrchestrator) historyRefresh(ctx context.Context) error {
	o.logger.Info("history_refresh: starting")

	if o.deps.Universe == nil {
		o.logger.Info("history_refresh: skipped — Universe not configured")
		return nil
	}

	allTickers, err := o.deps.Universe.GetActiveTickers(ctx, "", 5000)
	if err != nil {
		return fmt.Errorf("history_refresh: get active tickers: %w", err)
	}

	now := time.Now()
	histFrom := now.AddDate(-5, 0, 0)
	batchSize := 10
	updated := 0

	for i := 0; i < len(allTickers); i += batchSize {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		end := i + batchSize
		if end > len(allTickers) {
			end = len(allTickers)
		}
		batch := allTickers[i:end]

		_, err := o.deps.DataService.DownloadHistoricalOHLCV(
			ctx, domain.MarketTypeStock,
			batch, data.Timeframe1d,
			histFrom, now, true,
		)
		if err != nil {
			o.logger.Warn("history_refresh: batch download failed",
				slog.Int("batch_start", i),
				slog.Any("error", err),
			)
			// Continue with next batch.
		}

		updated += len(batch)
		o.logger.Info(fmt.Sprintf("history_refresh: %d/%d tickers updated", updated, len(allTickers)))

		// Rate limit pause between batches.
		if end < len(allTickers) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}
	}

	o.logger.Info("history_refresh: completed", slog.Int("tickers", updated))
	return nil
}
