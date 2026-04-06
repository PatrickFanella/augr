package options

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/discovery"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsSweepConfig controls the options backtest sweep.
type OptionsSweepConfig struct {
	Ticker      string
	Bars        []domain.OHLCV
	StartDate   time.Time
	EndDate     time.Time
	InitialCash float64
	Variations  int
	FillConfig  backtest.OptionsFillConfig
}

// OptionsSweepResult pairs an options config with backtest metrics.
type OptionsSweepResult struct {
	Label   string
	Config  rules.OptionsRulesConfig
	Metrics backtest.Metrics
	Score   float64
}

// RunOptionsSweep backtests multiple parameter variants and returns scored results.
func RunOptionsSweep(
	ctx context.Context,
	baseConfig rules.OptionsRulesConfig,
	cfg OptionsSweepConfig,
	scoring discovery.ScoringConfig,
	logger *slog.Logger,
) ([]OptionsSweepResult, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Variations <= 0 {
		cfg.Variations = 20
	}
	if cfg.InitialCash <= 0 {
		cfg.InitialCash = 100_000
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Generate variants.
	variants := make([]rules.OptionsRulesConfig, 0, cfg.Variations+1)
	variants = append(variants, baseConfig) // base is always first
	for i := 0; i < cfg.Variations; i++ {
		variants = append(variants, mutateOptionsConfig(baseConfig, rng))
	}

	// Filter bars to date range.
	var bars []domain.OHLCV
	for _, b := range cfg.Bars {
		if !b.Timestamp.Before(cfg.StartDate) && !b.Timestamp.After(cfg.EndDate) {
			bars = append(bars, b)
		}
	}
	if len(bars) < 50 {
		return nil, nil
	}

	// Compute realized vol for synthetic chain generation.
	rv := realizedVol(cfg.Bars, 60) // 60-day realized vol
	if rv < 0.05 {
		rv = 0.20 // floor at 20%
	}

	var results []OptionsSweepResult

	for i, variant := range variants {
		if ctx.Err() != nil {
			return results, ctx.Err()
		}

		label := "base"
		if i > 0 {
			label = "variant"
		}

		metrics := runOptionsBacktest(variant, bars, rv, cfg)
		score := discovery.ScoreMetrics(metrics, scoring)

		results = append(results, OptionsSweepResult{
			Label:   label,
			Config:  variant,
			Metrics: metrics,
			Score:   score,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	return results, nil
}

// optionsPosition tracks an open options position during backtest.
type optionsPosition struct {
	spread    *domain.OptionSpread
	entryBar  domain.OHLCV
	entryMid  float64 // net premium received (credit) or paid (debit)
	maxProfit float64 // max potential profit
	maxRisk   float64 // max potential loss
}

// runOptionsBacktest executes a single options backtest variant.
func runOptionsBacktest(
	config rules.OptionsRulesConfig,
	bars []domain.OHLCV,
	realizedVol float64,
	cfg OptionsSweepConfig,
) backtest.Metrics {
	cash := cfg.InitialCash
	var position *optionsPosition
	chainCfg := backtest.DefaultSyntheticChainConfig()

	equityCurve := make([]backtest.EquityPoint, 0, len(bars))
	var prevSnap *rules.Snapshot

	for _, bar := range bars {
		// Build snapshot from bar.
		snap := rules.Snapshot{
			Values: map[string]float64{
				"close":  bar.Close,
				"open":   bar.Open,
				"high":   bar.High,
				"low":    bar.Low,
				"volume": bar.Volume,
			},
		}

		// Compute simple indicators from recent bars context.
		// (For sweep, we rely on the condition fields that are in the snapshot.)

		// Synthesize options chain.
		dte := avgDTE(config.LegSelection)
		chain := backtest.SynthesizeChain(bar.Close, realizedVol, dte, bar.Timestamp, chainCfg)

		// Build options snapshot.
		optSnap := rules.NewOptionsSnapshot(snap, chain, nil, bar.Timestamp)

		marketValue := 0.0
		if position != nil {
			// Mark-to-market: estimate current spread value from synthetic chain.
			marketValue = estimateSpreadValue(position, bar.Close, realizedVol, bar.Timestamp, chainCfg)
		}

		equityCurve = append(equityCurve, backtest.EquityPoint{
			Timestamp:   bar.Timestamp,
			Cash:        cash,
			MarketValue: marketValue,
			Equity:      cash + marketValue,
		})

		if position == nil {
			// Evaluate entry.
			if rules.EvaluateGroup(config.Entry, optSnap.Snapshot, prevSnap) {
				spread, entryMid := buildSyntheticSpread(config, chain, bar)
				if spread != nil {
					maxProfit, maxRisk := spreadRiskReward(spread, entryMid)
					position = &optionsPosition{
						spread:    spread,
						entryBar:  bar,
						entryMid:  entryMid,
						maxProfit: maxProfit,
						maxRisk:   maxRisk,
					}
					cash -= maxRisk // reserve max risk as collateral
				}
			}
		} else {
			// Check management rules.
			shouldClose, reason := checkManagement(position, config.Management, bar, realizedVol, chainCfg)
			if !shouldClose {
				// Evaluate exit conditions.
				if rules.EvaluateGroup(config.Exit, optSnap.Snapshot, prevSnap) {
					shouldClose = true
					reason = "exit_signal"
				}
			}

			if shouldClose {
				pnl := closePosition(position, bar, realizedVol, chainCfg)
				cash += position.maxRisk + pnl // release collateral + P&L
				position = nil
				_ = reason
			}
		}

		prevSnap = &snap
	}

	return backtest.ComputeMetrics(equityCurve, bars)
}

// buildSyntheticSpread selects legs from the synthetic chain and builds a spread.
func buildSyntheticSpread(config rules.OptionsRulesConfig, chain []domain.OptionSnapshot, bar domain.OHLCV) (*domain.OptionSpread, float64) {
	now := bar.Timestamp
	selectedLegs, err := rules.SelectSpreadLegs(chain, config.LegSelection, now)
	if err != nil {
		return nil, 0
	}

	spread, err := rules.BuildSpread(config.StrategyType, config.Underlying, selectedLegs, config.LegSelection)
	if err != nil {
		return nil, 0
	}

	// Calculate net premium (positive = credit received).
	var netPremium float64
	for legName, snap := range selectedLegs {
		sel := config.LegSelection[legName]
		if sel.Side == "sell" {
			netPremium += snap.Mid * snap.Contract.Multiplier
		} else {
			netPremium -= snap.Mid * snap.Contract.Multiplier
		}
	}

	return spread, netPremium
}

// spreadRiskReward estimates max profit and max risk for a spread.
func spreadRiskReward(spread *domain.OptionSpread, netPremium float64) (maxProfit, maxRisk float64) {
	if len(spread.Legs) < 2 {
		// Single leg (e.g. covered call) — risk is unlimited, cap at premium * 3.
		return math.Abs(netPremium), math.Abs(netPremium) * 3
	}

	// Vertical spread: max risk = width between strikes - net premium.
	var strikes []float64
	for _, leg := range spread.Legs {
		strikes = append(strikes, leg.Contract.Strike)
	}
	sort.Float64s(strikes)
	width := (strikes[len(strikes)-1] - strikes[0]) * 100 // * multiplier

	if netPremium > 0 {
		// Credit spread.
		maxProfit = netPremium
		maxRisk = width - netPremium
	} else {
		// Debit spread.
		maxProfit = width + netPremium // netPremium is negative
		maxRisk = -netPremium
	}

	if maxRisk < 0 {
		maxRisk = math.Abs(netPremium)
	}
	return
}

// checkManagement evaluates automated management rules.
func checkManagement(pos *optionsPosition, mgmt rules.OptionsManagement, bar domain.OHLCV, vol float64, chainCfg backtest.SyntheticChainConfig) (bool, string) {
	currentValue := estimateSpreadValue(pos, bar.Close, vol, bar.Timestamp, chainCfg)
	pnl := currentValue + pos.entryMid // for credit spreads: entry is positive, current should decay

	// Close at profit target.
	if mgmt.CloseAtProfitPct > 0 && pos.maxProfit > 0 {
		if pnl >= pos.maxProfit*mgmt.CloseAtProfitPct {
			return true, "profit_target"
		}
	}

	// Close at DTE.
	if mgmt.CloseAtDTE > 0 && len(pos.spread.Legs) > 0 {
		expiry := pos.spread.Legs[0].Contract.Expiry
		dte := int(expiry.Sub(bar.Timestamp).Hours() / 24)
		if dte <= mgmt.CloseAtDTE {
			return true, "dte_close"
		}
	}

	// Stop loss.
	if mgmt.StopLossPct > 0 && pos.maxRisk > 0 {
		loss := -pnl
		if loss >= pos.maxRisk*mgmt.StopLossPct {
			return true, "stop_loss"
		}
	}

	return false, ""
}

// estimateSpreadValue estimates the current mark-to-market value of an open spread.
func estimateSpreadValue(pos *optionsPosition, underlying, vol float64, now time.Time, chainCfg backtest.SyntheticChainConfig) float64 {
	if pos == nil || pos.spread == nil {
		return 0
	}

	var value float64
	for _, leg := range pos.spread.Legs {
		dte := int(leg.Contract.Expiry.Sub(now).Hours() / 24)
		if dte < 1 {
			dte = 1
		}
		chain := backtest.SynthesizeChain(underlying, vol, dte, now, chainCfg)

		// Find the contract in the synthetic chain closest to our strike.
		bestDist := math.Inf(1)
		var legValue float64
		for _, snap := range chain {
			if snap.Contract.OptionType != leg.Contract.OptionType {
				continue
			}
			dist := math.Abs(snap.Contract.Strike - leg.Contract.Strike)
			if dist < bestDist {
				bestDist = dist
				legValue = snap.Mid * leg.Contract.Multiplier
			}
		}

		if leg.Side == "sell" {
			value -= legValue // we owe this
		} else {
			value += legValue // we own this
		}
	}

	return value
}

// closePosition calculates P&L when closing a spread.
func closePosition(pos *optionsPosition, bar domain.OHLCV, vol float64, chainCfg backtest.SyntheticChainConfig) float64 {
	currentValue := estimateSpreadValue(pos, bar.Close, vol, bar.Timestamp, chainCfg)
	return pos.entryMid + currentValue
}

// avgDTE returns the average target DTE from leg selectors.
func avgDTE(legs map[string]rules.LegSelector) int {
	if len(legs) == 0 {
		return 30
	}
	var sum int
	for _, sel := range legs {
		sum += (sel.DTEMin + sel.DTEMax) / 2
	}
	avg := sum / len(legs)
	if avg < 7 {
		return 30
	}
	return avg
}

// mutateOptionsConfig creates a random variant of an options config.
func mutateOptionsConfig(base rules.OptionsRulesConfig, rng *rand.Rand) rules.OptionsRulesConfig {
	cfg := base // shallow copy

	// Deep copy leg selection.
	cfg.LegSelection = make(map[string]rules.LegSelector, len(base.LegSelection))
	for k, v := range base.LegSelection {
		// Mutate delta target.
		v.DeltaTarget = clamp(v.DeltaTarget+rng.Float64()*0.10-0.05, 0.05, 0.50)
		// Mutate DTE range.
		shift := rng.Intn(11) - 5
		v.DTEMin = max(7, v.DTEMin+shift)
		v.DTEMax = max(v.DTEMin+7, v.DTEMax+shift)
		cfg.LegSelection[k] = v
	}

	// Mutate management.
	if cfg.Management.CloseAtProfitPct > 0 {
		cfg.Management.CloseAtProfitPct = clamp(cfg.Management.CloseAtProfitPct*(0.8+rng.Float64()*0.4), 0.20, 0.90)
	}
	if cfg.Management.CloseAtDTE > 0 {
		cfg.Management.CloseAtDTE = max(1, cfg.Management.CloseAtDTE+rng.Intn(5)-2)
	}
	if cfg.Management.StopLossPct > 0 {
		cfg.Management.StopLossPct = clamp(cfg.Management.StopLossPct*(0.7+rng.Float64()*0.6), 0.5, 3.0)
	}

	// Mutate sizing.
	if cfg.PositionSizing.MaxRiskUSD > 0 {
		cfg.PositionSizing.MaxRiskUSD = cfg.PositionSizing.MaxRiskUSD * (0.7 + rng.Float64()*0.6)
	}

	// Deep copy conditions (mutate values).
	cfg.Entry = mutateConditionGroup(base.Entry, rng)
	cfg.Exit = mutateConditionGroup(base.Exit, rng)

	return cfg
}

func mutateConditionGroup(group rules.ConditionGroup, rng *rand.Rand) rules.ConditionGroup {
	out := group
	out.Conditions = make([]rules.Condition, len(group.Conditions))
	for i, c := range group.Conditions {
		out.Conditions[i] = c
		if c.Value != nil {
			mutated := *c.Value * (0.8 + rng.Float64()*0.4)
			out.Conditions[i].Value = &mutated
		}
	}
	return out
}
