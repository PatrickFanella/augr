package rules

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func fp(v float64) *float64 { return &v }

func TestNewSnapshotFromBar(t *testing.T) {
	t.Parallel()
	indicators := []domain.Indicator{
		{Name: "rsi_14", Value: 45},
		{Name: "sma_200", Value: 150},
	}
	bar := domain.OHLCV{Close: 155, Open: 152, High: 157, Low: 151, Volume: 50000}
	snap := NewSnapshotFromBar(indicators, bar)

	if snap.Values["rsi_14"] != 45 {
		t.Errorf("rsi_14 = %v, want 45", snap.Values["rsi_14"])
	}
	if snap.Values["close"] != 155 {
		t.Errorf("close = %v, want 155", snap.Values["close"])
	}
	if snap.Values["volume"] != 50000 {
		t.Errorf("volume = %v, want 50000", snap.Values["volume"])
	}
}

func TestEvaluateCondition_Operators(t *testing.T) {
	t.Parallel()
	snap := Snapshot{Values: map[string]float64{"rsi_14": 25, "close": 100, "sma_200": 95}}

	cases := []struct {
		name string
		cond Condition
		want bool
	}{
		{"gt true", Condition{Field: "rsi_14", Op: "gt", Value: fp(20)}, true},
		{"gt false", Condition{Field: "rsi_14", Op: "gt", Value: fp(30)}, false},
		{"lt true", Condition{Field: "rsi_14", Op: "lt", Value: fp(30)}, true},
		{"lt false", Condition{Field: "rsi_14", Op: "lt", Value: fp(20)}, false},
		{"gte equal", Condition{Field: "rsi_14", Op: "gte", Value: fp(25)}, true},
		{"lte equal", Condition{Field: "rsi_14", Op: "lte", Value: fp(25)}, true},
		{"eq true", Condition{Field: "rsi_14", Op: "eq", Value: fp(25)}, true},
		{"eq false", Condition{Field: "rsi_14", Op: "eq", Value: fp(26)}, false},
		{"ref gt", Condition{Field: "close", Op: "gt", Ref: "sma_200"}, true},
		{"ref lt", Condition{Field: "close", Op: "lt", Ref: "sma_200"}, false},
		{"missing field", Condition{Field: "unknown", Op: "gt", Value: fp(0)}, false},
		{"missing ref", Condition{Field: "rsi_14", Op: "gt", Ref: "unknown"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := EvaluateCondition(tc.cond, snap, nil)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEvaluateCondition_CrossAbove(t *testing.T) {
	t.Parallel()
	prev := &Snapshot{Values: map[string]float64{"rsi_14": 28}}
	snap := Snapshot{Values: map[string]float64{"rsi_14": 32}}

	cond := Condition{Field: "rsi_14", Op: "cross_above", Value: fp(30)}
	if !EvaluateCondition(cond, snap, prev) {
		t.Error("cross_above should be true: prev 28 <= 30, current 32 > 30")
	}

	// No cross — both above
	prev2 := &Snapshot{Values: map[string]float64{"rsi_14": 31}}
	if EvaluateCondition(cond, snap, prev2) {
		t.Error("cross_above should be false: prev 31 > 30")
	}

	// No prev — returns false
	if EvaluateCondition(cond, snap, nil) {
		t.Error("cross_above should be false without prev")
	}
}

func TestEvaluateCondition_CrossBelow(t *testing.T) {
	t.Parallel()
	prev := &Snapshot{Values: map[string]float64{"rsi_14": 72}}
	snap := Snapshot{Values: map[string]float64{"rsi_14": 68}}

	cond := Condition{Field: "rsi_14", Op: "cross_below", Value: fp(70)}
	if !EvaluateCondition(cond, snap, prev) {
		t.Error("cross_below should be true: prev 72 >= 70, current 68 < 70")
	}
}

func TestEvaluateGroup_AND(t *testing.T) {
	t.Parallel()
	snap := Snapshot{Values: map[string]float64{"rsi_14": 25, "close": 100, "sma_200": 95}}
	group := ConditionGroup{
		Operator: "AND",
		Conditions: []Condition{
			{Field: "rsi_14", Op: "lt", Value: fp(30)},
			{Field: "close", Op: "gt", Ref: "sma_200"},
		},
	}
	if !EvaluateGroup(group, snap, nil) {
		t.Error("AND group should be true: rsi < 30 AND close > sma_200")
	}

	// Break one condition
	snap.Values["rsi_14"] = 35
	if EvaluateGroup(group, snap, nil) {
		t.Error("AND group should be false: rsi 35 is not < 30")
	}
}

func TestEvaluateGroup_OR(t *testing.T) {
	t.Parallel()
	snap := Snapshot{Values: map[string]float64{"rsi_14": 75, "close": 80, "sma_50": 90}}
	group := ConditionGroup{
		Operator: "OR",
		Conditions: []Condition{
			{Field: "rsi_14", Op: "gt", Value: fp(70)},
			{Field: "close", Op: "lt", Ref: "sma_50"},
		},
	}
	if !EvaluateGroup(group, snap, nil) {
		t.Error("OR group should be true: rsi > 70")
	}
}

func TestPassesFilters(t *testing.T) {
	t.Parallel()
	snap := Snapshot{Values: map[string]float64{"volume": 200000, "atr_14": 2.5}}

	if !PassesFilters(nil, snap) {
		t.Error("nil filters should pass")
	}
	if !PassesFilters(&FilterConfig{MinVolume: 100000, MinATR: 1.0}, snap) {
		t.Error("should pass with volume 200k > 100k and ATR 2.5 > 1.0")
	}
	if PassesFilters(&FilterConfig{MinVolume: 300000}, snap) {
		t.Error("should fail with volume 200k < 300k")
	}
}
