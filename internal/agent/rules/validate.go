package rules

import (
	"fmt"
	"strings"
)

// KnownFields is the set of indicator and OHLCV field names that conditions
// may reference.
var KnownFields = map[string]bool{
	// 21 indicators
	"sma_20": true, "sma_50": true, "sma_200": true,
	"ema_12": true, "rsi_14": true, "mfi_14": true,
	"williams_r_14": true, "cci_20": true, "roc_12": true,
	"atr_14": true, "vwma_20": true, "obv": true, "adl": true,
	"macd_line": true, "macd_signal": true, "macd_histogram": true,
	"stochastic_k": true, "stochastic_d": true,
	"bollinger_upper": true, "bollinger_middle": true, "bollinger_lower": true,
	// OHLCV fields
	"close": true, "open": true, "high": true, "low": true, "volume": true,
}

var knownOperators = map[string]bool{
	"gt": true, "gte": true, "lt": true, "lte": true, "eq": true,
	"cross_above": true, "cross_below": true,
}

var knownSizingMethods = map[string]bool{
	"fixed_fraction": true, "atr_based": true, "fixed_amount": true,
}

var knownStopMethods = map[string]bool{
	"fixed_pct": true, "atr_multiple": true, "indicator": true,
}

var knownTakeProfitMethods = map[string]bool{
	"fixed_pct": true, "atr_multiple": true, "risk_reward": true,
}

// Validate checks that a RulesEngineConfig is well-formed.
func Validate(cfg *RulesEngineConfig) error {
	if cfg == nil {
		return fmt.Errorf("rules: config is nil")
	}
	if cfg.Version < 1 {
		return fmt.Errorf("rules: version must be >= 1")
	}
	if err := validateGroup("entry", cfg.Entry); err != nil {
		return err
	}
	if err := validateGroup("exit", cfg.Exit); err != nil {
		return err
	}
	if err := validateSizing(cfg.PositionSizing); err != nil {
		return err
	}
	if err := validateStopLoss(cfg.StopLoss); err != nil {
		return err
	}
	if err := validateTakeProfit(cfg.TakeProfit); err != nil {
		return err
	}
	return nil
}

func validateGroup(context string, g ConditionGroup) error {
	op := strings.ToUpper(g.Operator)
	if op != "AND" && op != "OR" {
		return fmt.Errorf("rules: %s.operator must be AND or OR, got %q", context, g.Operator)
	}
	if len(g.Conditions) == 0 {
		return fmt.Errorf("rules: %s.conditions must have at least one condition", context)
	}
	for i, c := range g.Conditions {
		if err := validateCondition(fmt.Sprintf("%s.conditions[%d]", context, i), c); err != nil {
			return err
		}
	}
	return nil
}

func validateCondition(context string, c Condition) error {
	if !KnownFields[c.Field] {
		return fmt.Errorf("rules: %s.field: unknown field %q", context, c.Field)
	}
	if !knownOperators[c.Op] {
		return fmt.Errorf("rules: %s.op: unknown operator %q", context, c.Op)
	}
	if c.Value == nil && c.Ref == "" {
		return fmt.Errorf("rules: %s: must have value or ref", context)
	}
	if c.Value != nil && c.Ref != "" {
		return fmt.Errorf("rules: %s: value and ref are mutually exclusive", context)
	}
	if c.Ref != "" && !KnownFields[c.Ref] {
		return fmt.Errorf("rules: %s.ref: unknown field %q", context, c.Ref)
	}
	return nil
}

func validateSizing(s SizingConfig) error {
	if !knownSizingMethods[s.Method] {
		return fmt.Errorf("rules: position_sizing.method: unknown method %q", s.Method)
	}
	switch s.Method {
	case "atr_based":
		if s.RiskPerTradePct <= 0 {
			return fmt.Errorf("rules: position_sizing.risk_per_trade_pct must be > 0")
		}
		if s.ATRMultiplier <= 0 {
			return fmt.Errorf("rules: position_sizing.atr_multiplier must be > 0")
		}
	case "fixed_fraction":
		if s.FractionPct <= 0 {
			return fmt.Errorf("rules: position_sizing.fraction_pct must be > 0")
		}
	case "fixed_amount":
		if s.FixedAmountUSD <= 0 {
			return fmt.Errorf("rules: position_sizing.fixed_amount_usd must be > 0")
		}
	}
	return nil
}

func validateStopLoss(s StopLossConfig) error {
	if !knownStopMethods[s.Method] {
		return fmt.Errorf("rules: stop_loss.method: unknown method %q", s.Method)
	}
	switch s.Method {
	case "fixed_pct":
		if s.Pct <= 0 {
			return fmt.Errorf("rules: stop_loss.pct must be > 0")
		}
	case "atr_multiple":
		if s.ATRMultiplier <= 0 {
			return fmt.Errorf("rules: stop_loss.atr_multiplier must be > 0")
		}
	case "indicator":
		if !KnownFields[s.IndicatorRef] {
			return fmt.Errorf("rules: stop_loss.indicator_ref: unknown field %q", s.IndicatorRef)
		}
	}
	return nil
}

func validateTakeProfit(t TakeProfitConfig) error {
	if !knownTakeProfitMethods[t.Method] {
		return fmt.Errorf("rules: take_profit.method: unknown method %q", t.Method)
	}
	switch t.Method {
	case "fixed_pct":
		if t.Pct <= 0 {
			return fmt.Errorf("rules: take_profit.pct must be > 0")
		}
	case "atr_multiple":
		if t.ATRMultiplier <= 0 {
			return fmt.Errorf("rules: take_profit.atr_multiplier must be > 0")
		}
	case "risk_reward":
		if t.Ratio <= 0 {
			return fmt.Errorf("rules: take_profit.ratio must be > 0")
		}
	}
	return nil
}
