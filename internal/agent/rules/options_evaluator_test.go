package rules

import (
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func syntheticChain(underlying float64, expiry time.Time) []domain.OptionSnapshot {
	return []domain.OptionSnapshot{
		{
			Contract: domain.OptionContract{
				Underlying: "SPY",
				OptionType: domain.OptionTypeCall,
				Strike:     underlying - 5,
				Expiry:     expiry,
				Multiplier: 100,
			},
			Greeks:       domain.OptionGreeks{Delta: 0.70, Gamma: 0.03, Theta: -0.15, Vega: 0.20, IV: 0.25},
			Bid:          6.50,
			Ask:          6.80,
			Mid:          6.65,
			Volume:       1200,
			OpenInterest: 5000,
		},
		{
			Contract: domain.OptionContract{
				Underlying: "SPY",
				OptionType: domain.OptionTypeCall,
				Strike:     underlying,
				Expiry:     expiry,
				Multiplier: 100,
			},
			Greeks:       domain.OptionGreeks{Delta: 0.50, Gamma: 0.04, Theta: -0.20, Vega: 0.25, IV: 0.22},
			Bid:          3.00,
			Ask:          3.30,
			Mid:          3.15,
			Volume:       3000,
			OpenInterest: 10000,
		},
		{
			Contract: domain.OptionContract{
				Underlying: "SPY",
				OptionType: domain.OptionTypeCall,
				Strike:     underlying + 5,
				Expiry:     expiry,
				Multiplier: 100,
			},
			Greeks:       domain.OptionGreeks{Delta: 0.30, Gamma: 0.03, Theta: -0.10, Vega: 0.18, IV: 0.24},
			Bid:          1.20,
			Ask:          1.50,
			Mid:          1.35,
			Volume:       2000,
			OpenInterest: 8000,
		},
		{
			Contract: domain.OptionContract{
				Underlying: "SPY",
				OptionType: domain.OptionTypePut,
				Strike:     underlying - 5,
				Expiry:     expiry,
				Multiplier: 100,
			},
			Greeks:       domain.OptionGreeks{Delta: -0.30, Gamma: 0.03, Theta: -0.10, Vega: 0.18, IV: 0.24},
			Bid:          1.00,
			Ask:          1.30,
			Mid:          1.15,
			Volume:       1500,
			OpenInterest: 6000,
		},
		{
			Contract: domain.OptionContract{
				Underlying: "SPY",
				OptionType: domain.OptionTypePut,
				Strike:     underlying,
				Expiry:     expiry,
				Multiplier: 100,
			},
			Greeks:       domain.OptionGreeks{Delta: -0.50, Gamma: 0.04, Theta: -0.20, Vega: 0.25, IV: 0.23},
			Bid:          2.80,
			Ask:          3.10,
			Mid:          2.95,
			Volume:       2500,
			OpenInterest: 9000,
		},
		{
			Contract: domain.OptionContract{
				Underlying: "SPY",
				OptionType: domain.OptionTypePut,
				Strike:     underlying + 5,
				Expiry:     expiry,
				Multiplier: 100,
			},
			Greeks:       domain.OptionGreeks{Delta: -0.70, Gamma: 0.03, Theta: -0.15, Vega: 0.20, IV: 0.26},
			Bid:          6.00,
			Ask:          6.30,
			Mid:          6.15,
			Volume:       800,
			OpenInterest: 3000,
		},
	}
}

func TestOptionsSnapshot_DerivedFields(t *testing.T) {
	t.Parallel()
	closePrice := 450.0
	snap := Snapshot{Values: map[string]float64{
		"close":         closePrice,
		"open":          449,
		"high":          452,
		"low":           448,
		"volume":        5000000,
		"iv_rank":       65.0,
		"iv_percentile": 72.0,
	}}

	expiry := time.Now().AddDate(0, 0, 30)
	chain := syntheticChain(closePrice, expiry)

	os := NewOptionsSnapshot(snap, chain, nil, time.Now())

	// ATM IV should come from the contract with strike closest to close (= $450 strike).
	if os.ATMImpliedVol == 0 {
		t.Error("expected non-zero ATM IV")
	}

	// Put/call ratio: sum(put volume) / sum(call volume).
	// Puts: 1500 + 2500 + 800 = 4800
	// Calls: 1200 + 3000 + 2000 = 6200
	expectedRatio := 4800.0 / 6200.0
	if diff := os.PutCallRatio - expectedRatio; diff > 0.01 || diff < -0.01 {
		t.Errorf("put/call ratio = %v, want ~%v", os.PutCallRatio, expectedRatio)
	}

	// IV rank/percentile should be propagated from snap.
	if os.IVRank != 65.0 {
		t.Errorf("IV rank = %v, want 65", os.IVRank)
	}
	if os.IVPercentile != 72.0 {
		t.Errorf("IV percentile = %v, want 72", os.IVPercentile)
	}

	// Derived values should be in embedded Values map for condition evaluation.
	if os.Values["atm_iv"] != os.ATMImpliedVol {
		t.Error("atm_iv not propagated to Values map")
	}
	if os.Values["put_call_ratio"] != os.PutCallRatio {
		t.Error("put_call_ratio not propagated to Values map")
	}
}

func TestOptionsSnapshot_WithPosition(t *testing.T) {
	t.Parallel()
	snap := Snapshot{Values: map[string]float64{
		"close":   450,
		"open":    449,
		"high":    452,
		"low":     448,
		"volume":  5000000,
		"pnl_pct": 15.5,
		"dte":     20,
	}}

	pos := &OpenPosition{
		Ticker:     "SPY",
		Side:       domain.PositionSideLong,
		EntryPrice: 440,
		Quantity:   1,
	}

	expiry := time.Now().AddDate(0, 0, 30)
	chain := syntheticChain(450, expiry)

	os := NewOptionsSnapshot(snap, chain, pos, time.Now())

	if os.PositionPnLPct != 15.5 {
		t.Errorf("position pnl pct = %v, want 15.5", os.PositionPnLPct)
	}
	if os.DTE != 20 {
		t.Errorf("DTE = %v, want 20", os.DTE)
	}
}

func TestOptionsSnapshot_ConditionEvaluation(t *testing.T) {
	t.Parallel()
	snap := Snapshot{Values: map[string]float64{
		"close":         450,
		"open":          449,
		"high":          452,
		"low":           448,
		"volume":        5000000,
		"iv_rank":       75.0,
		"iv_percentile": 80.0,
	}}

	expiry := time.Now().AddDate(0, 0, 30)
	chain := syntheticChain(450, expiry)
	os := NewOptionsSnapshot(snap, chain, nil, time.Now())

	// Test: iv_rank > 50 should be true.
	cond := Condition{Field: "iv_rank", Op: "gt", Value: fp(50)}
	if !EvaluateCondition(cond, os.Snapshot, nil) {
		t.Error("expected iv_rank > 50 to be true")
	}

	// Test: put_call_ratio < 1 should be true (calls have more volume than puts).
	cond2 := Condition{Field: "put_call_ratio", Op: "lt", Value: fp(1)}
	if !EvaluateCondition(cond2, os.Snapshot, nil) {
		t.Error("expected put_call_ratio < 1 to be true")
	}
}

func TestSelectLeg_ByDeltaTarget(t *testing.T) {
	t.Parallel()
	now := time.Now()
	expiry := now.AddDate(0, 0, 30)
	chain := syntheticChain(450, expiry)

	// Select a call with delta closest to 0.30.
	sel := LegSelector{
		OptionType:  domain.OptionTypeCall,
		DeltaTarget: 0.30,
		DTEMin:      20,
		DTEMax:      45,
		Side:        domain.OrderSideBuy,
		Intent:      domain.PositionIntentBuyToOpen,
	}

	leg, err := SelectLeg(chain, sel, now)
	if err != nil {
		t.Fatalf("SelectLeg() error = %v", err)
	}
	if leg.Contract.OptionType != domain.OptionTypeCall {
		t.Errorf("option type = %q, want call", leg.Contract.OptionType)
	}
	// The 0.30 delta call has strike = 455.
	if leg.Greeks.Delta != 0.30 {
		t.Errorf("delta = %v, want 0.30", leg.Greeks.Delta)
	}
}

func TestSelectLeg_NoMatchingContracts_DTERange(t *testing.T) {
	t.Parallel()
	now := time.Now()
	expiry := now.AddDate(0, 0, 30)
	chain := syntheticChain(450, expiry)

	// DTE range that excludes all contracts.
	sel := LegSelector{
		OptionType:  domain.OptionTypeCall,
		DeltaTarget: 0.50,
		DTEMin:      60,
		DTEMax:      90,
	}

	_, err := SelectLeg(chain, sel, now)
	if err == nil {
		t.Error("expected error for no matching contracts")
	}
}

func TestSelectSpreadLegs_BullCallSpread(t *testing.T) {
	t.Parallel()
	now := time.Now()
	expiry := now.AddDate(0, 0, 30)
	chain := syntheticChain(450, expiry)

	selectors := map[string]LegSelector{
		"long_call": {
			OptionType:  domain.OptionTypeCall,
			DeltaTarget: 0.50,
			DTEMin:      20,
			DTEMax:      45,
			Side:        domain.OrderSideBuy,
			Intent:      domain.PositionIntentBuyToOpen,
			Ratio:       1,
		},
		"short_call": {
			OptionType:  domain.OptionTypeCall,
			DeltaTarget: 0.30,
			DTEMin:      20,
			DTEMax:      45,
			Side:        domain.OrderSideSell,
			Intent:      domain.PositionIntentSellToOpen,
			Ratio:       1,
		},
	}

	legs, err := SelectSpreadLegs(chain, selectors, now)
	if err != nil {
		t.Fatalf("SelectSpreadLegs() error = %v", err)
	}
	if len(legs) != 2 {
		t.Fatalf("legs = %d, want 2", len(legs))
	}
	if legs["long_call"] == nil || legs["short_call"] == nil {
		t.Fatal("expected both legs to be selected")
	}
	// Long call should have higher delta than short call.
	if legs["long_call"].Greeks.Delta < legs["short_call"].Greeks.Delta {
		t.Error("long call delta should be >= short call delta")
	}
}

func TestBuildSpread_BullCallSpread(t *testing.T) {
	t.Parallel()
	now := time.Now()
	expiry := now.AddDate(0, 0, 30)
	chain := syntheticChain(450, expiry)

	selectors := map[string]LegSelector{
		"long_call": {
			OptionType:  domain.OptionTypeCall,
			DeltaTarget: 0.50,
			DTEMin:      20,
			DTEMax:      45,
			Side:        domain.OrderSideBuy,
			Intent:      domain.PositionIntentBuyToOpen,
			Ratio:       1,
		},
		"short_call": {
			OptionType:  domain.OptionTypeCall,
			DeltaTarget: 0.30,
			DTEMin:      20,
			DTEMax:      45,
			Side:        domain.OrderSideSell,
			Intent:      domain.PositionIntentSellToOpen,
			Ratio:       1,
		},
	}

	legs, err := SelectSpreadLegs(chain, selectors, now)
	if err != nil {
		t.Fatalf("SelectSpreadLegs() error = %v", err)
	}

	spread, err := BuildSpread(domain.StrategyBullCallSpread, "SPY", legs, selectors)
	if err != nil {
		t.Fatalf("BuildSpread() error = %v", err)
	}
	if spread.StrategyType != domain.StrategyBullCallSpread {
		t.Errorf("strategy = %q, want bull_call_spread", spread.StrategyType)
	}
	if spread.Underlying != "SPY" {
		t.Errorf("underlying = %q, want SPY", spread.Underlying)
	}
	if len(spread.Legs) != 2 {
		t.Errorf("legs = %d, want 2", len(spread.Legs))
	}

	// Verify legs have correct sides.
	hasBuy, hasSell := false, false
	for _, leg := range spread.Legs {
		if leg.Side == domain.OrderSideBuy {
			hasBuy = true
		}
		if leg.Side == domain.OrderSideSell {
			hasSell = true
		}
	}
	if !hasBuy || !hasSell {
		t.Error("expected one buy leg and one sell leg")
	}
}

func TestBuildSpread_EmptyLegs(t *testing.T) {
	t.Parallel()
	_, err := BuildSpread(domain.StrategyBullCallSpread, "SPY", nil, nil)
	if err == nil {
		t.Error("expected error for empty legs")
	}
}

func TestSelectLeg_EmptyChain_ReturnsError(t *testing.T) {
	t.Parallel()
	sel := LegSelector{OptionType: domain.OptionTypeCall, DeltaTarget: 0.50, DTEMin: 20, DTEMax: 45}
	_, err := SelectLeg(nil, sel, time.Now())
	if err == nil {
		t.Error("expected error for empty chain")
	}
}
