package rules

import (
	"fmt"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// knownStrategyTypes enumerates supported option strategies.
var knownStrategyTypes = map[domain.OptionStrategyType]bool{
	domain.StrategyLongCall:       true,
	domain.StrategyLongPut:        true,
	domain.StrategyCoveredCall:    true,
	domain.StrategyCashSecuredPut: true,
	domain.StrategyBullCallSpread: true,
	domain.StrategyBearPutSpread:  true,
	domain.StrategyBullPutSpread:  true,
	domain.StrategyBearCallSpread: true,
	domain.StrategyIronCondor:     true,
	domain.StrategyIronButterfly:  true,
	domain.StrategyLongStraddle:   true,
	domain.StrategyLongStrangle:   true,
	domain.StrategyShortStraddle:  true,
	domain.StrategyShortStrangle:  true,
	domain.StrategyCalendarSpread: true,
	domain.StrategyDiagonalSpread: true,
}

// knownOptionsSizingMethods enumerates valid sizing methods for options.
var knownOptionsSizingMethods = map[string]bool{
	"max_risk":        true,
	"fixed_contracts": true,
	"premium_budget":  true,
}

// ValidateOptions checks that an OptionsRulesConfig is well-formed.
func ValidateOptions(cfg *OptionsRulesConfig) error {
	if cfg == nil {
		return fmt.Errorf("options_rules: config is nil")
	}
	if cfg.Version < 1 {
		return fmt.Errorf("options_rules: version must be >= 1")
	}
	if !knownStrategyTypes[cfg.StrategyType] {
		return fmt.Errorf("options_rules: unknown strategy_type %q", cfg.StrategyType)
	}
	if strings.TrimSpace(cfg.Underlying) == "" {
		return fmt.Errorf("options_rules: underlying must be non-empty")
	}

	// Entry/exit reuse the generic condition group validator.
	if err := validateGroup("entry", cfg.Entry); err != nil {
		return err
	}
	if err := validateGroup("exit", cfg.Exit); err != nil {
		return err
	}

	// Leg selectors.
	if len(cfg.LegSelection) == 0 {
		return fmt.Errorf("options_rules: leg_selection must have at least one leg")
	}
	for name, sel := range cfg.LegSelection {
		if err := validateLegSelector(name, sel); err != nil {
			return err
		}
	}

	// Sizing.
	if err := validateOptionsSizing(cfg.PositionSizing); err != nil {
		return err
	}

	// Management.
	if err := validateOptionsManagement(cfg.Management); err != nil {
		return err
	}

	return nil
}

func validateLegSelector(name string, sel LegSelector) error {
	if sel.OptionType != domain.OptionTypeCall && sel.OptionType != domain.OptionTypePut {
		return fmt.Errorf("options_rules: leg %q: option_type must be %q or %q, got %q",
			name, domain.OptionTypeCall, domain.OptionTypePut, sel.OptionType)
	}
	if sel.DeltaTarget < 0 || sel.DeltaTarget > 1 {
		return fmt.Errorf("options_rules: leg %q: delta_target must be in [0, 1], got %v",
			name, sel.DeltaTarget)
	}
	if sel.DTEMin < 0 {
		return fmt.Errorf("options_rules: leg %q: dte_min must be >= 0", name)
	}
	if sel.DTEMax < sel.DTEMin {
		return fmt.Errorf("options_rules: leg %q: dte_max (%d) must be >= dte_min (%d)",
			name, sel.DTEMax, sel.DTEMin)
	}
	if sel.Side != domain.OrderSideBuy && sel.Side != domain.OrderSideSell {
		return fmt.Errorf("options_rules: leg %q: side must be %q or %q, got %q",
			name, domain.OrderSideBuy, domain.OrderSideSell, sel.Side)
	}
	if sel.Ratio < 0 {
		return fmt.Errorf("options_rules: leg %q: ratio must be >= 0", name)
	}
	return nil
}

func validateOptionsSizing(s OptionsSizingConfig) error {
	if !knownOptionsSizingMethods[s.Method] {
		return fmt.Errorf("options_rules: position_sizing.method: unknown method %q", s.Method)
	}
	switch s.Method {
	case "max_risk":
		if s.MaxRiskUSD <= 0 {
			return fmt.Errorf("options_rules: position_sizing.max_risk_usd must be > 0")
		}
	case "fixed_contracts":
		if s.FixedContracts <= 0 {
			return fmt.Errorf("options_rules: position_sizing.fixed_contracts must be > 0")
		}
	case "premium_budget":
		if s.PremiumBudget <= 0 {
			return fmt.Errorf("options_rules: position_sizing.premium_budget must be > 0")
		}
	}
	return nil
}

func validateOptionsManagement(m OptionsManagement) error {
	if m.CloseAtProfitPct < 0 || m.CloseAtProfitPct > 100 {
		return fmt.Errorf("options_rules: management.close_at_profit_pct must be in [0, 100], got %v", m.CloseAtProfitPct)
	}
	if m.CloseAtDTE < 0 {
		return fmt.Errorf("options_rules: management.close_at_dte must be >= 0")
	}
	if m.RollAtDTE < 0 {
		return fmt.Errorf("options_rules: management.roll_at_dte must be >= 0")
	}
	if m.StopLossPct < 0 || m.StopLossPct > 100 {
		return fmt.Errorf("options_rules: management.stop_loss_pct must be in [0, 100], got %v", m.StopLossPct)
	}
	return nil
}
