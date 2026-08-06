package automation

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/google/uuid"

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
	summary := map[string]int{"requested": 0, "snapshot_batches": 0, "failed_batches": 0, "snapshots": 0, "missing_snapshots": 0, "stale_snapshots": 0, "gaps": 0, "score_failed": 0, "trigger_requests": 0, "strategy_list_failed": 0}
	defer func() { o.SetLastSummary("gap_scanner", summary) }()
	if o.deps.Universe == nil || o.deps.Polygon == nil {
		return fmt.Errorf("gap_scanner: universe and Polygon providers are required")
	}

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
	summary["requested"] = len(symbols)

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
		summary["snapshot_batches"]++

		snapshots, snapErr := o.deps.Polygon.BulkSnapshot(ctx, batch)
		if snapErr != nil {
			summary["failed_batches"]++
			o.logger.Warn("gap_scanner: snapshot batch failed",
				slog.Int("offset", i),
				slog.Any("error", snapErr),
			)
			continue
		}
		summary["snapshots"] += len(snapshots)
		if len(snapshots) < len(batch) {
			summary["missing_snapshots"] += len(batch) - len(snapshots)
		}

		for _, snap := range snapshots {
			if !preMarketSnapshotFresh(time.Now(), snap.UpdatedAt()) {
				summary["stale_snapshots"]++
				continue
			}
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
					summary["score_failed"]++
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

	// Trigger active strategies for tickers with detected gaps.
	if o.deps.StrategyTrigger != nil && len(gaps) > 0 {
		gapTickers := make(map[string]struct{}, len(gaps))
		for _, g := range gaps {
			gapTickers[g.ticker] = struct{}{}
		}
		strategies, listErr := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{
			Status: domain.StrategyStatusActive,
		})
		if listErr == nil {
			for _, s := range canonicalTriggeredStrategies(strategies) {
				if _, ok := gapTickers[s.Ticker]; ok {
					o.logger.Info("gap_scanner: requesting strategy trigger for gap ticker",
						slog.String("ticker", s.Ticker),
						slog.String("strategy_id", s.ID.String()),
					)
					o.deps.StrategyTrigger.TriggerStrategy(s)
					summary["trigger_requests"]++
				}
			}
		} else {
			summary["strategy_list_failed"]++
			o.logger.Warn("gap_scanner: failed to list strategies for triggers", slog.Any("error", listErr))
		}
	}
	summary["gaps"] = len(gaps)

	o.logger.Info("gap_scanner: complete",
		slog.Int("scanned", len(symbols)),
		slog.Int("gaps_found", len(gaps)),
	)
	return gapScannerCompletionError(summary)
}

// discoveryRun runs the full strategy discovery pipeline on top watchlist tickers.
func (o *JobOrchestrator) discoveryRun(ctx context.Context) error {
	tickers, err := tradeableWatchlistTickers(ctx, o.logger, o.deps.Universe, o.deps.DataService, 300, 30)
	if err != nil {
		return fmt.Errorf("discovery_run: get watchlist: %w", err)
	}
	if len(tickers) == 0 {
		o.logger.Info("discovery_run: no tradeable watchlist tickers, skipping")
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
			Metrics:  o.deps.GeneratorMetrics,
		},
		Scoring:    discovery.DefaultScoringConfig(),
		MaxWinners: 3,
	}

	deps := discovery.DiscoveryDeps{
		DataService:     o.deps.DataService,
		LLMProvider:     o.deps.LLMProvider,
		Strategies:      o.deps.StrategyRepo,
		BacktestConfigs: o.deps.BacktestConfigRepo,
		Logger:          o.logger,
	}

	result, err := discovery.RunDiscovery(ctx, cfg, deps)
	if err != nil {
		return fmt.Errorf("discovery_run: %w", err)
	}

	o.SetLastSummary("discovery_run", map[string]int{
		"candidates": result.Candidates,
		"generated":  result.Generated,
		"swept":      result.Swept,
		"validated":  result.Validated,
		"deployed":   result.Deployed,
		"errors":     len(result.Errors),
		"winners":    len(result.Winners),
	})

	o.logger.Info("discovery_run: complete",
		slog.Int("candidates", result.Candidates),
		slog.Int("generated", result.Generated),
		slog.Int("swept", result.Swept),
		slog.Int("validated", result.Validated),
		slog.Int("deployed", result.Deployed),
		slog.Int("errors", len(result.Errors)),
		slog.Duration("duration", result.Duration),
	)

	for _, w := range result.Winners {
		o.logger.Info("discovery_run: winner deployed",
			slog.String("strategy_id", w.StrategyID.String()),
			slog.String("ticker", w.Ticker),
			slog.Float64("score", w.Score),
		)
	}

	return discoveryRunCompletionError(result.Errors)
}

func gapScannerCompletionError(summary map[string]int) error {
	incomplete := summary["failed_batches"] + summary["missing_snapshots"] + summary["stale_snapshots"] + summary["score_failed"] + summary["strategy_list_failed"]
	if incomplete == 0 {
		return nil
	}
	return fmt.Errorf("gap_scanner: incomplete run: failed_batches=%d missing_snapshots=%d stale_snapshots=%d score_failed=%d strategy_list_failed=%d",
		summary["failed_batches"], summary["missing_snapshots"], summary["stale_snapshots"], summary["score_failed"], summary["strategy_list_failed"])
}

func preMarketSnapshotFresh(now, updatedAt time.Time) bool {
	if updatedAt.IsZero() {
		return false
	}
	nowET := now.In(easternTime)
	updatedET := updatedAt.In(easternTime)
	if !sameMarketDate(nowET, updatedET) {
		return false
	}
	sessionStart := time.Date(nowET.Year(), nowET.Month(), nowET.Day(), 4, 0, 0, 0, easternTime)
	return !updatedET.Before(sessionStart) && !updatedET.After(nowET.Add(5*time.Minute))
}

func discoveryRunCompletionError(errors []string) error {
	if len(errors) == 0 {
		return nil
	}
	return fmt.Errorf("discovery_run: completed with %d pipeline errors", len(errors))
}

// positionReview reviews all active strategies and their open positions before market open.
func (o *JobOrchestrator) positionReview(ctx context.Context) error {
	if o.deps.StrategyRepo == nil || o.deps.PositionRepo == nil {
		return fmt.Errorf("position_review: strategy and position repositories are required")
	}
	strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{Status: domain.StrategyStatusActive})
	if err != nil {
		return fmt.Errorf("position_review: list strategies: %w", err)
	}
	positions, err := listAllOpenPositions(ctx, o.deps.PositionRepo)
	if err != nil {
		return fmt.Errorf("position_review: list open positions: %w", err)
	}

	active := make(map[uuid.UUID]domain.Strategy, len(strategies))
	for _, s := range strategies {
		active[s.ID] = s
	}
	withPositions := make(map[uuid.UUID]struct{})
	var unowned, inactiveStrategy, missingStop, missingPrice int
	for _, position := range positions {
		if position.StrategyID == nil {
			unowned++
		} else if _, ok := active[*position.StrategyID]; ok {
			withPositions[*position.StrategyID] = struct{}{}
		} else {
			inactiveStrategy++
		}
		if position.StopLoss == nil {
			missingStop++
		}
		if position.CurrentPrice == nil {
			missingPrice++
		}
		o.logger.Info("position_review: open position",
			slog.String("ticker", position.Ticker),
			slog.String("market_type", string(position.MarketType)),
			slog.String("side", position.Side.String()),
			slog.Float64("quantity", position.Quantity),
			slog.Any("unrealized_pnl", position.UnrealizedPnL),
		)
	}

	summary := map[string]int{
		"active_strategies":           len(strategies),
		"open_positions":              len(positions),
		"strategies_with_positions":   len(withPositions),
		"unowned_positions":           unowned,
		"inactive_strategy_positions": inactiveStrategy,
		"positions_missing_stop_loss": missingStop,
		"positions_missing_price":     missingPrice,
	}
	o.SetLastSummary("position_review", summary)
	o.logger.Info("position_review: complete",
		slog.Int("active_strategies", summary["active_strategies"]),
		slog.Int("open_positions", summary["open_positions"]),
		slog.Int("strategies_with_positions", summary["strategies_with_positions"]),
		slog.Int("unowned_positions", summary["unowned_positions"]),
		slog.Int("inactive_strategy_positions", summary["inactive_strategy_positions"]),
		slog.Int("positions_missing_stop_loss", summary["positions_missing_stop_loss"]),
		slog.Int("positions_missing_price", summary["positions_missing_price"]),
	)
	return positionReviewCompletionError(summary)
}

func positionReviewCompletionError(summary map[string]int) error {
	findings := summary["unowned_positions"] + summary["inactive_strategy_positions"] + summary["positions_missing_stop_loss"] + summary["positions_missing_price"]
	if findings == 0 {
		return nil
	}
	return fmt.Errorf("position_review: unsafe position findings: unowned=%d inactive_strategy=%d missing_stop_loss=%d missing_price=%d",
		summary["unowned_positions"], summary["inactive_strategy_positions"], summary["positions_missing_stop_loss"], summary["positions_missing_price"])
}
