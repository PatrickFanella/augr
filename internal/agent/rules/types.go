package rules

// RulesEngineConfig is the top-level JSON config that the LLM generates.
// It defines deterministic entry/exit conditions, position sizing, and
// risk parameters that can be evaluated per bar without LLM calls.
type RulesEngineConfig struct {
	Version        int              `json:"version"`
	Name           string           `json:"name,omitempty"`
	Description    string           `json:"description,omitempty"`
	Entry          ConditionGroup   `json:"entry"`
	Exit           ConditionGroup   `json:"exit"`
	PositionSizing SizingConfig     `json:"position_sizing"`
	StopLoss       StopLossConfig   `json:"stop_loss"`
	TakeProfit     TakeProfitConfig `json:"take_profit"`
	Filters        *FilterConfig    `json:"filters,omitempty"`
}

// ConditionGroup is an AND/OR group of conditions.
type ConditionGroup struct {
	Operator   string      `json:"operator"` // "AND" or "OR"
	Conditions []Condition `json:"conditions"`
}

// Condition compares a field against a literal value or another field.
type Condition struct {
	Field string   `json:"field"`           // indicator name or OHLCV field
	Op    string   `json:"op"`              // gt, gte, lt, lte, eq, cross_above, cross_below
	Value *float64 `json:"value,omitempty"` // literal comparand
	Ref   string   `json:"ref,omitempty"`   // reference to another field (mutually exclusive with Value)
}

// SizingConfig defines how position size is calculated.
type SizingConfig struct {
	Method          string  `json:"method"`                       // "fixed_fraction", "atr_based", "fixed_amount"
	RiskPerTradePct float64 `json:"risk_per_trade_pct,omitempty"` // for atr_based: % of equity risked per trade
	ATRMultiplier   float64 `json:"atr_multiplier,omitempty"`     // for atr_based: ATR units for risk distance
	FixedAmountUSD  float64 `json:"fixed_amount_usd,omitempty"`   // for fixed_amount
	FractionPct     float64 `json:"fraction_pct,omitempty"`       // for fixed_fraction: % of equity
}

// StopLossConfig defines how stop-loss is calculated.
type StopLossConfig struct {
	Method        string  `json:"method"`                    // "fixed_pct", "atr_multiple", "indicator"
	Pct           float64 `json:"pct,omitempty"`             // for fixed_pct
	ATRMultiplier float64 `json:"atr_multiplier,omitempty"`  // for atr_multiple
	IndicatorRef  string  `json:"indicator_ref,omitempty"`   // for indicator (e.g. "bollinger_lower")
}

// TakeProfitConfig defines how take-profit is calculated.
type TakeProfitConfig struct {
	Method        string  `json:"method"`                   // "fixed_pct", "atr_multiple", "risk_reward"
	Pct           float64 `json:"pct,omitempty"`            // for fixed_pct
	ATRMultiplier float64 `json:"atr_multiplier,omitempty"` // for atr_multiple
	Ratio         float64 `json:"ratio,omitempty"`          // for risk_reward
}

// FilterConfig defines pre-conditions that must be met before any signal.
type FilterConfig struct {
	MinVolume float64 `json:"min_volume,omitempty"`
	MinATR    float64 `json:"min_atr,omitempty"`
}
