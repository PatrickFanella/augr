package risk

import (
	"fmt"
	"math"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type OptionsLimits struct {
	MaxAbsDeltaPctEquity      float64
	MaxGammaNotionalPctEquity float64
	MaxVegaPctEquity          float64
	MaxDailyThetaPctEquity    float64
}

func DefaultOptionsLimits() OptionsLimits {
	return OptionsLimits{MaxAbsDeltaPctEquity: 0.20, MaxGammaNotionalPctEquity: 0.05, MaxVegaPctEquity: 0.10, MaxDailyThetaPctEquity: 0.02}
}

type OptionLegExposure struct {
	Greeks     domain.OptionGreeks
	Side       domain.OrderSide
	Quantity   float64
	Multiplier float64
}

type OptionsExposure struct {
	DeltaPctEquity      float64
	GammaPctEquity      float64
	VegaPctEquity       float64
	DailyThetaPctEquity float64
}

// CheckOptionsExposure applies deterministic portfolio Greek caps. Gamma is
// expressed as P&L sensitivity to a one-percent underlying move.
func CheckOptionsExposure(limits OptionsLimits, equity, underlyingPrice float64, existing []domain.Position, proposed []OptionLegExposure) (OptionsExposure, bool, string) {
	if equity <= 0 || underlyingPrice <= 0 {
		return OptionsExposure{}, false, "options risk: positive equity and underlying price are required"
	}
	var delta, gamma, vega, theta float64
	accumulate := func(g domain.OptionGreeks, sign, quantity, multiplier float64) {
		if multiplier <= 0 {
			multiplier = 100
		}
		delta += sign * g.Delta * quantity * multiplier * underlyingPrice
		gamma += sign * g.Gamma * quantity * multiplier * underlyingPrice * underlyingPrice * 0.01
		vega += sign * g.Vega * quantity * multiplier
		theta += sign * g.Theta * quantity * multiplier
	}
	for _, position := range existing {
		if position.AssetClass != domain.AssetClassOption || position.ClosedAt != nil || position.Quantity <= 0 || position.Delta == nil || position.Gamma == nil || position.Theta == nil || position.Vega == nil {
			continue
		}
		sign := 1.0
		if position.Side == domain.PositionSideShort {
			sign = -1
		}
		accumulate(domain.OptionGreeks{Delta: *position.Delta, Gamma: *position.Gamma, Theta: *position.Theta, Vega: *position.Vega}, sign, position.Quantity, position.ContractMultiplier)
	}
	for _, leg := range proposed {
		sign := 1.0
		if leg.Side == domain.OrderSideSell {
			sign = -1
		}
		accumulate(leg.Greeks, sign, leg.Quantity, leg.Multiplier)
	}
	exposure := OptionsExposure{DeltaPctEquity: math.Abs(delta) / equity, GammaPctEquity: math.Abs(gamma) / equity, VegaPctEquity: math.Abs(vega) / equity, DailyThetaPctEquity: math.Abs(theta) / equity}
	checks := []struct {
		name         string
		value, limit float64
	}{{"delta", exposure.DeltaPctEquity, limits.MaxAbsDeltaPctEquity}, {"gamma", exposure.GammaPctEquity, limits.MaxGammaNotionalPctEquity}, {"vega", exposure.VegaPctEquity, limits.MaxVegaPctEquity}, {"daily theta", exposure.DailyThetaPctEquity, limits.MaxDailyThetaPctEquity}}
	for _, check := range checks {
		if check.limit > 0 && check.value > check.limit {
			return exposure, false, fmt.Sprintf("options risk: %s exposure %.2f%% exceeds %.2f%% of equity", check.name, check.value*100, check.limit*100)
		}
	}
	return exposure, true, ""
}
