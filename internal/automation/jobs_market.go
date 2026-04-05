package automation

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
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

// hotScan scans the top 200 watchlist tickers and updates their scores.
func (o *JobOrchestrator) hotScan(ctx context.Context) error {
	tickers, err := o.deps.Universe.GetWatchlist(ctx, 200)
	if err != nil {
		return fmt.Errorf("hot_scan: get watchlist: %w", err)
	}
	if len(tickers) == 0 {
		o.logger.Info("hot_scan: watchlist empty, nothing to scan")
		return nil
	}

	symbols := make([]string, len(tickers))
	for i, t := range tickers {
		symbols[i] = t.Ticker
	}

	// Batch snapshot 100 at a time.
	const batchSize = 100
	type mover struct {
		ticker    string
		changePct float64
	}
	var topMovers []mover

	for i := 0; i < len(symbols); i += batchSize {
		end := i + batchSize
		if end > len(symbols) {
			end = len(symbols)
		}
		batch := symbols[i:end]

		snapshots, snapErr := o.deps.Polygon.BulkSnapshot(ctx, batch)
		if snapErr != nil {
			o.logger.Warn("hot_scan: snapshot batch failed",
				slog.Int("offset", i),
				slog.Any("error", snapErr),
			)
			continue
		}

		for _, snap := range snapshots {
			score := scoreFromSnapshot(snap.TodaysChangePct, snap.Day.Volume, snap.PrevDay.Volume)
			if err := o.deps.Universe.UpdateScore(ctx, snap.Ticker, score); err != nil {
				o.logger.Warn("hot_scan: update score failed",
					slog.String("ticker", snap.Ticker),
					slog.Any("error", err),
				)
			}
			topMovers = append(topMovers, mover{ticker: snap.Ticker, changePct: snap.TodaysChangePct})
		}
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

	o.logger.Info("hot_scan: complete", slog.Int("scanned", len(symbols)))
	return nil
}

// deepScan scans the entire active universe and updates all watch scores.
func (o *JobOrchestrator) deepScan(ctx context.Context) error {
	allSymbols, err := o.deps.Universe.GetActiveTickers(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("deep_scan: get active tickers: %w", err)
	}
	if len(allSymbols) == 0 {
		o.logger.Info("deep_scan: no active tickers")
		return nil
	}

	const batchSize = 100
	const batchPause = 300 * time.Millisecond

	var totalScored int
	var scoreSum float64

	type scored struct {
		ticker string
		score  float64
	}
	var allScored []scored

	for i := 0; i < len(allSymbols); i += batchSize {
		end := i + batchSize
		if end > len(allSymbols) {
			end = len(allSymbols)
		}
		batch := allSymbols[i:end]

		snapshots, snapErr := o.deps.Polygon.BulkSnapshot(ctx, batch)
		if snapErr != nil {
			o.logger.Warn("deep_scan: snapshot batch failed",
				slog.Int("offset", i),
				slog.Any("error", snapErr),
			)
			continue
		}

		for _, snap := range snapshots {
			score := scoreFromSnapshot(snap.TodaysChangePct, snap.Day.Volume, snap.PrevDay.Volume)
			if err := o.deps.Universe.UpdateScore(ctx, snap.Ticker, score); err != nil {
				o.logger.Warn("deep_scan: update score failed",
					slog.String("ticker", snap.Ticker),
					slog.Any("error", err),
				)
			}
			totalScored++
			scoreSum += score
			allScored = append(allScored, scored{ticker: snap.Ticker, score: score})
		}

		// Pause between batches to respect rate limits.
		if end < len(allSymbols) {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(batchPause):
			}
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

// scoreFromSnapshot computes a simple watch score from snapshot data.
// Higher absolute change and higher relative volume produce higher scores.
func scoreFromSnapshot(changePct, todayVol, prevVol float64) float64 {
	volRatio := 1.0
	if prevVol > 0 {
		volRatio = todayVol / prevVol
	}
	return math.Abs(changePct) * math.Log1p(volRatio)
}
