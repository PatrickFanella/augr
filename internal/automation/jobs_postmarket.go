package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/eventmarkets"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
	"github.com/PatrickFanella/get-rich-quick/internal/universe"
)

const optionsScanWatchlistLimit = 100

func (o *JobOrchestrator) registerPostMarketJobs() {
	o.Register("daily_review", "Review daily pipeline completion and decision quality", dailyReviewSpec, o.dailyReview)
	o.Register("strategy_resweep", "Re-sweep deployed strategies with latest data", strategyResweepSpec, o.strategyResweep)
	o.Register("options_scan", "Scan options chains for next-day setups", optionsScanSpec, o.optionsScan)
}

var (
	dailyReviewSpec     = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "30 20 * * 1-5", SkipWeekends: true, SkipHolidays: true}
	strategyResweepSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "0 21 * * 1-5", SkipWeekends: true, SkipHolidays: true}
	optionsScanSpec     = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "0 22 * * 1-5", SkipWeekends: true, SkipHolidays: true}
)

// dailyReview checks all active strategies' pipeline runs from today and
// persists an operationally meaningful status/signal summary.
func (o *JobOrchestrator) dailyReview(ctx context.Context) error {
	o.logger.Info("daily_review: starting")
	if o.deps.StrategyRepo == nil || o.deps.RunRepo == nil {
		return fmt.Errorf("daily_review: strategy and pipeline run repositories are required")
	}

	strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{Status: "active"})
	if err != nil {
		return fmt.Errorf("daily_review: list strategies: %w", err)
	}

	today := easternDayStartUTC(time.Now())

	summary := map[string]int{"strategies": len(strategies), "query_errors": 0}
	defer func() { o.SetLastSummary("daily_review", summary) }()
	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		stratID := strat.ID
		runs, err := listAllPipelineRuns(ctx, o.deps.RunRepo, repository.PipelineRunFilter{
			StrategyID:   &stratID,
			StartedAfter: &today,
		})
		if err != nil {
			summary["query_errors"]++
			o.logger.Warn("daily_review: failed to list runs",
				slog.String("strategy", strat.Name),
				slog.Any("error", err),
			)
			continue
		}

		strategySummary := summarizePipelineRuns(runs)
		for key, value := range strategySummary {
			summary[key] += value
		}
		if strategySummary["failed"] > 0 || strategySummary["running"] > 0 {
			o.logger.Warn("daily_review: strategy has incomplete runs",
				slog.String("ticker", strat.Ticker),
				slog.String("strategy", strat.Name),
				slog.Int("failed", strategySummary["failed"]),
				slog.Int("running", strategySummary["running"]),
			)
		}
		o.logger.Info("daily_review: strategy summary", slog.String("ticker", strat.Ticker), slog.String("strategy", strat.Name), slog.Any("summary", strategySummary))
	}

	o.logger.Info("daily_review: completed", slog.Any("summary", summary))
	return dailyReviewCompletionError(summary)
}

func dailyReviewCompletionError(summary map[string]int) error {
	findings := summary["query_errors"] + summary[domain.PipelineStatusFailed.String()] + summary[domain.PipelineStatusRunning.String()] + summary["completed_without_signal"]
	if findings == 0 {
		return nil
	}
	return fmt.Errorf("daily_review: incomplete daily runs: query_errors=%d failed=%d running=%d completed_without_signal=%d",
		summary["query_errors"], summary[domain.PipelineStatusFailed.String()], summary[domain.PipelineStatusRunning.String()], summary["completed_without_signal"])
}

func listAllPipelineRuns(ctx context.Context, repo repository.PipelineRunRepository, filter repository.PipelineRunFilter) ([]domain.PipelineRun, error) {
	count, err := repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	const pageSize = 100
	runs := make([]domain.PipelineRun, 0, count)
	for offset := 0; offset < count; {
		page, err := repo.List(ctx, filter, min(pageSize, count-offset), offset)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		runs = append(runs, page...)
		offset += len(page)
	}
	return runs, nil
}

func easternDayStartUTC(now time.Time) time.Time {
	local := now.In(easternTime)
	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, easternTime).UTC()
}

func summarizePipelineRuns(runs []domain.PipelineRun) map[string]int {
	summary := map[string]int{"runs": len(runs)}
	for _, run := range runs {
		summary[run.Status.String()]++
		if run.Status != domain.PipelineStatusCompleted {
			continue
		}
		switch run.Signal {
		case domain.PipelineSignalBuy, domain.PipelineSignalSell, domain.PipelineSignalHold:
			summary[run.Signal.String()]++
		default:
			summary["completed_without_signal"]++
		}
	}
	return summary
}

// strategyResweep runs a lighter parameter sweep (10 variants) on each
// active strategy using the latest data, logging suggestions when a
// variant scores significantly better.
func (o *JobOrchestrator) strategyResweep(ctx context.Context) error {
	o.logger.Info("strategy_resweep: starting")
	summary := map[string]int{"strategies": 0, "eligible": 0, "swept": 0, "improved": 0, "skipped": 0, "failed": 0, "insufficient": 0, "stale": 0, "empty_results": 0}
	defer func() { o.SetLastSummary("strategy_resweep", summary) }()

	strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{Status: "active"})
	if err != nil {
		return fmt.Errorf("strategy_resweep: list strategies: %w", err)
	}
	summary["strategies"] = len(strategies)

	scoring := discovery.DefaultScoringConfig()
	now := time.Now()
	histFrom := now.AddDate(-1, 0, 0)

	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if !eventmarkets.SupportsOHLCVResweep(strat.MarketType) {
			summary["skipped"]++
			o.logger.Info("strategy_resweep: skipped unsupported market type",
				slog.String("ticker", strat.Ticker),
				slog.String("strategy", strat.Name),
				slog.String("market_type", strat.MarketType.String()),
			)
			continue
		}

		// Extract rules_engine config from strategy config JSON.
		rulesConfig, err := extractRulesConfig(strat.Config)
		if err != nil {
			summary["failed"]++
			o.logger.Warn("strategy_resweep: bad config",
				slog.String("strategy", strat.Name),
				slog.Any("error", err),
			)
			continue
		}
		summary["eligible"]++

		// Download 1 year of OHLCV.
		barsMap, err := o.deps.DataService.DownloadHistoricalOHLCV(
			ctx, strat.MarketType,
			[]string{strat.Ticker},
			data.Timeframe1d, histFrom, now, true,
		)
		if err != nil {
			summary["failed"]++
			o.logger.Warn("strategy_resweep: download failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		bars := barsMap[strat.Ticker]
		if len(bars) < 50 {
			summary["failed"]++
			summary["insufficient"]++
			o.logger.Warn("strategy_resweep: insufficient bars",
				slog.String("ticker", strat.Ticker),
				slog.Int("bars", len(bars)),
			)
			continue
		}
		if !dailyBarFresh(now, bars[len(bars)-1].Timestamp) {
			summary["failed"]++
			summary["stale"]++
			o.logger.Warn("strategy_resweep: stale latest bar",
				slog.String("ticker", strat.Ticker),
				slog.Time("latest", bars[len(bars)-1].Timestamp),
			)
			continue
		}

		sweepCfg := discovery.SweepConfig{
			Ticker:      strat.Ticker,
			MarketType:  strat.MarketType,
			Bars:        bars,
			StartDate:   bars[0].Timestamp,
			EndDate:     bars[len(bars)-1].Timestamp,
			InitialCash: 100_000,
			Variations:  10,
		}

		results, err := discovery.RunSweep(ctx, *rulesConfig, sweepCfg, scoring, o.logger)
		if err != nil {
			summary["failed"]++
			o.logger.Warn("strategy_resweep: sweep failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		if len(results) == 0 {
			summary["failed"]++
			summary["empty_results"]++
			continue
		}
		summary["swept"]++

		// Score the current config (the "base" variant is always index 0 in results
		// but let's find it explicitly).
		var currentScore float64
		for _, r := range results {
			if r.Label == "base" {
				currentScore = r.Score
				break
			}
		}

		best := results[0]
		if currentScore > 0 && best.Score > currentScore*1.20 {
			summary["improved"]++
			o.logger.Info("strategy_resweep: improvement found",
				slog.String("ticker", strat.Ticker),
				slog.String("strategy", strat.Name),
				slog.String("best_variant", best.Label),
				slog.Float64("current_score", currentScore),
				slog.Float64("best_score", best.Score),
				slog.Float64("improvement_pct", (best.Score-currentScore)/currentScore*100),
			)
		} else {
			o.logger.Info("strategy_resweep: no significant improvement",
				slog.String("ticker", strat.Ticker),
				slog.Float64("current_score", currentScore),
				slog.Float64("best_score", best.Score),
			)
		}
	}

	o.logger.Info("strategy_resweep: completed", slog.Int("strategies", len(strategies)))
	return strategyResweepCompletionError(summary["failed"])
}

func strategyResweepCompletionError(failed int) error {
	if failed <= 0 {
		return nil
	}
	return fmt.Errorf("strategy_resweep: %d strategies failed", failed)
}

// optionsScan fetches options chains for the top watchlist tickers and logs
// setups with elevated IV, unusual volume, or favourable put/call skew.
func (o *JobOrchestrator) optionsScan(ctx context.Context) error {
	o.logger.Info("options_scan: starting")
	summary := map[string]int{"universe": 0, "optionable": 0, "price_fetch_failed": 0, "price_empty": 0, "price_stale": 0, "chains": 0, "chain_insufficient": 0, "setups": 0, "fetch_failed": 0, "persist_failed": 0}
	defer func() { o.SetLastSummary("options_scan", summary) }()

	if o.deps.OptionsProvider == nil {
		return fmt.Errorf("options_scan: options data provider not configured")
	}

	if o.deps.Universe == nil {
		return fmt.Errorf("options_scan: universe not configured")
	}
	if o.deps.DataService == nil {
		return fmt.Errorf("options_scan: data service not configured")
	}

	// Fetch the bounded top watchlist and filter for optionable names (price > $5).
	watchlist, err := o.deps.Universe.GetWatchlist(ctx, optionsScanWatchlistLimit)
	if err != nil {
		return fmt.Errorf("options_scan: get watchlist: %w", err)
	}
	allTickers := optionsScanTickers(watchlist)
	summary["universe"] = len(allTickers)

	type optionable struct {
		ticker string
		close  float64
	}
	var candidates []optionable
	for _, ticker := range allTickers {
		if o.deps.DataService != nil {
			priceNow := time.Now()
			bars, err := o.deps.DataService.GetOHLCV(ctx, domain.MarketTypeStock, ticker, data.Timeframe1d, priceNow.AddDate(0, 0, -5), priceNow)
			if err != nil {
				summary["price_fetch_failed"]++
				o.logger.Warn("options_scan: price lookup failed", slog.String("ticker", ticker), slog.Any("error", err))
				continue
			}
			if len(bars) == 0 {
				summary["price_empty"]++
				continue
			}
			if !dailyBarFresh(priceNow, bars[len(bars)-1].Timestamp) {
				summary["price_stale"]++
				continue
			}
			closePrice := bars[len(bars)-1].Close
			if closePrice < 5.0 {
				continue
			}
			candidates = append(candidates, optionable{ticker: ticker, close: closePrice})
		} else {
			candidates = append(candidates, optionable{ticker: ticker})
		}
	}

	o.logger.Info("options_scan: filtered optionable tickers",
		slog.Int("universe", len(allTickers)),
		slog.Int("optionable", len(candidates)),
	)
	summary["optionable"] = len(candidates)

	// Target expiry window: 20-50 DTE (sweet spot for premium selling).
	now := time.Now()
	targetExpiry := now.AddDate(0, 0, 30) // ~30 DTE centre

	var hits int
	for _, candidate := range candidates {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		chain, err := o.deps.OptionsProvider.GetOptionsChain(ctx, candidate.ticker, targetExpiry, "")
		if err != nil {
			summary["fetch_failed"]++
			o.logger.Warn("options_scan: chain fetch failed",
				slog.String("ticker", candidate.ticker),
				slog.Any("error", err),
			)
			continue
		}
		if len(chain) < 10 { // need at least 10 contracts for a meaningful chain
			summary["chain_insufficient"]++
			continue
		}
		summary["chains"]++

		o.logger.Info("options_scan: chain found",
			slog.String("ticker", candidate.ticker),
			slog.Int("contracts", len(chain)),
		)

		result := analyzeChain(chain)
		if result == nil {
			continue
		}

		hits++
		summary["setups"]++
		o.logger.Info("options_scan: setup found",
			slog.String("ticker", candidate.ticker),
			slog.Float64("atm_iv", result.atmIV),
			slog.Float64("put_call_ratio", result.putCallRatio),
			slog.Float64("avg_spread_pct", result.avgSpreadPct),
			slog.Int("total_contracts", result.totalContracts),
			slog.Float64("total_volume", result.totalVolume),
			slog.Float64("max_oi", result.maxOI),
			slog.String("note", result.note),
		)

		// Persist scan result and IV history.
		if o.deps.OptionsScanRepo != nil {
			scanDate := easternDayStartUTC(now)
			if err := o.deps.OptionsScanRepo.UpsertScanResult(ctx, &pgrepo.OptionsScanResult{
				Ticker:       candidate.ticker,
				ScanDate:     scanDate,
				ATMIV:        result.atmIV,
				PutCallRatio: result.putCallRatio,
				ChainDepth:   result.totalContracts,
				ATMOI:        result.maxOI,
			}); err != nil {
				summary["persist_failed"]++
				o.logger.Warn("options_scan: persist scan result failed", slog.String("ticker", candidate.ticker), slog.Any("error", err))
			}
			if err := o.deps.OptionsScanRepo.UpsertIVHistory(ctx, &pgrepo.IVHistoryRecord{
				Ticker: candidate.ticker,
				Date:   scanDate,
				ATMIV:  result.atmIV,
			}); err != nil {
				summary["persist_failed"]++
				o.logger.Warn("options_scan: persist IV history failed", slog.String("ticker", candidate.ticker), slog.Any("error", err))
			}
		} else {
			summary["persist_failed"]++
			o.logger.Warn("options_scan: setup persistence unavailable", slog.String("ticker", candidate.ticker))
		}
	}

	o.logger.Info("options_scan: completed",
		slog.Int("tickers_scanned", len(candidates)),
		slog.Int("setups_found", hits),
	)
	return optionsScanCompletionError(summary)
}

func optionsScanCompletionError(summary map[string]int) error {
	noUsableChains := 0
	if summary["optionable"] > 0 && summary["chains"] == 0 {
		noUsableChains = 1
	}
	errors := summary["price_fetch_failed"] + summary["price_empty"] + summary["price_stale"] + summary["fetch_failed"] + summary["persist_failed"] + noUsableChains
	if errors == 0 {
		return nil
	}
	return fmt.Errorf("options_scan: incomplete run: price_fetch_failed=%d price_empty=%d price_stale=%d chain_fetch_failed=%d persist_failed=%d no_usable_chains=%d",
		summary["price_fetch_failed"], summary["price_empty"], summary["price_stale"], summary["fetch_failed"], summary["persist_failed"], noUsableChains)
}

func optionsScanTickers(watchlist []universe.TrackedTicker) []string {
	tickers := make([]string, 0, min(len(watchlist), optionsScanWatchlistLimit))
	seen := make(map[string]struct{}, min(len(watchlist), optionsScanWatchlistLimit))
	for _, tracked := range watchlist {
		ticker := strings.ToUpper(strings.TrimSpace(tracked.Ticker))
		if ticker == "" {
			continue
		}
		if _, exists := seen[ticker]; exists {
			continue
		}
		seen[ticker] = struct{}{}
		tickers = append(tickers, ticker)
		if len(tickers) == optionsScanWatchlistLimit {
			break
		}
	}
	return tickers
}

type chainAnalysis struct {
	atmIV          float64
	putCallRatio   float64
	avgSpreadPct   float64
	totalContracts int
	totalVolume    float64
	maxOI          float64
	note           string
}

// analyzeChain evaluates an options chain for actionable setups.
// Returns nil if nothing interesting is found.
func analyzeChain(chain []domain.OptionSnapshot) *chainAnalysis {
	if len(chain) == 0 {
		return nil
	}

	// Find ATM IV: the contract with the narrowest bid/ask spread near the money.
	// We approximate "near the money" by finding the strike closest to mid price.
	var putVol, callVol, totalVol, totalOI float64
	var spreadSum float64
	var liquidContracts int
	var maxOI float64

	for _, snap := range chain {
		totalVol += snap.Volume
		totalOI += snap.OpenInterest
		if snap.OpenInterest > maxOI {
			maxOI = snap.OpenInterest
		}
		switch snap.Contract.OptionType {
		case domain.OptionTypePut:
			putVol += snap.Volume
		case domain.OptionTypeCall:
			callVol += snap.Volume
		}
		if snap.Bid > 0 && snap.Ask > 0 {
			spreadPct := (snap.Ask - snap.Bid) / snap.Mid * 100
			spreadSum += spreadPct
			liquidContracts++
		}
	}

	// Find ATM IV from the call with delta closest to 0.50.
	var atmIV float64
	bestDeltaDist := 999.0
	for _, snap := range chain {
		if snap.Contract.OptionType != domain.OptionTypeCall {
			continue
		}
		dist := math.Abs(math.Abs(snap.Greeks.Delta) - 0.50)
		if dist < bestDeltaDist {
			bestDeltaDist = dist
			atmIV = snap.Greeks.IV
		}
	}

	var putCallRatio float64
	if callVol > 0 {
		putCallRatio = putVol / callVol
	}

	var avgSpread float64
	if liquidContracts > 0 {
		avgSpread = spreadSum / float64(liquidContracts)
	}

	// Flag setups: elevated IV (>40%), unusual put/call ratio, or high volume.
	var notes []string
	if atmIV > 0.40 {
		notes = append(notes, fmt.Sprintf("elevated IV %.0f%%", atmIV*100))
	}
	if putCallRatio > 1.5 {
		notes = append(notes, fmt.Sprintf("high put/call %.2f", putCallRatio))
	} else if putCallRatio > 0 && putCallRatio < 0.5 {
		notes = append(notes, fmt.Sprintf("low put/call %.2f (bullish)", putCallRatio))
	}
	if totalVol > 10000 {
		notes = append(notes, fmt.Sprintf("high volume %.0f", totalVol))
	}

	if len(notes) == 0 {
		return nil
	}

	note := notes[0]
	for _, n := range notes[1:] {
		note += "; " + n
	}

	return &chainAnalysis{
		atmIV:          atmIV,
		putCallRatio:   putCallRatio,
		avgSpreadPct:   avgSpread,
		totalContracts: len(chain),
		totalVolume:    totalVol,
		maxOI:          maxOI,
		note:           note,
	}
}

// extractRulesConfig parses the rules_engine config from a strategy's
// raw JSON Config field.
func extractRulesConfig(raw json.RawMessage) (*rules.RulesEngineConfig, error) {
	var wrapper struct {
		RulesEngine rules.RulesEngineConfig `json:"rules_engine"`
	}
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return nil, fmt.Errorf("unmarshal rules_engine config: %w", err)
	}
	return &wrapper.RulesEngine, nil
}
