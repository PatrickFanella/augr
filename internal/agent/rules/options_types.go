package rules

import "github.com/PatrickFanella/get-rich-quick/internal/domain"

// OptionsRulesConfig defines a rules-based options strategy.
type OptionsRulesConfig struct {
	Version        int                       `json:"version"`
	StrategyType   domain.OptionStrategyType `json:"strategy_type"`
	Underlying     string                    `json:"underlying"`
	Entry          ConditionGroup            `json:"entry"`         // reuse existing type
	Exit           ConditionGroup            `json:"exit"`          // reuse existing type
	LegSelection   map[string]LegSelector    `json:"leg_selection"` // keyed by leg name
	PositionSizing OptionsSizingConfig       `json:"position_sizing"`
	Management     OptionsManagement         `json:"management"`
}

// LegSelector describes how to pick a specific contract for one leg.
type LegSelector struct {
	OptionType  domain.OptionType     `json:"option_type"`  // "call" or "put"
	DeltaTarget float64               `json:"delta_target"` // e.g. 0.16 for short call
	DTEMin      int                   `json:"dte_min"`      // minimum days to expiry
	DTEMax      int                   `json:"dte_max"`      // maximum days to expiry
	Side        domain.OrderSide      `json:"side"`         // "buy" or "sell"
	Intent      domain.PositionIntent `json:"position_intent"`
	Ratio       int                   `json:"ratio"` // leg ratio (usually 1)
}

// OptionsSizingConfig defines position sizing for options.
type OptionsSizingConfig struct {
	Method         string  `json:"method"` // "max_risk", "fixed_contracts", "premium_budget"
	MaxRiskUSD     float64 `json:"max_risk_usd,omitempty"`
	FixedContracts int     `json:"fixed_contracts,omitempty"`
	PremiumBudget  float64 `json:"premium_budget,omitempty"`
}

// OptionsManagement defines automated position management rules.
type OptionsManagement struct {
	CloseAtProfitPct float64 `json:"close_at_profit_pct,omitempty"` // close at X% of max profit
	CloseAtDTE       int     `json:"close_at_dte,omitempty"`        // close at N days to expiry
	RollAtDTE        int     `json:"roll_at_dte,omitempty"`         // roll at N DTE
	StopLossPct      float64 `json:"stop_loss_pct,omitempty"`       // close at X% loss
}
