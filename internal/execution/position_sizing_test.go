package execution_test

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

func TestATRPositionSize(t *testing.T) {
	t.Parallel()

	got := execution.ATRPositionSize(100000, 0.02, 5, 2)
	want := 200.0

	if got != want {
		t.Fatalf("ATRPositionSize() = %v, want %v", got, want)
	}
}

func TestKellyPositionSize(t *testing.T) {
	t.Parallel()

	got := execution.KellyPositionSize(100000, 0.60, 2)
	want := 40000.0

	if got != want {
		t.Fatalf("KellyPositionSize() = %v, want %v", got, want)
	}
}

func TestFixedFractionalSize(t *testing.T) {
	t.Parallel()

	got := execution.FixedFractionalSize(100000, 0.10, 50)
	want := 200.0

	if got != want {
		t.Fatalf("FixedFractionalSize() = %v, want %v", got, want)
	}
}

func TestCalculatePositionSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method execution.PositionSizingMethod
		params execution.PositionSizingParams
		want   float64
	}{
		{
			name:   "atr",
			method: execution.PositionSizingMethodATR,
			params: execution.PositionSizingParams{
				AccountValue: 100000,
				RiskPct:      0.02,
				ATR:          5,
				Multiplier:   2,
			},
			want: 200,
		},
		{
			name:   "kelly",
			method: execution.PositionSizingMethodKelly,
			params: execution.PositionSizingParams{
				AccountValue: 100000,
				WinRate:      0.60,
				WinLossRatio: 2,
			},
			want: 40000,
		},
		{
			name:   "fixed fractional",
			method: execution.PositionSizingMethodFixedFractional,
			params: execution.PositionSizingParams{
				AccountValue:  100000,
				FractionPct:   0.10,
				PricePerShare: 50,
			},
			want: 200,
		},
		{
			name:   "half kelly",
			method: execution.PositionSizingMethodKelly,
			params: execution.PositionSizingParams{
				AccountValue: 100000,
				WinRate:      0.60,
				WinLossRatio: 2,
				HalfKelly:    true,
			},
			want: 20000,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := execution.CalculatePositionSize(tc.method, tc.params)
			if got != tc.want {
				t.Fatalf("CalculatePositionSize(%q) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
