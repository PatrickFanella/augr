package agent

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func ptr(f float64) *float64 { return &f }

func TestApplyStrategyRiskOverrides_NilGuards(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		state  *PipelineState
		config *StrategyConfig
	}{
		{"nil state", nil, &StrategyConfig{}},
		{"nil config", &PipelineState{}, nil},
		{"nil risk config", &PipelineState{}, &StrategyConfig{RiskConfig: nil}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ApplyStrategyRiskOverrides(tc.state, domain.PipelineSignalBuy, tc.config)
			if got != domain.PipelineSignalBuy {
				t.Errorf("expected signal unchanged, got %q", got)
			}
		})
	}
}

func TestApplyStrategyRiskOverrides_ConfidenceGate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		confidence    float64
		minConfidence float64
		inputSignal   domain.PipelineSignal
		wantSignal    domain.PipelineSignal
		wantPosition  float64
		wantRationale bool
	}{
		{
			name:          "below threshold flips to hold",
			confidence:    0.3,
			minConfidence: 0.5,
			inputSignal:   domain.PipelineSignalBuy,
			wantSignal:    domain.PipelineSignalHold,
			wantPosition:  0,
			wantRationale: true,
		},
		{
			name:          "at threshold passes through",
			confidence:    0.5,
			minConfidence: 0.5,
			inputSignal:   domain.PipelineSignalBuy,
			wantSignal:    domain.PipelineSignalBuy,
			wantPosition:  100,
			wantRationale: false,
		},
		{
			name:          "above threshold passes through",
			confidence:    0.8,
			minConfidence: 0.5,
			inputSignal:   domain.PipelineSignalSell,
			wantSignal:    domain.PipelineSignalSell,
			wantPosition:  50,
			wantRationale: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := &PipelineState{
				FinalSignal: FinalSignal{Signal: tc.inputSignal, Confidence: tc.confidence},
				TradingPlan: TradingPlan{
					Action:       tc.inputSignal,
					PositionSize: 100,
					Rationale:    "original",
				},
			}
			if tc.wantPosition == 50 {
				state.TradingPlan.PositionSize = 50
			}
			cfg := &StrategyConfig{RiskConfig: &StrategyRiskConfig{MinConfidence: ptr(tc.minConfidence)}}

			got := ApplyStrategyRiskOverrides(state, tc.inputSignal, cfg)

			if got != tc.wantSignal {
				t.Errorf("signal: got %q, want %q", got, tc.wantSignal)
			}
			if tc.wantSignal == domain.PipelineSignalHold {
				if state.FinalSignal.Signal != domain.PipelineSignalHold {
					t.Error("state.FinalSignal.Signal not updated to hold")
				}
				if state.TradingPlan.Action != domain.PipelineSignalHold {
					t.Error("state.TradingPlan.Action not updated to hold")
				}
				if state.TradingPlan.PositionSize != 0 {
					t.Errorf("position size: got %f, want 0", state.TradingPlan.PositionSize)
				}
			}
			hasRationale := state.TradingPlan.Rationale != "original"
			if hasRationale != tc.wantRationale {
				t.Errorf("rationale changed: got %v, want %v (rationale=%q)", hasRationale, tc.wantRationale, state.TradingPlan.Rationale)
			}
		})
	}
}

func TestAdjustTradingPlanTargets_StopLoss(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		signal     domain.PipelineSignal
		entry      float64
		stopLoss   float64
		multiplier float64
		want       float64
	}{
		{
			name:       "buy: multiplier widens stop",
			signal:     domain.PipelineSignalBuy,
			entry:      100,
			stopLoss:   90,
			multiplier: 2.0,
			want:       80, // distance 10, scaled 20, 100-20=80
		},
		{
			name:       "buy: multiplier tightens stop",
			signal:     domain.PipelineSignalBuy,
			entry:      100,
			stopLoss:   90,
			multiplier: 0.5,
			want:       95, // distance 10, scaled 5, 100-5=95
		},
		{
			name:       "sell: multiplier widens stop",
			signal:     domain.PipelineSignalSell,
			entry:      100,
			stopLoss:   110,
			multiplier: 2.0,
			want:       120, // distance 10, scaled 20, 100+20=120
		},
		{
			name:       "buy: stop clamped to zero",
			signal:     domain.PipelineSignalBuy,
			entry:      5,
			stopLoss:   2,
			multiplier: 3.0,
			want:       0, // distance 3, scaled 9, max(0, 5-9)=0
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := &TradingPlan{EntryPrice: tc.entry, StopLoss: tc.stopLoss}
			cfg := &StrategyRiskConfig{StopLossMultiplier: ptr(tc.multiplier)}
			AdjustTradingPlanTargets(plan, tc.signal, cfg)
			if plan.StopLoss != tc.want {
				t.Errorf("stop loss: got %f, want %f", plan.StopLoss, tc.want)
			}
		})
	}
}

func TestAdjustTradingPlanTargets_TakeProfit(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		signal     domain.PipelineSignal
		entry      float64
		takeProfit float64
		multiplier float64
		want       float64
	}{
		{
			name:       "buy: multiplier widens take profit",
			signal:     domain.PipelineSignalBuy,
			entry:      100,
			takeProfit: 120,
			multiplier: 2.0,
			want:       140, // distance 20, scaled 40, 100+40=140
		},
		{
			name:       "sell: multiplier widens take profit",
			signal:     domain.PipelineSignalSell,
			entry:      100,
			takeProfit: 80,
			multiplier: 1.5,
			want:       70, // distance 20, scaled 30, 100-30=70
		},
		{
			name:       "sell: take profit clamped to zero",
			signal:     domain.PipelineSignalSell,
			entry:      10,
			takeProfit: 5,
			multiplier: 5.0,
			want:       0, // distance 5, scaled 25, max(0, 10-25)=0
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := &TradingPlan{EntryPrice: tc.entry, TakeProfit: tc.takeProfit}
			cfg := &StrategyRiskConfig{TakeProfitMultiplier: ptr(tc.multiplier)}
			AdjustTradingPlanTargets(plan, tc.signal, cfg)
			if plan.TakeProfit != tc.want {
				t.Errorf("take profit: got %f, want %f", plan.TakeProfit, tc.want)
			}
		})
	}
}

func TestAdjustTradingPlanTargets_NilPlanAndZeroEntry(t *testing.T) {
	t.Parallel()
	cfg := &StrategyRiskConfig{StopLossMultiplier: ptr(2.0)}

	// nil plan should not panic
	AdjustTradingPlanTargets(nil, domain.PipelineSignalBuy, cfg)

	// zero entry price should be a no-op
	plan := &TradingPlan{EntryPrice: 0, StopLoss: 10}
	AdjustTradingPlanTargets(plan, domain.PipelineSignalBuy, cfg)
	if plan.StopLoss != 10 {
		t.Errorf("expected no-op for zero entry price, stop loss changed to %f", plan.StopLoss)
	}
}

func TestAdjustTradingPlanTargets_NilMultipliersNoOp(t *testing.T) {
	t.Parallel()
	plan := &TradingPlan{EntryPrice: 100, StopLoss: 90, TakeProfit: 120}
	cfg := &StrategyRiskConfig{} // no multipliers set
	AdjustTradingPlanTargets(plan, domain.PipelineSignalBuy, cfg)
	if plan.StopLoss != 90 {
		t.Errorf("stop loss changed without multiplier: %f", plan.StopLoss)
	}
	if plan.TakeProfit != 120 {
		t.Errorf("take profit changed without multiplier: %f", plan.TakeProfit)
	}
}

func TestAppendRationale(t *testing.T) {
	t.Parallel()
	cases := []struct {
		existing, addition, want string
	}{
		{"", "new", "new"},
		{"old", "", "old"},
		{"old", "new", "old new"},
		{"", "", ""},
		{"  spaced  ", "  also  ", "spaced also"},
	}
	for _, tc := range cases {
		got := appendRationale(tc.existing, tc.addition)
		if got != tc.want {
			t.Errorf("appendRationale(%q, %q) = %q, want %q", tc.existing, tc.addition, got, tc.want)
		}
	}
}
