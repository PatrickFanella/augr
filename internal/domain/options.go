package domain

import "time"

// OptionType identifies a contract as a call or put.
type OptionType string

const (
	OptionTypeCall OptionType = "call"
	OptionTypePut  OptionType = "put"
)

// PositionIntent describes the open/close direction of an options trade.
type PositionIntent string

const (
	PositionIntentBuyToOpen   PositionIntent = "buy_to_open"
	PositionIntentSellToOpen  PositionIntent = "sell_to_open"
	PositionIntentBuyToClose  PositionIntent = "buy_to_close"
	PositionIntentSellToClose PositionIntent = "sell_to_close"
)

// AssetClass discriminates between equity and options instruments.
type AssetClass string

const (
	AssetClassEquity AssetClass = "equity"
	AssetClassOption AssetClass = "option"
)

// OptionContract describes a single options contract.
type OptionContract struct {
	OCCSymbol  string     `json:"occ_symbol"`
	Underlying string     `json:"underlying"`
	OptionType OptionType `json:"option_type"`
	Strike     float64    `json:"strike"`
	Expiry     time.Time  `json:"expiry"`
	Multiplier float64    `json:"multiplier"`
	Style      string     `json:"style,omitempty"` // "american" or "european"
}

// OptionGreeks holds the sensitivity measures for an options contract.
type OptionGreeks struct {
	Delta float64 `json:"delta"`
	Gamma float64 `json:"gamma"`
	Theta float64 `json:"theta"`
	Vega  float64 `json:"vega"`
	Rho   float64 `json:"rho,omitempty"`
	IV    float64 `json:"iv"`
}

// OptionSnapshot is a point-in-time view of a contract including price and Greeks.
type OptionSnapshot struct {
	Contract     OptionContract `json:"contract"`
	Greeks       OptionGreeks   `json:"greeks"`
	Bid          float64        `json:"bid"`
	Ask          float64        `json:"ask"`
	Mid          float64        `json:"mid"`
	Last         float64        `json:"last"`
	Volume       float64        `json:"volume"`
	OpenInterest float64        `json:"open_interest"`
}

// SpreadLeg is one leg of a multi-leg options spread.
type SpreadLeg struct {
	Contract       OptionContract `json:"contract"`
	Side           OrderSide      `json:"side"`
	PositionIntent PositionIntent `json:"position_intent"`
	Ratio          int            `json:"ratio"`
	Quantity       float64        `json:"quantity"`
}

// OptionStrategyType identifies a named options strategy.
type OptionStrategyType string

const (
	StrategyLongCall       OptionStrategyType = "long_call"
	StrategyLongPut        OptionStrategyType = "long_put"
	StrategyCoveredCall    OptionStrategyType = "covered_call"
	StrategyCashSecuredPut OptionStrategyType = "cash_secured_put"
	StrategyBullCallSpread OptionStrategyType = "bull_call_spread"
	StrategyBearPutSpread  OptionStrategyType = "bear_put_spread"
	StrategyBullPutSpread  OptionStrategyType = "bull_put_spread"
	StrategyBearCallSpread OptionStrategyType = "bear_call_spread"
	StrategyIronCondor     OptionStrategyType = "iron_condor"
	StrategyIronButterfly  OptionStrategyType = "iron_butterfly"
	StrategyLongStraddle   OptionStrategyType = "long_straddle"
	StrategyLongStrangle   OptionStrategyType = "long_strangle"
	StrategyShortStraddle  OptionStrategyType = "short_straddle"
	StrategyShortStrangle  OptionStrategyType = "short_strangle"
	StrategyCalendarSpread OptionStrategyType = "calendar_spread"
	StrategyDiagonalSpread OptionStrategyType = "diagonal_spread"
)

// OptionSpread describes a multi-leg options position.
type OptionSpread struct {
	StrategyType OptionStrategyType `json:"strategy_type"`
	Underlying   string             `json:"underlying"`
	Legs         []SpreadLeg        `json:"legs"`
	MaxRisk      float64            `json:"max_risk"`
	MaxReward    float64            `json:"max_reward"`
}
