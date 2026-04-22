package options

import (
	"context"
	"log/slog"
	"math"
	"math/rand"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/agent/rules"
	"github.com/PatrickFanella/get-rich-quick/internal/backtest"
	"github.com/PatrickFanella/get-rich-quick/internal/data"
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

// OptionsBacktestArtifacts captures the full outputs of a single synthetic options backtest.
type OptionsBacktestArtifacts struct {
	Metrics     backtest.Metrics
	Trades      []domain.Trade
	EquityCurve []backtest.EquityPoint
}

// OptionsSweepResult pairs an options config with backtest metrics.
type OptionsSweepResult struct {
	Label       string
	Config      rules.OptionsRulesConfig
	Metrics     backtest.Metrics
	Score       float64
	Trades      []domain.Trade
	EquityCurve []backtest.EquityPoint
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

	variants := make([]rules.OptionsRulesConfig, 0, cfg.Variations+1)
	variants = append(variants, baseConfig)
	for i := 0; i < cfg.Variations; i++ {
		variants = append(variants, mutateOptionsConfig(baseConfig, rng))
	}

	var bars []domain.OHLCV
	for _, b := range cfg.Bars {
		if !b.Timestamp.Before(cfg.StartDate) && !b.Timestamp.After(cfg.EndDate) {
			bars = append(bars, b)
		}
	}
	if len(bars) < 50 {
		return nil, nil
	}
	indicatorSnapshots := precomputeIndicatorSnapshots(bars)

	rv := realizedVol(cfg.Bars, 60)
	if rv < 0.05 {
		rv = 0.20
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

		artifacts := runOptionsBacktest(variant, bars, indicatorSnapshots, rv, cfg)
		score := discovery.ScoreMetrics(artifacts.Metrics, scoring)
		if len(artifacts.Trades) > 0 {
			score += 0.001
		}

		results = append(results, OptionsSweepResult{
			Label:       label,
			Config:      variant,
			Metrics:     artifacts.Metrics,
			Score:       score,
			Trades:      artifacts.Trades,
			EquityCurve: artifacts.EquityCurve,
		})
	}

	sort.Slice(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return len(results[i].Trades) > len(results[j].Trades)
		}
		return results[i].Score > results[j].Score
	})

	return results, nil
}

type optionsPosition struct {
	spread    *domain.OptionSpread
	entryBar  domain.OHLCV
	entryMid  float64
	maxProfit float64
	maxRisk   float64
}

func runOptionsBacktest(
	config rules.OptionsRulesConfig,
	bars []domain.OHLCV,
	indicatorSnapshots []map[string]float64,
	realizedVol float64,
	cfg OptionsSweepConfig,
) OptionsBacktestArtifacts {
	cash := cfg.InitialCash
	var position *optionsPosition
	chainCfg := backtest.DefaultSyntheticChainConfig()

	equityCurve := make([]backtest.EquityPoint, 0, len(bars))
	trades := make([]domain.Trade, 0, 16)
	var prevSnap *rules.Snapshot

	for i, bar := range bars {
		snap := rules.Snapshot{Values: buildOptionSignalValues(indicatorSnapshots[i], bar, position, realizedVol, chainCfg)}

		dte := avgDTE(config.LegSelection)
		chain := backtest.SynthesizeChain(bar.Close, realizedVol, dte, bar.Timestamp, chainCfg)
		optSnap := rules.NewOptionsSnapshot(snap, chain, nil, bar.Timestamp)

		marketValue := 0.0
		unrealizedPnL := 0.0
		if position != nil {
			marketValue = estimateSpreadValue(position, bar.Close, realizedVol, bar.Timestamp, chainCfg)
			unrealizedPnL = position.entryMid + marketValue
		}
		totalPnL := cash + marketValue - cfg.InitialCash

		equityCurve = append(equityCurve, backtest.EquityPoint{
			Timestamp:     bar.Timestamp,
			Cash:          cash,
			MarketValue:   marketValue,
			Equity:        cash + marketValue,
			UnrealizedPnL: unrealizedPnL,
			RealizedPnL:   totalPnL - unrealizedPnL,
			TotalPnL:      totalPnL,
		})

		if position == nil {
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
					cash -= maxRisk
					trades = append(trades, buildOptionsTrades(position, bar, true)...)
				}
			}
		} else {
			shouldClose, reason := checkManagement(position, config.Management, bar, realizedVol, chainCfg)
			if !shouldClose && rules.EvaluateGroup(config.Exit, optSnap.Snapshot, prevSnap) {
				shouldClose = true
				reason = "exit_signal"
			}

			if shouldClose {
				closeValue, pnl := closePosition(position, bar, realizedVol, chainCfg)
				cash += position.maxRisk + pnl
				trades = append(trades, buildOptionsCloseTrades(position, bar, closeValue, reason)...)
				position = nil
			}
		}

		clone := cloneSnapshot(optSnap.Snapshot)
		prevSnap = &clone
	}

	if position != nil && len(bars) > 0 {
		lastBar := bars[len(bars)-1]
		closeValue, pnl := closePosition(position, lastBar, realizedVol, chainCfg)
		cash += position.maxRisk + pnl
		trades = append(trades, buildOptionsCloseTrades(position, lastBar, closeValue, "final_bar")...)
		position = nil
		equityCurve[len(equityCurve)-1] = backtest.EquityPoint{
			Timestamp:     lastBar.Timestamp,
			Cash:          cash,
			MarketValue:   0,
			Equity:        cash,
			UnrealizedPnL: 0,
			RealizedPnL:   cash - cfg.InitialCash,
			TotalPnL:      cash - cfg.InitialCash,
		}
	}

	metrics := backtest.ComputeMetrics(equityCurve, bars)
	return OptionsBacktestArtifacts{
		Metrics:     metrics,
		Trades:      trades,
		EquityCurve: equityCurve,
	}
}

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

func spreadRiskReward(spread *domain.OptionSpread, netPremium float64) (maxProfit, maxRisk float64) {
	if len(spread.Legs) < 2 {
		return math.Abs(netPremium), math.Abs(netPremium) * 3
	}

	var strikes []float64
	for _, leg := range spread.Legs {
		strikes = append(strikes, leg.Contract.Strike)
	}
	sort.Float64s(strikes)
	width := (strikes[len(strikes)-1] - strikes[0]) * 100

	if netPremium > 0 {
		maxProfit = netPremium
		maxRisk = width - netPremium
	} else {
		maxProfit = width + netPremium
		maxRisk = -netPremium
	}

	if maxRisk < 0 {
		maxRisk = math.Abs(netPremium)
	}
	return maxProfit, maxRisk
}

func checkManagement(pos *optionsPosition, mgmt rules.OptionsManagement, bar domain.OHLCV, vol float64, chainCfg backtest.SyntheticChainConfig) (bool, string) {
	currentValue := estimateSpreadValue(pos, bar.Close, vol, bar.Timestamp, chainCfg)
	pnl := currentValue + pos.entryMid

	if mgmt.CloseAtProfitPct > 0 && pos.maxProfit > 0 {
		profitTargetRatio := mgmt.CloseAtProfitPct
		if profitTargetRatio > 1 {
			profitTargetRatio /= 100
		}
		if pnl >= pos.maxProfit*profitTargetRatio {
			return true, "profit_target"
		}
	}

	if mgmt.CloseAtDTE > 0 && len(pos.spread.Legs) > 0 {
		expiry := pos.spread.Legs[0].Contract.Expiry
		dte := int(expiry.Sub(bar.Timestamp).Hours() / 24)
		if dte <= mgmt.CloseAtDTE {
			return true, "dte_close"
		}
	}

	if mgmt.StopLossPct > 0 && pos.maxRisk > 0 {
		stopLossRatio := mgmt.StopLossPct
		if stopLossRatio > 1 {
			stopLossRatio /= 100
		}
		loss := -pnl
		if loss >= pos.maxRisk*stopLossRatio {
			return true, "stop_loss"
		}
	}

	return false, ""
}

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
			value -= legValue
		} else {
			value += legValue
		}
	}

	return value
}

func closePosition(pos *optionsPosition, bar domain.OHLCV, vol float64, chainCfg backtest.SyntheticChainConfig) (float64, float64) {
	currentValue := estimateSpreadValue(pos, bar.Close, vol, bar.Timestamp, chainCfg)
	return currentValue, pos.entryMid + currentValue
}

func buildOptionsTrades(pos *optionsPosition, bar domain.OHLCV, opening bool) []domain.Trade {
	if pos == nil || pos.spread == nil {
		return nil
	}
	trades := make([]domain.Trade, 0, len(pos.spread.Legs))
	for _, leg := range pos.spread.Legs {
		premium := legMarkPremium(leg, pos.entryBar.Close, bar.Close, pos.entryMid, len(pos.spread.Legs), opening)
		trades = append(trades, domain.Trade{
			ID:                 uuid.New(),
			Ticker:             leg.Contract.OCCSymbol,
			Side:               leg.Side,
			Quantity:           leg.Quantity,
			Price:              premium,
			ExecutedAt:         bar.Timestamp,
			CreatedAt:          bar.Timestamp,
			AssetClass:         domain.AssetClassOption,
			OpenClose:          openCloseValue(opening),
			ContractMultiplier: leg.Contract.Multiplier,
			Premium:            premium,
			Fee:                0,
		})
	}
	return trades
}

func buildOptionsCloseTrades(pos *optionsPosition, bar domain.OHLCV, closeValue float64, reason string) []domain.Trade {
	if pos == nil || pos.spread == nil {
		return nil
	}
	trades := make([]domain.Trade, 0, len(pos.spread.Legs))
	for _, leg := range pos.spread.Legs {
		closeSide := domain.OrderSideBuy
		if leg.Side == domain.OrderSideBuy {
			closeSide = domain.OrderSideSell
		}
		premium := legMarkPremium(leg, pos.entryBar.Close, bar.Close, closeValue, len(pos.spread.Legs), false)
		trades = append(trades, domain.Trade{
			ID:                 uuid.New(),
			Ticker:             leg.Contract.OCCSymbol,
			Side:               closeSide,
			Quantity:           leg.Quantity,
			Price:              premium,
			ExecutedAt:         bar.Timestamp,
			CreatedAt:          bar.Timestamp,
			AssetClass:         domain.AssetClassOption,
			OpenClose:          openCloseValue(false),
			ContractMultiplier: leg.Contract.Multiplier,
			Premium:            premium,
			ExitReason:         reason,
			Fee:                0,
		})
	}
	return trades
}

func legMarkPremium(leg domain.SpreadLeg, entryUnderlying, currentUnderlying, netValue float64, legCount int, opening bool) float64 {
	multiplier := leg.Contract.Multiplier
	if multiplier <= 0 {
		multiplier = 100
	}
	if legCount < 1 {
		legCount = 1
	}
	base := math.Abs(netValue) / float64(legCount) / multiplier
	if base > 0 {
		return base
	}
	intrinsic := 0.0
	switch leg.Contract.OptionType {
	case domain.OptionTypeCall:
		if currentUnderlying > leg.Contract.Strike {
			intrinsic = currentUnderlying - leg.Contract.Strike
		}
	case domain.OptionTypePut:
		if leg.Contract.Strike > currentUnderlying {
			intrinsic = leg.Contract.Strike - currentUnderlying
		}
	}
	timeValue := math.Max(0.1, math.Abs(currentUnderlying-entryUnderlying)*0.02)
	if !opening {
		timeValue = math.Max(0.05, timeValue*0.5)
	}
	return intrinsic + timeValue
}

func openCloseValue(opening bool) string {
	if opening {
		return "open"
	}
	return "close"
}

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

func mutateOptionsConfig(base rules.OptionsRulesConfig, rng *rand.Rand) rules.OptionsRulesConfig {
	cfg := base

	cfg.LegSelection = make(map[string]rules.LegSelector, len(base.LegSelection))
	for k, v := range base.LegSelection {
		v.DeltaTarget = clampFloat(v.DeltaTarget+rng.Float64()*0.10-0.05, 0.05, 0.50)
		shift := rng.Intn(11) - 5
		v.DTEMin = maxInt(7, v.DTEMin+shift)
		v.DTEMax = maxInt(v.DTEMin+7, v.DTEMax+shift)
		cfg.LegSelection[k] = v
	}

	if cfg.Management.CloseAtProfitPct > 0 {
		cfg.Management.CloseAtProfitPct = clampFloat(cfg.Management.CloseAtProfitPct*(0.8+rng.Float64()*0.4), 0.20, 90.0)
	}
	if cfg.Management.CloseAtDTE > 0 {
		cfg.Management.CloseAtDTE = maxInt(1, cfg.Management.CloseAtDTE+rng.Intn(5)-2)
	}
	if cfg.Management.StopLossPct > 0 {
		cfg.Management.StopLossPct = clampFloat(cfg.Management.StopLossPct*(0.7+rng.Float64()*0.6), 0.5, 300.0)
	}

	if cfg.PositionSizing.MaxRiskUSD > 0 {
		cfg.PositionSizing.MaxRiskUSD = cfg.PositionSizing.MaxRiskUSD * (0.7 + rng.Float64()*0.6)
	}

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

func precomputeIndicatorSnapshots(bars []domain.OHLCV) []map[string]float64 {
	snapshots := make([]map[string]float64, len(bars))
	for i := range bars {
		snapshot := make(map[string]float64)
		for _, indicator := range data.IndicatorSnapshotFromBars(bars[:i+1]) {
			snapshot[indicator.Name] = indicator.Value
		}
		snapshots[i] = snapshot
	}
	return snapshots
}

func buildOptionSignalValues(indicators map[string]float64, bar domain.OHLCV, pos *optionsPosition, rv float64, chainCfg backtest.SyntheticChainConfig) map[string]float64 {
	values := map[string]float64{
		"close":  bar.Close,
		"open":   bar.Open,
		"high":   bar.High,
		"low":    bar.Low,
		"volume": bar.Volume,
	}
	for name, value := range indicators {
		values[name] = value
	}

	dte := 30
	if pos != nil && pos.spread != nil && len(pos.spread.Legs) > 0 {
		expiry := pos.spread.Legs[0].Contract.Expiry
		dte = int(expiry.Sub(bar.Timestamp).Hours() / 24)
		if dte < 0 {
			dte = 0
		}
		currentValue := estimateSpreadValue(pos, bar.Close, rv, bar.Timestamp, chainCfg)
		if pos.maxRisk > 0 {
			values["pnl_pct"] = ((pos.entryMid + currentValue) / pos.maxRisk) * 100
		} else {
			values["pnl_pct"] = 0
		}
		values["dte"] = float64(dte)
	}

	chain := backtest.SynthesizeChain(bar.Close, rv, dte, bar.Timestamp, chainCfg)
	atmIV, putCallRatio := chainMetrics(chain, bar.Close)
	values["atm_iv"] = atmIV
	values["put_call_ratio"] = putCallRatio

	ivRank := 0.0
	if rv > 0 && atmIV > 0 {
		minVol := rv * 0.7
		maxVol := rv * 1.5
		if maxVol > minVol {
			ivRank = clampFloat((atmIV-minVol)/(maxVol-minVol)*100, 0, 100)
		}
	}
	values["iv_rank"] = ivRank
	values["iv_percentile"] = ivRank
	return values
}

func cloneSnapshot(s rules.Snapshot) rules.Snapshot {
	values := make(map[string]float64, len(s.Values))
	for k, v := range s.Values {
		values[k] = v
	}
	return rules.Snapshot{Values: values}
}

func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
