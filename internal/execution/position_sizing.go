package execution

// PositionSizingMethod identifies the supported position sizing approaches.
type PositionSizingMethod string

const (
	PositionSizingMethodATR             PositionSizingMethod = "atr"
	PositionSizingMethodKelly           PositionSizingMethod = "kelly"
	PositionSizingMethodFixedFractional PositionSizingMethod = "fixed_fractional"
)

// PositionSizingParams contains the inputs needed by the position sizing dispatcher.
type PositionSizingParams struct {
	AccountValue  float64
	RiskPct       float64
	ATR           float64
	Multiplier    float64
	WinRate       float64
	WinLossRatio  float64
	FractionPct   float64
	PricePerShare float64
	HalfKelly     bool
}

// ATRPositionSize calculates the number of shares based on ATR stop distance.
func ATRPositionSize(accountValue, riskPct, atr, multiplier float64) float64 {
	if atr <= 0 || multiplier <= 0 {
		return 0
	}

	return (accountValue * riskPct) / (atr * multiplier)
}

// KellyPositionSize calculates the account value allocation using the Kelly criterion.
func KellyPositionSize(accountValue, winRate, winLossRatio float64) float64 {
	if winLossRatio <= 0 {
		return 0
	}

	fraction := winRate - (1-winRate)/winLossRatio
	if fraction <= 0 {
		return 0
	}

	return accountValue * fraction
}

// FixedFractionalSize calculates the number of shares to buy using a fixed account fraction.
func FixedFractionalSize(accountValue, fractionPct, pricePerShare float64) float64 {
	if pricePerShare <= 0 {
		return 0
	}

	return (accountValue * fractionPct) / pricePerShare
}

// CalculatePositionSize dispatches to the configured position sizing method.
func CalculatePositionSize(method PositionSizingMethod, params PositionSizingParams) float64 {
	switch method {
	case PositionSizingMethodATR:
		return ATRPositionSize(params.AccountValue, params.RiskPct, params.ATR, params.Multiplier)
	case PositionSizingMethodKelly:
		size := KellyPositionSize(params.AccountValue, params.WinRate, params.WinLossRatio)
		if params.HalfKelly {
			return size * 0.5
		}

		return size
	case PositionSizingMethodFixedFractional:
		return FixedFractionalSize(params.AccountValue, params.FractionPct, params.PricePerShare)
	default:
		return 0
	}
}
