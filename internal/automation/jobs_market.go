package automation

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

// Schedule specs for market-hours jobs.
var (
	hotScanSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		Cron:         "*/15 * * * 1-5",
		SkipWeekends: true,
		SkipHolidays: true,
	}
	deepScanSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		Cron:         "0 * * * 1-5",
		SkipWeekends: true,
		SkipHolidays: true,
	}
)

func (o *JobOrchestrator) registerMarketJobs() {
	o.Register("hot_scan", "Quick scan top 200 tickers by watch score", hotScanSpec, o.hotScan)
	o.Register("deep_scan", "Full universe snapshot and score update", deepScanSpec, o.deepScan, "hot_scan")
}

// hotScan scores the top 200 watchlist tickers using locally stored OHLCV data.
func (o *JobOrchestrator) hotScan(ctx context.Context) error {
	tickers, err := o.deps.Universe.GetWatchlist(ctx, 200)
	if err != nil {
		return fmt.Errorf("hot_scan: get watchlist: %w", err)
	}
	if len(tickers) == 0 {
		o.logger.Info("hot_scan: watchlist empty, nothing to scan")
		return nil
	}

	type mover struct {
		ticker    string
		changePct float64
	}
	var topMovers []mover

	now := time.Now()
	from := now.AddDate(0, 0, -10) // last 10 days for quick scoring

	for _, t := range tickers {
		bars, fetchErr := o.deps.DataService.GetOHLCV(ctx, "stock", t.Ticker, data.Timeframe1d, from, now)
		if fetchErr != nil || len(bars) < 2 {
			continue
		}

		lastBar := bars[len(bars)-1]
		prevBar := bars[len(bars)-2]
		changePct := 0.0
		if prevBar.Close > 0 {
			changePct = (lastBar.Close - prevBar.Close) / prevBar.Close * 100
		}

		score := scoreFromSnapshot(changePct, lastBar.Volume, prevBar.Volume, lastBar.Close) * universe.IndexBoost(t.Ticker)
		if err := o.deps.Universe.UpdateScore(ctx, t.Ticker, score); err != nil {
			o.logger.Warn("hot_scan: update score failed",
				slog.String("ticker", t.Ticker),
				slog.Any("error", err),
			)
		}
		topMovers = append(topMovers, mover{ticker: t.Ticker, changePct: changePct})
	}

	// Sort movers by absolute change pct descending.
	sort.Slice(topMovers, func(i, j int) bool {
		return math.Abs(topMovers[i].changePct) > math.Abs(topMovers[j].changePct)
	})

	logCount := 10
	if logCount > len(topMovers) {
		logCount = len(topMovers)
	}
	for _, m := range topMovers[:logCount] {
		o.logger.Info("hot_scan: top mover",
			slog.String("ticker", m.ticker),
			slog.Float64("change_pct", m.changePct),
		)
	}

	// Trigger active strategies for significant movers (|change| > 3%).
	if o.deps.StrategyTrigger != nil {
		significantTickers := make(map[string]float64)
		for _, m := range topMovers {
			if math.Abs(m.changePct) > 3.0 {
				significantTickers[m.ticker] = m.changePct
			}
		}
		if len(significantTickers) > 0 {
			strategies, listErr := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{
				Status: domain.StrategyStatusActive,
			}, 0, 0)
			if listErr == nil {
				for _, s := range strategies {
					if changePct, ok := significantTickers[s.Ticker]; ok {
						o.logger.Info("hot_scan: triggering strategy for significant move",
							slog.String("ticker", s.Ticker),
							slog.String("strategy_id", s.ID.String()),
							slog.Float64("change_pct", changePct),
						)
						o.deps.StrategyTrigger.TriggerStrategy(s)
					}
				}
			}
		}
	}

	o.logger.Info("hot_scan: complete", slog.Int("scanned", len(tickers)))
	return nil
}

// deepScan scores the universe using locally stored OHLCV data (from history_refresh)
// instead of the Polygon snapshot API, which requires a paid plan.
func (o *JobOrchestrator) deepScan(ctx context.Context) error {
	allSymbols, err := o.deps.Universe.GetActiveTickers(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("deep_scan: get active tickers: %w", err)
	}
	if len(allSymbols) == 0 {
		o.logger.Info("deep_scan: no active tickers")
		return nil
	}

	var totalScored int
	var scoreSum float64

	type scored struct {
		ticker string
		score  float64
	}
	var allScored []scored

	now := time.Now()
	from := now.AddDate(0, -1, 0) // 1 month of recent bars for scoring

	for i, ticker := range allSymbols {
		bars, fetchErr := o.deps.DataService.GetOHLCV(ctx, "stock", ticker, data.Timeframe1d, from, now)
		if fetchErr != nil || len(bars) < 5 {
			continue
		}

		// Score from recent bars: volatility + volume + momentum.
		lastBar := bars[len(bars)-1]
		prevBar := bars[len(bars)-2]
		changePct := 0.0
		if prevBar.Close > 0 {
			changePct = (lastBar.Close - prevBar.Close) / prevBar.Close * 100
		}

		score := scoreFromSnapshot(changePct, lastBar.Volume, prevBar.Volume, lastBar.Close) * universe.IndexBoost(ticker)
		if err := o.deps.Universe.UpdateScore(ctx, ticker, score); err != nil {
			o.logger.Warn("deep_scan: update score failed",
				slog.String("ticker", ticker),
				slog.Any("error", err),
			)
		}
		totalScored++
		scoreSum += score
		allScored = append(allScored, scored{ticker: ticker, score: score})

		if (i+1)%500 == 0 {
			o.logger.Info("deep_scan: progress",
				slog.Int("scored", i+1),
				slog.Int("total", len(allSymbols)),
			)
		}
	}

	// Log summary with top 10.
	avgScore := 0.0
	if totalScored > 0 {
		avgScore = scoreSum / float64(totalScored)
	}

	sort.Slice(allScored, func(i, j int) bool {
		return allScored[i].score > allScored[j].score
	})

	logCount := 10
	if logCount > len(allScored) {
		logCount = len(allScored)
	}

	o.logger.Info("deep_scan: summary",
		slog.Int("total_scanned", totalScored),
		slog.Float64("avg_score", avgScore),
	)
	for _, s := range allScored[:logCount] {
		o.logger.Info("deep_scan: top ticker",
			slog.String("ticker", s.ticker),
			slog.Float64("score", s.score),
		)
	}

	return nil
}

// scoreFromSnapshot computes a watch score combining momentum, volume surge,
// and dollar volume (liquidity). Dollar volume prevents penny stocks from
// dominating — a $0.50 stock needs 400x the share volume of a $200 stock
// to score equivalently on the liquidity component.
func scoreFromSnapshot(changePct, todayVol, prevVol, closePrice float64) float64 {
	volRatio := 1.0
	if prevVol > 0 {
		volRatio = todayVol / prevVol
	}

	momentum := math.Abs(changePct)
	volSurge := math.Log1p(math.Max(0, volRatio-1))           // only reward above-average volume
	dollarVol := math.Log10(math.Max(1, closePrice*todayVol)) // log10 of dollar volume

	// Weights: liquidity matters most, then momentum, then volume surge.
	return 0.4*dollarVol + 0.35*momentum + 0.25*volSurge
}
