package rules

import (
	"strings"
	"testing"
)

func validConfig() *RulesEngineConfig {
	return &RulesEngineConfig{
		Version: 1,
		Entry: ConditionGroup{
			Operator:   "AND",
			Conditions: []Condition{{Field: "rsi_14", Op: "lt", Value: fp(30)}},
		},
		Exit: ConditionGroup{
			Operator:   "OR",
			Conditions: []Condition{{Field: "rsi_14", Op: "gt", Value: fp(70)}},
		},
		PositionSizing: SizingConfig{Method: "fixed_fraction", FractionPct: 5},
		StopLoss:       StopLossConfig{Method: "fixed_pct", Pct: 2},
		TakeProfit:     TakeProfitConfig{Method: "risk_reward", Ratio: 2.5},
	}
}

func TestValidate_ValidConfig(t *testing.T) {
	t.Parallel()
	if err := Validate(validConfig()); err != nil {
		t.Fatalf("valid config should pass: %v", err)
	}
}

func TestValidate_NilConfig(t *testing.T) {
	t.Parallel()
	if err := Validate(nil); err == nil {
		t.Fatal("nil config should fail")
	}
}

func TestValidate_BadVersion(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Version = 0
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "version") {
		t.Fatalf("version 0 should fail with version error, got: %v", err)
	}
}

func TestValidate_UnknownField(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Entry.Conditions[0].Field = "fake_indicator"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("unknown field should fail, got: %v", err)
	}
}

func TestValidate_UnknownOperator(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Entry.Conditions[0].Op = "between"
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "unknown operator") {
		t.Fatalf("unknown op should fail, got: %v", err)
	}
}

func TestValidate_MutuallyExclusiveValueRef(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Entry.Conditions[0].Ref = "sma_200"
	// Value is already set — both present
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("value+ref should fail, got: %v", err)
	}
}

func TestValidate_EmptyConditions(t *testing.T) {
	t.Parallel()
	cfg := validConfig()
	cfg.Entry.Conditions = nil
	if err := Validate(cfg); err == nil || !strings.Contains(err.Error(), "at least one") {
		t.Fatalf("empty conditions should fail, got: %v", err)
	}
}

func TestValidate_SizingMethods(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		sizing SizingConfig
		errStr string
	}{
		{"unknown method", SizingConfig{Method: "magic"}, "unknown method"},
		{"atr no risk", SizingConfig{Method: "atr_based", ATRMultiplier: 1}, "risk_per_trade_pct"},
		{"atr no multiplier", SizingConfig{Method: "atr_based", RiskPerTradePct: 2}, "atr_multiplier"},
		{"fraction zero", SizingConfig{Method: "fixed_fraction"}, "fraction_pct"},
		{"amount zero", SizingConfig{Method: "fixed_amount"}, "fixed_amount_usd"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.PositionSizing = tc.sizing
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.errStr) {
				t.Errorf("expected error containing %q, got: %v", tc.errStr, err)
			}
		})
	}
}

func TestValidate_StopLossMethods(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		sl     StopLossConfig
		errStr string
	}{
		{"unknown", StopLossConfig{Method: "magic"}, "unknown method"},
		{"pct zero", StopLossConfig{Method: "fixed_pct"}, "pct"},
		{"atr zero", StopLossConfig{Method: "atr_multiple"}, "atr_multiplier"},
		{"indicator unknown", StopLossConfig{Method: "indicator", IndicatorRef: "fake"}, "unknown field"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := validConfig()
			cfg.StopLoss = tc.sl
			err := Validate(cfg)
			if err == nil || !strings.Contains(err.Error(), tc.errStr) {
				t.Errorf("expected error containing %q, got: %v", tc.errStr, err)
			}
		})
	}
}
