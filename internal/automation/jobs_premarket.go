package automation

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

// Schedule specs for pre-market jobs (all times Eastern via orchestrator cron).
// Pre-market data available ~4 AM ET; market opens 9:30 AM ET.
var (
	gapScannerSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypePreMarket,
		Cron:         "0 8 * * 1-5", // 8:00 AM ET
		SkipWeekends: true,
		SkipHolidays: true,
	}
	discoveryRunSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypePreMarket,
		Cron:         "30 8 * * 1-5", // 8:30 AM ET
		SkipWeekends: true,
		SkipHolidays: true,
	}
	positionReviewSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypePreMarket,
		Cron:         "0 9 * * 1-5", // 9:00 AM ET — 30 min before open
		SkipWeekends: true,
		SkipHolidays: true,
	}
)

func (o *JobOrchestrator) registerPreMarketJobs() {
	o.Register("gap_scanner", "Detect overnight gaps and unusual volume", gapScannerSpec, o.gapScanner)
	o.Register("discovery_run", "Full strategy discovery on top watchlist tickers", discoveryRunSpec, o.discoveryRun, "gap_scanner")
	o.Register("position_review", "Review open positions before market open", positionReviewSpec, o.positionReview)
}

// gapScanner detects overnight gaps and unusual volume in the top 500 tickers.
func (o *JobOrchestrator) gapScanner(ctx context.Context) error {
	tickers, err := o.deps.Universe.GetWatchlist(ctx, 500)
	if err != nil {
		return fmt.Errorf("gap_scanner: get watchlist: %w", err)
	}
	if len(tickers) == 0 {
		o.logger.Info("gap_scanner: watchlist empty")
		return nil
	}

	symbols := make([]string, len(tickers))
	for i, t := range tickers {
		symbols[i] = t.Ticker
	}

	// Batch snapshot 100 at a time.
	const batchSize = 100

	type gapStock struct {
		ticker   string
		gapPct   float64
		volRatio float64
	}
	var gaps []gapStock

	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]

		snapshots, snapErr := o.deps.Polygon.BulkSnapshot(ctx, batch)
		if snapErr != nil {
			o.logger.Warn("gap_scanner: snapshot batch failed",
				slog.Int("offset", i),
				slog.Any("error", snapErr),
			)
			continue
		}

		for _, snap := range snapshots {
			// Calculate gap percentage: (today open - prev close) / prev close.
			gapPct := 0.0
			if snap.PrevDay.Close > 0 {
				gapPct = (snap.Day.Open - snap.PrevDay.Close) / snap.PrevDay.Close * 100
			}

			// Calculate volume ratio.
			volRatio := 0.0
			if snap.PrevDay.Volume > 0 {
				volRatio = snap.Day.Volume / snap.PrevDay.Volume
			}

			// Filter: |gap| > 2% or volume ratio > 3x.
			if math.Abs(gapPct) > 2.0 || volRatio > 3.0 {
				gaps = append(gaps, gapStock{
					ticker:   snap.Ticker,
					gapPct:   gapPct,
					volRatio: volRatio,
				})

				// Bonus score for gap stocks.
				bonus := math.Abs(gapPct)*0.5 + math.Max(0, volRatio-1)*0.3
				baseScore := scoreFromSnapshot(snap.TodaysChangePct, snap.Day.Volume, snap.PrevDay.Volume, snap.Day.Close) * universe.IndexBoost(snap.Ticker)
				if err := o.deps.Universe.UpdateScore(ctx, snap.Ticker, baseScore+bonus); err != nil {
					o.logger.Warn("gap_scanner: update score failed",
						slog.String("ticker", snap.Ticker),
						slog.Any("error", err),
					)
				}
			}
		}
	}

	// Log gap stocks.
	for _, g := range gaps {
		o.logger.Info("gap_scanner: gap detected",
			slog.String("ticker", g.ticker),
			slog.Float64("gap_pct", g.gapPct),
			slog.Float64("vol_ratio", g.volRatio),
		)
	}

	o.logger.Info("gap_scanner: complete",
		slog.Int("scanned", len(symbols)),
		slog.Int("gaps_found", len(gaps)),
	)
	return nil
}

// discoveryRun runs the full strategy discovery pipeline on top watchlist tickers.
func (o *JobOrchestrator) discoveryRun(ctx context.Context) error {
	tickers, err := o.deps.Universe.GetWatchlist(ctx, 30)
	if err != nil {
		return fmt.Errorf("discovery_run: get watchlist: %w", err)
	}
	if len(tickers) == 0 {
		o.logger.Info("discovery_run: watchlist empty, skipping")
		return nil
	}

	symbols := make([]string, len(tickers))
	for i, t := range tickers {
		symbols[i] = t.Ticker
	}

	cfg := discovery.DiscoveryConfig{
		Screener: discovery.ScreenerConfig{
			Tickers:    symbols,
			MarketType: domain.MarketTypeStock,
		},
		Generator: discovery.GeneratorConfig{
			Provider: o.deps.LLMProvider,
		},
		Scoring:    discovery.DefaultScoringConfig(),
		MaxWinners: 3,
	}

	deps := discovery.DiscoveryDeps{
		DataService: o.deps.DataService,
		LLMProvider: o.deps.LLMProvider,
		Strategies:  o.deps.StrategyRepo,
		Logger:      o.logger,
	}

	result, err := discovery.RunDiscovery(ctx, cfg, deps)
	if err != nil {
		return fmt.Errorf("discovery_run: %w", err)
	}

	o.logger.Info("discovery_run: complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("generated", result.Generated),
		slog.Int("swept", result.Swept),
		slog.Int("validated", result.Validated),
		slog.Int("deployed", result.Deployed),
		slog.Duration("duration", result.Duration),
	)

	for _, w := range result.Winners {
		o.logger.Info("discovery_run: winner deployed",
			slog.String("strategy_id", w.StrategyID.String()),
			slog.String("ticker", w.Ticker),
			slog.Float64("score", w.Score),
		)
	}

	return nil
}

// positionReview reviews all active strategies and their open positions before market open.
func (o *JobOrchestrator) positionReview(ctx context.Context) error {
	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{
		Status: domain.StrategyStatusActive,
	}, 0, 0)
	if err != nil {
		return fmt.Errorf("position_review: list strategies: %w", err)
	}

	if len(strategies) == 0 {
		o.logger.Info("position_review: no active strategies")
		return nil
	}

	var withPositions int
	for _, s := range strategies {
		// For now, log each active strategy. Full position checking will be
		// wired once the PositionRepository is exposed through deps.
		o.logger.Info("position_review: active strategy",
			slog.String("id", s.ID.String()),
			slog.String("ticker", s.Ticker),
			slog.String("name", s.Name),
			slog.Bool("is_paper", s.IsPaper),
		)
		withPositions++
	}

	o.logger.Info("position_review: complete",
		slog.Int("active_strategies", len(strategies)),
		slog.Int("with_positions", withPositions),
	)
	return nil
}
