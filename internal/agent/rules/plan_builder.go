package rules

import (
	"fmt"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/agent"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// BuildTradingPlan constructs a TradingPlan from the rules config, the current
// snapshot, and the signal direction. Equity is the current portfolio value used
// for position sizing.
func BuildTradingPlan(
	cfg *RulesEngineConfig,
	snap Snapshot,
	signal domain.PipelineSignal,
	ticker string,
	equity float64,
) agent.TradingPlan {
	if signal == domain.PipelineSignalHold {
		return agent.TradingPlan{
			Action:    signal,
			Ticker:    ticker,
			Rationale: "No entry or exit conditions met.",
		}
	}

	close := snap.Values["close"]
	atr := snap.Values["atr_14"]
	entryPrice := close

	stopLoss := computeStopLoss(cfg.StopLoss, snap, entryPrice, signal)
	takeProfit := computeTakeProfit(cfg.TakeProfit, entryPrice, stopLoss, signal)
	positionSize := computePositionSize(cfg.PositionSizing, equity, entryPrice, atr, stopLoss)
	riskDistance := math.Abs(entryPrice - stopLoss)
	rewardDistance := math.Abs(takeProfit - entryPrice)
	riskReward := 0.0
	if riskDistance > 0 {
		riskReward = rewardDistance / riskDistance
	}

	return agent.TradingPlan{
		Action:       signal,
		Ticker:       ticker,
		EntryType:    "market",
		EntryPrice:   entryPrice,
		PositionSize: positionSize,
		StopLoss:     stopLoss,
		TakeProfit:   takeProfit,
		TimeHorizon:  "swing",
		Confidence:   0.8,
		RiskReward:   riskReward,
		Rationale:    fmt.Sprintf("Rules engine: %s signal at %.2f, SL %.2f, TP %.2f", signal, entryPrice, stopLoss, takeProfit),
	}
}

func computeStopLoss(cfg StopLossConfig, snap Snapshot, entryPrice float64, signal domain.PipelineSignal) float64 {
	switch cfg.Method {
	case "fixed_pct":
		distance := entryPrice * cfg.Pct / 100
		if signal == domain.PipelineSignalBuy {
			return math.Max(0, entryPrice-distance)
		}
		return entryPrice + distance
	case "atr_multiple":
		atr := snap.Values["atr_14"]
		distance := atr * cfg.ATRMultiplier
		if signal == domain.PipelineSignalBuy {
			return math.Max(0, entryPrice-distance)
		}
		return entryPrice + distance
	case "indicator":
		if val, ok := snap.Values[cfg.IndicatorRef]; ok {
			return val
		}
		return math.Max(0, entryPrice*0.95)
	default:
		return math.Max(0, entryPrice*0.95)
	}
}

func computeTakeProfit(cfg TakeProfitConfig, entryPrice, stopLoss float64, signal domain.PipelineSignal) float64 {
	switch cfg.Method {
	case "fixed_pct":
		distance := entryPrice * cfg.Pct / 100
		if signal == domain.PipelineSignalBuy {
			return entryPrice + distance
		}
		return math.Max(0, entryPrice-distance)
	case "atr_multiple":
		riskDistance := math.Abs(entryPrice - stopLoss)
		distance := riskDistance * cfg.ATRMultiplier
		if signal == domain.PipelineSignalBuy {
			return entryPrice + distance
		}
		return math.Max(0, entryPrice-distance)
	case "risk_reward":
		riskDistance := math.Abs(entryPrice - stopLoss)
		distance := riskDistance * cfg.Ratio
		if signal == domain.PipelineSignalBuy {
			return entryPrice + distance
		}
		return math.Max(0, entryPrice-distance)
	default:
		return entryPrice * 1.05
	}
}

func computePositionSize(cfg SizingConfig, equity, entryPrice, atr, stopLoss float64) float64 {
	if entryPrice <= 0 {
		return 0
	}
	switch cfg.Method {
	case "fixed_fraction":
		return (equity * cfg.FractionPct / 100) / entryPrice
	case "atr_based":
		riskPerShare := atr * cfg.ATRMultiplier
		if riskPerShare <= 0 {
			return 0
		}
		riskAmount := equity * cfg.RiskPerTradePct / 100
		return riskAmount / riskPerShare
	case "fixed_amount":
		return cfg.FixedAmountUSD / entryPrice
	default:
		return (equity * 0.02) / entryPrice
	}
}
