package execution_test

import (
	"math"
	"testing"
	"testing/quick"

	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

func TestATRPositionSize(t *testing.T) {
	t.Parallel()

	got := execution.ATRPositionSize(100000, 0.02, 5, 2)
	want := 200.0

	assertFloatClose(t, got, want)
}

func TestKellyPositionSize(t *testing.T) {
	t.Parallel()

	got := execution.KellyPositionSize(100000, 0.60, 2)
	want := 40000.0

	assertFloatClose(t, got, want)
}

func TestFixedFractionalSize(t *testing.T) {
	t.Parallel()

	got := execution.FixedFractionalSize(100000, 0.10, 50)
	want := 200.0

	assertFloatClose(t, got, want)
}

func TestPositionSizingReturnsZeroForInvalidInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		got  float64
	}{
		{
			name: "atr with non-positive account value",
			got:  execution.ATRPositionSize(0, 0.02, 5, 2),
		},
		{
			name: "atr with non-positive risk percent",
			got:  execution.ATRPositionSize(100000, -0.02, 5, 2),
		},
		{
			name: "kelly with out of range win rate",
			got:  execution.KellyPositionSize(100000, 1.2, 2),
		},
		{
			name: "kelly with negative expectation",
			got:  execution.KellyPositionSize(100000, 0.30, 2),
		},
		{
			name: "fixed fractional with non-positive account value",
			got:  execution.FixedFractionalSize(-100000, 0.10, 50),
		},
		{
			name: "fixed fractional with non-positive fraction",
			got:  execution.FixedFractionalSize(100000, 0, 50),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != 0 {
				t.Fatalf("%s = %v, want 0", tc.name, tc.got)
			}
		})
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
		t.Run(tc.name, func(t *testing.T) {
			got := execution.CalculatePositionSize(tc.method, tc.params)
			assertFloatClose(t, got, tc.want)
		})
	}
}

func TestCalculatePositionSize_BoundariesAndUnknownMethod(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method execution.PositionSizingMethod
		params execution.PositionSizingParams
		want   float64
	}{
		{
			name:   "unknown method returns zero",
			method: execution.PositionSizingMethod("unknown"),
			params: execution.PositionSizingParams{AccountValue: 100000},
			want:   0,
		},
		{
			name:   "kelly win rate zero returns zero",
			method: execution.PositionSizingMethodKelly,
			params: execution.PositionSizingParams{
				AccountValue: 100000,
				WinRate:      0,
				WinLossRatio: 2,
			},
			want: 0,
		},
		{
			name:   "kelly full win rate allocates full account",
			method: execution.PositionSizingMethodKelly,
			params: execution.PositionSizingParams{
				AccountValue: 100000,
				WinRate:      1,
				WinLossRatio: 2,
			},
			want: 100000,
		},
		{
			name:   "kelly zero edge only halves positive sizes",
			method: execution.PositionSizingMethodKelly,
			params: execution.PositionSizingParams{
				AccountValue: 100000,
				WinRate:      0.5,
				WinLossRatio: 1,
				HalfKelly:    true,
			},
			want: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := execution.CalculatePositionSize(tc.method, tc.params)
			assertFloatClose(t, got, tc.want)
		})
	}
}

func TestPositionSizingProperties(t *testing.T) {
	t.Parallel()

	cfg := &quick.Config{MaxCount: 128}

	t.Run("kelly stays within account value and half kelly halves result", func(t *testing.T) {
		t.Parallel()

		err := quick.Check(func(account uint32, winRatePct uint8, winLossRatio uint8) bool {
			accountValue := float64(account%1_000_000) + 1
			winRate := float64(winRatePct%101) / 100
			ratio := float64(winLossRatio%20) + 1

			full := execution.KellyPositionSize(accountValue, winRate, ratio)
			half := execution.CalculatePositionSize(execution.PositionSizingMethodKelly, execution.PositionSizingParams{
				AccountValue: accountValue,
				WinRate:      winRate,
				WinLossRatio: ratio,
				HalfKelly:    true,
			})

			return full >= 0 &&
				full <= accountValue &&
				math.Abs(half-full*0.5) <= 1e-9
		}, cfg)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("atr and fixed fractional scale linearly with account value", func(t *testing.T) {
		t.Parallel()

		err := quick.Check(func(account uint32, riskPct uint8, atr uint16, multiplier uint8, fractionPct uint8, price uint16) bool {
			accountValue := float64(account%1_000_000) + 1
			doubleAccountValue := accountValue * 2
			risk := float64(riskPct%20+1) / 1000
			atrValue := float64(atr%5000+1) / 100
			mult := float64(multiplier%10 + 1)
			fraction := float64(fractionPct%20+1) / 100
			pricePerShare := float64(price%5000+1) / 10

			atrSize := execution.ATRPositionSize(accountValue, risk, atrValue, mult)
			doubleATRSize := execution.ATRPositionSize(doubleAccountValue, risk, atrValue, mult)
			fixedSize := execution.FixedFractionalSize(accountValue, fraction, pricePerShare)
			doubleFixedSize := execution.FixedFractionalSize(doubleAccountValue, fraction, pricePerShare)

			return atrSize > 0 &&
				fixedSize > 0 &&
				math.Abs(doubleATRSize-atrSize*2) <= 1e-9 &&
				math.Abs(doubleFixedSize-fixedSize*2) <= 1e-9
		}, cfg)
		if err != nil {
			t.Fatal(err)
		}
	})
}

func assertFloatClose(t *testing.T, got, want float64) {
	t.Helper()

	const tolerance = 1e-9

	if math.Abs(got-want) > tolerance {
		t.Fatalf("got %v, want %v", got, want)
	}
}
