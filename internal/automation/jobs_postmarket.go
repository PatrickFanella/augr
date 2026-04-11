package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

func (o *JobOrchestrator) registerPostMarketJobs() {
	o.Register("daily_review", "Performance review — disable losing strategies", dailyReviewSpec, o.dailyReview)
	o.Register("strategy_resweep", "Re-sweep deployed strategies with latest data", strategyResweepSpec, o.strategyResweep)
	o.Register("options_scan", "Scan options chains for next-day setups", optionsScanSpec, o.optionsScan)
}

var (
	dailyReviewSpec     = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "30 20 * * 1-5", SkipWeekends: true, SkipHolidays: true}
	strategyResweepSpec = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "0 21 * * 1-5", SkipWeekends: true, SkipHolidays: true}
	optionsScanSpec     = scheduler.ScheduleSpec{Type: scheduler.ScheduleTypeAfterHours, Cron: "0 22 * * 1-5", SkipWeekends: true, SkipHolidays: true}
)

// dailyReview checks all active strategies' pipeline runs from today
// and logs a summary per strategy.
func (o *JobOrchestrator) dailyReview(ctx context.Context) error {
	o.logger.Info("daily_review: starting")

	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{Status: "active"}, 100, 0)
	if err != nil {
		return fmt.Errorf("daily_review: list strategies: %w", err)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)

	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		stratID := strat.ID
		runs, err := o.deps.RunRepo.List(ctx, repository.PipelineRunFilter{
			StrategyID:   &stratID,
			StartedAfter: &today,
		}, 50, 0)
		if err != nil {
			o.logger.Warn("daily_review: failed to list runs",
				slog.String("strategy", strat.Name),
				slog.Any("error", err),
			)
			continue
		}

		var buyCount, sellCount, holdCount int
		var confidenceSum float64
		for _, run := range runs {
			switch run.Signal {
			case domain.PipelineSignalBuy:
				buyCount++
			case domain.PipelineSignalSell:
				sellCount++
			case domain.PipelineSignalHold:
				holdCount++
			}
			// Use signal as rough confidence proxy: buy=1, sell=-1, hold=0.
			switch run.Signal {
			case domain.PipelineSignalBuy:
				confidenceSum += 1.0
			case domain.PipelineSignalSell:
				confidenceSum -= 1.0
			}
		}

		totalRuns := len(runs)
		var avgConfidence float64
		if totalRuns > 0 {
			avgConfidence = confidenceSum / float64(totalRuns)
		}

		// Warn if strategy has enough runs and trending negative.
		totalAllTime := buyCount + sellCount + holdCount
		if totalAllTime >= 5 && avgConfidence < 0 {
			o.logger.Warn("daily_review: strategy trending negative",
				slog.String("ticker", strat.Ticker),
				slog.String("strategy", strat.Name),
				slog.Float64("avg_confidence", avgConfidence),
			)
		}

		o.logger.Info(fmt.Sprintf("daily_review: %s — %d runs today, %d buy, %d sell, %d hold, avg confidence %.2f",
			strat.Ticker, totalRuns, buyCount, sellCount, holdCount, avgConfidence),
		)
	}

	o.logger.Info("daily_review: completed", slog.Int("strategies", len(strategies)))
	return nil
}

// strategyResweep runs a lighter parameter sweep (10 variants) on each
// active strategy using the latest data, logging suggestions when a
// variant scores significantly better.
func (o *JobOrchestrator) strategyResweep(ctx context.Context) error {
	o.logger.Info("strategy_resweep: starting")

	strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{Status: "active"}, 100, 0)
	if err != nil {
		return fmt.Errorf("strategy_resweep: list strategies: %w", err)
	}

	scoring := discovery.DefaultScoringConfig()
	now := time.Now()
	histFrom := now.AddDate(-1, 0, 0)

	for _, strat := range strategies {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Extract rules_engine config from strategy config JSON.
		rulesConfig, err := extractRulesConfig(strat.Config)
		if err != nil {
			o.logger.Warn("strategy_resweep: bad config",
				slog.String("strategy", strat.Name),
				slog.Any("error", err),
			)
			continue
		}

		// Download 1 year of OHLCV.
		barsMap, err := o.deps.DataService.DownloadHistoricalOHLCV(
			ctx, strat.MarketType,
			[]string{strat.Ticker},
			data.Timeframe1d, histFrom, now, true,
		)
		if err != nil {
			o.logger.Warn("strategy_resweep: download failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		bars := barsMap[strat.Ticker]
		if len(bars) < 50 {
			o.logger.Warn("strategy_resweep: insufficient bars",
				slog.String("ticker", strat.Ticker),
				slog.Int("bars", len(bars)),
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
			o.logger.Warn("strategy_resweep: sweep failed",
				slog.String("ticker", strat.Ticker),
				slog.Any("error", err),
			)
			continue
		}

		if len(results) == 0 {
			continue
		}

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
	return nil
}

// optionsScan fetches options chains for the top watchlist tickers and logs
// setups with elevated IV, unusual volume, or favourable put/call skew.
func (o *JobOrchestrator) optionsScan(ctx context.Context) error {
	o.logger.Info("options_scan: starting")

	if o.deps.OptionsProvider == nil {
		o.logger.Info("options_scan: skipped — options data provider not configured")
		return nil
	}

	if o.deps.Universe == nil {
		o.logger.Info("options_scan: skipped — Universe not configured")
		return nil
	}

	// Fetch all active tickers and filter for optionable ones (price > $5).
	allTickers, err := o.deps.Universe.GetActiveTickers(ctx, "", 0)
	if err != nil {
		return fmt.Errorf("options_scan: get tickers: %w", err)
	}

	type optionable struct {
		ticker string
		close  float64
	}
	var candidates []optionable
	for _, ticker := range allTickers {
		if o.deps.DataService != nil {
			bars, err := o.deps.DataService.GetOHLCV(ctx, domain.MarketTypeStock, ticker, data.Timeframe1d, time.Now().AddDate(0, 0, -5), time.Now())
			if err != nil || len(bars) == 0 {
				continue
			}
			close_ := bars[len(bars)-1].Close
			if close_ < 5.0 {
				continue
			}
			candidates = append(candidates, optionable{ticker: ticker, close: close_})
		} else {
			candidates = append(candidates, optionable{ticker: ticker})
		}
	}

	o.logger.Info("options_scan: filtered optionable tickers",
		slog.Int("universe", len(allTickers)),
		slog.Int("optionable", len(candidates)),
	)

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
			o.logger.Warn("options_scan: chain fetch failed",
				slog.String("ticker", candidate.ticker),
				slog.Any("error", err),
			)
			continue
		}
		if len(chain) < 10 { // need at least 10 contracts for a meaningful chain
			continue
		}

		o.logger.Info("options_scan: chain found",
			slog.String("ticker", candidate.ticker),
			slog.Int("contracts", len(chain)),
		)

		result := analyzeChain(candidate.ticker, chain, now)
		if result == nil {
			continue
		}

		hits++
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
			scanDate := now.Truncate(24 * time.Hour)
			_ = o.deps.OptionsScanRepo.UpsertScanResult(ctx, &pgrepo.OptionsScanResult{
				Ticker:       candidate.ticker,
				ScanDate:     scanDate,
				ATMIV:        result.atmIV,
				PutCallRatio: result.putCallRatio,
				ChainDepth:   result.totalContracts,
				ATMOI:        result.maxOI,
			})
			_ = o.deps.OptionsScanRepo.UpsertIVHistory(ctx, &pgrepo.IVHistoryRecord{
				Ticker: candidate.ticker,
				Date:   scanDate,
				ATMIV:  result.atmIV,
			})
		}
	}

	o.logger.Info("options_scan: completed",
		slog.Int("tickers_scanned", len(candidates)),
		slog.Int("setups_found", hits),
	)
	return nil
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
func analyzeChain(ticker string, chain []domain.OptionSnapshot, now time.Time) *chainAnalysis {
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
