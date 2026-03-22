package data_test

import (
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestSMAAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	tests := []struct {
		name     string
		period   int
		wantLen  int
		wantTail []float64
	}{
		{
			name:     "SMA20",
			period:   20,
			wantLen:  231,
			wantTail: []float64{184.861078, 184.813277, 184.805252},
		},
		{
			name:     "SMA50",
			period:   50,
			wantLen:  201,
			wantTail: []float64{177.706731, 177.975427, 178.276023},
		},
		{
			name:     "SMA200",
			period:   200,
			wantLen:  51,
			wantTail: []float64{151.858728, 152.176761, 152.497387},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := data.SMA(bars, tc.period)
			if len(got) != tc.wantLen {
				t.Fatalf("SMA(..., %d) len = %d, want %d", tc.period, len(got), tc.wantLen)
			}
			assertTailClose(t, got, tc.wantTail, 1e-6)
		})
	}
}

func TestEMAAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	tests := []struct {
		name     string
		period   int
		wantLen  int
		wantTail []float64
	}{
		{
			name:     "EMA12",
			period:   12,
			wantLen:  239,
			wantTail: []float64{184.139851, 184.108639, 184.343402},
		},
		{
			name:     "EMA26",
			period:   26,
			wantLen:  225,
			wantTail: []float64{182.574128, 182.675079, 182.894303},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := data.EMA(bars, tc.period)
			if len(got) != tc.wantLen {
				t.Fatalf("EMA(..., %d) len = %d, want %d", tc.period, len(got), tc.wantLen)
			}
			assertTailClose(t, got, tc.wantTail, 1e-6)
		})
	}
}

func TestMACDAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	macdLine, signalLine, histogram := data.MACD(bars, 12, 26, 9)

	if len(macdLine) != 225 {
		t.Fatalf("MACD macdLine len = %d, want 225", len(macdLine))
	}
	if len(signalLine) != 217 {
		t.Fatalf("MACD signalLine len = %d, want 217", len(signalLine))
	}
	if len(histogram) != 217 {
		t.Fatalf("MACD histogram len = %d, want 217", len(histogram))
	}

	assertTailClose(t, macdLine, []float64{1.565723, 1.433559, 1.449099}, 1e-6)
	assertTailClose(t, signalLine, []float64{2.330387, 2.151022, 2.010637}, 1e-6)
	assertTailClose(t, histogram, []float64{-0.764665, -0.717463, -0.561538}, 1e-6)
}

func TestIndicatorsReturnEmptyWhenInsufficientData(t *testing.T) {
	bars := indicatorTestBars(10)

	if got := data.SMA(bars, 20); len(got) != 0 {
		t.Fatalf("SMA() len = %d, want 0", len(got))
	}
	if got := data.EMA(bars, 12); len(got) != 0 {
		t.Fatalf("EMA() len = %d, want 0", len(got))
	}

	macdLine, signalLine, histogram := data.MACD(bars, 12, 26, 9)
	if len(macdLine) != 0 || len(signalLine) != 0 || len(histogram) != 0 {
		t.Fatalf("MACD() lens = (%d, %d, %d), want (0, 0, 0)", len(macdLine), len(signalLine), len(histogram))
	}
}

func TestIndicatorsReturnEmptyForInvalidParameters(t *testing.T) {
	bars := indicatorTestBars(50)

	if got := data.SMA(bars, 0); len(got) != 0 {
		t.Fatalf("SMA() with period 0 len = %d, want 0", len(got))
	}
	if got := data.SMA(bars, -5); len(got) != 0 {
		t.Fatalf("SMA() with negative period len = %d, want 0", len(got))
	}

	if got := data.EMA(bars, 0); len(got) != 0 {
		t.Fatalf("EMA() with period 0 len = %d, want 0", len(got))
	}
	if got := data.EMA(bars, -3); len(got) != 0 {
		t.Fatalf("EMA() with negative period len = %d, want 0", len(got))
	}

	macdLine, signalLine, histogram := data.MACD(bars, 26, 26, 9)
	if len(macdLine) != 0 || len(signalLine) != 0 || len(histogram) != 0 {
		t.Fatalf("MACD() with fast>=slow lens = (%d, %d, %d), want (0, 0, 0)", len(macdLine), len(signalLine), len(histogram))
	}

	macdLine, signalLine, histogram = data.MACD(bars, 12, 26, 0)
	if len(macdLine) != 0 || len(signalLine) != 0 || len(histogram) != 0 {
		t.Fatalf("MACD() with signal=0 lens = (%d, %d, %d), want (0, 0, 0)", len(macdLine), len(signalLine), len(histogram))
	}

	macdLine, signalLine, histogram = data.MACD(bars, 12, 26, -1)
	if len(macdLine) != 0 || len(signalLine) != 0 || len(histogram) != 0 {
		t.Fatalf("MACD() with negative signal lens = (%d, %d, %d), want (0, 0, 0)", len(macdLine), len(signalLine), len(histogram))
	}
}

func indicatorTestBars(count int) []domain.OHLCV {
	bars := make([]domain.OHLCV, count)
	start := time.Unix(0, 0).UTC()

	for i := range count {
		close := round6(100 + float64(i)*0.35 + math.Sin(float64(i)/7.0)*4.2 + float64((i%5)-2)*0.8 - float64((i%3)-1)*0.45)
		bars[i] = domain.OHLCV{
			Timestamp: start.Add(time.Duration(i) * time.Hour),
			Open:      close - 0.5,
			High:      close + 1,
			Low:       close - 1,
			Close:     close,
			Volume:    1000 + float64(i),
		}
	}

	return bars
}

func assertTailClose(t *testing.T, got, want []float64, delta float64) {
	t.Helper()

	if len(got) < len(want) {
		t.Fatalf("got %d values, want at least %d", len(got), len(want))
	}

	start := len(got) - len(want)
	for i, expected := range want {
		actual := got[start+i]
		if math.Abs(actual-expected) > delta {
			t.Fatalf("tail[%d] = %.6f, want %.6f (delta %.6f)", i, actual, expected, delta)
		}
	}
}

func round6(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}
