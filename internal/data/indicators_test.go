package data_test

import (
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

var computeAllIndicatorsSink map[string]any

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

func TestRSIAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.RSI(bars, 14)
	if len(got) != 236 {
		t.Fatalf("RSI len = %d, want 236", len(got))
	}

	assertTailClose(t, got, []float64{53.559125, 54.298812, 58.573861}, 1e-6)
}

func TestMFIAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.MFI(bars, 14)
	if len(got) != 236 {
		t.Fatalf("MFI len = %d, want 236", len(got))
	}

	assertTailClose(t, got, []float64{78.755090, 78.741012, 85.849543}, 1e-6)
}

func TestStochasticAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	k, d := data.Stochastic(bars, 14, 3, 3)
	if len(k) != 235 {
		t.Fatalf("Stochastic %%K len = %d, want 235", len(k))
	}
	if len(d) != 233 {
		t.Fatalf("Stochastic %%D len = %d, want 233", len(d))
	}

	assertTailClose(t, k, []float64{26.688359, 37.791535, 50.988790}, 1e-6)
	assertTailClose(t, d, []float64{32.305975, 31.849547, 38.489561}, 1e-6)
}

func TestWilliamsRAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.WilliamsR(bars, 14)
	if len(got) != 237 {
		t.Fatalf("WilliamsR len = %d, want 237", len(got))
	}

	assertTailClose(t, got, []float64{-64.905492, -53.943515, -28.184623}, 1e-6)
}

func TestCCIAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.CCI(bars, 20)
	if len(got) != 231 {
		t.Fatalf("CCI len = %d, want 231", len(got))
	}

	assertTailClose(t, got, []float64{-70.466559, -49.464500, 47.166374}, 1e-6)
}

func TestROCAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.ROC(bars, 12)
	if len(got) != 238 {
		t.Fatalf("ROC len = %d, want 238", len(got))
	}

	assertTailClose(t, got, []float64{-0.270142, -0.297897, -0.253791}, 1e-6)
}

func TestBollingerBandsAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	upper, middle, lower := data.BollingerBands(bars, 20, 2.0)
	if len(upper) != 231 || len(middle) != 231 || len(lower) != 231 {
		t.Fatalf("BollingerBands lens = (%d, %d, %d), want (231, 231, 231)", len(upper), len(middle), len(lower))
	}

	assertTailClose(t, upper, []float64{187.622069, 187.603352, 187.584890}, 1e-6)
	assertTailClose(t, middle, []float64{184.861078, 184.813277, 184.805252}, 1e-6)
	assertTailClose(t, lower, []float64{182.100088, 182.023202, 182.025615}, 1e-6)
}

func TestATRAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.ATR(bars, 14)
	if len(got) != 237 {
		t.Fatalf("ATR len = %d, want 237", len(got))
	}

	assertTailClose(t, got, []float64{2.654968, 2.608185, 2.614574}, 1e-6)
}

func TestVWMAAgainstKnownValues(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.VWMA(bars, 20)
	if len(got) != 231 {
		t.Fatalf("VWMA len = %d, want 231", len(got))
	}

	assertTailClose(t, got, []float64{184.859197, 184.811056, 184.803764}, 1e-6)
}

func TestOBVAgainstKnownValues(t *testing.T) {
	bars := volumeIndicatorTestBars()

	got := data.OBV(bars)
	if len(got) != len(bars) {
		t.Fatalf("OBV len = %d, want %d", len(got), len(bars))
	}

	assertClose(t, got, []float64{0, 150, 30, -100, 60, 260}, 1e-6)
}

func TestADLAgainstKnownValues(t *testing.T) {
	bars := volumeIndicatorTestBars()

	got := data.ADL(bars)
	if len(got) != len(bars) {
		t.Fatalf("ADL len = %d, want %d", len(got), len(bars))
	}

	assertClose(t, got, []float64{0, 75, -15, -145, -65, -65}, 1e-6)
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
	if got := data.RSI(bars, 14); len(got) != 0 {
		t.Fatalf("RSI() len = %d, want 0", len(got))
	}
	if got := data.MFI(bars, 14); len(got) != 0 {
		t.Fatalf("MFI() len = %d, want 0", len(got))
	}
	k, d := data.Stochastic(bars, 14, 3, 3)
	if len(k) != 0 || len(d) != 0 {
		t.Fatalf("Stochastic() lens = (%d, %d), want (0, 0)", len(k), len(d))
	}
	if got := data.WilliamsR(bars, 14); len(got) != 0 {
		t.Fatalf("WilliamsR() len = %d, want 0", len(got))
	}
	if got := data.CCI(bars, 20); len(got) != 0 {
		t.Fatalf("CCI() len = %d, want 0", len(got))
	}
	if got := data.ROC(bars, 12); len(got) != 0 {
		t.Fatalf("ROC() len = %d, want 0", len(got))
	}
	upper, middle, lower := data.BollingerBands(bars, 20, 2)
	if len(upper) != 0 || len(middle) != 0 || len(lower) != 0 {
		t.Fatalf("BollingerBands() lens = (%d, %d, %d), want (0, 0, 0)", len(upper), len(middle), len(lower))
	}
	if got := data.ATR(bars, 14); len(got) != 0 {
		t.Fatalf("ATR() len = %d, want 0", len(got))
	}
	if got := data.VWMA(bars, 20); len(got) != 0 {
		t.Fatalf("VWMA() len = %d, want 0", len(got))
	}
	if got := data.OBV(nil); len(got) != 0 {
		t.Fatalf("OBV() len = %d, want 0", len(got))
	}
	if got := data.ADL(nil); len(got) != 0 {
		t.Fatalf("ADL() len = %d, want 0", len(got))
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
	if got := data.RSI(bars, 0); len(got) != 0 {
		t.Fatalf("RSI() with period 0 len = %d, want 0", len(got))
	}
	if got := data.MFI(bars, -1); len(got) != 0 {
		t.Fatalf("MFI() with negative period len = %d, want 0", len(got))
	}
	k, d := data.Stochastic(bars, 14, 0, 3)
	if len(k) != 0 || len(d) != 0 {
		t.Fatalf("Stochastic() with dPeriod=0 lens = (%d, %d), want (0, 0)", len(k), len(d))
	}
	k, d = data.Stochastic(bars, 14, 3, -1)
	if len(k) != 0 || len(d) != 0 {
		t.Fatalf("Stochastic() with negative smooth lens = (%d, %d), want (0, 0)", len(k), len(d))
	}
	if got := data.WilliamsR(bars, 0); len(got) != 0 {
		t.Fatalf("WilliamsR() with period 0 len = %d, want 0", len(got))
	}
	if got := data.CCI(bars, -5); len(got) != 0 {
		t.Fatalf("CCI() with negative period len = %d, want 0", len(got))
	}
	if got := data.ROC(bars, 0); len(got) != 0 {
		t.Fatalf("ROC() with period 0 len = %d, want 0", len(got))
	}
	upper, middle, lower := data.BollingerBands(bars, 0, 2)
	if len(upper) != 0 || len(middle) != 0 || len(lower) != 0 {
		t.Fatalf("BollingerBands() with period 0 lens = (%d, %d, %d), want (0, 0, 0)", len(upper), len(middle), len(lower))
	}
	upper, middle, lower = data.BollingerBands(bars, 20, -1)
	if len(upper) != 0 || len(middle) != 0 || len(lower) != 0 {
		t.Fatalf("BollingerBands() with negative stdDev lens = (%d, %d, %d), want (0, 0, 0)", len(upper), len(middle), len(lower))
	}
	if got := data.ATR(bars, 0); len(got) != 0 {
		t.Fatalf("ATR() with period 0 len = %d, want 0", len(got))
	}
	if got := data.VWMA(bars, -1); len(got) != 0 {
		t.Fatalf("VWMA() with negative period len = %d, want 0", len(got))
	}
}

func TestComputeAllIndicatorsReturnsFullIndicatorSet(t *testing.T) {
	bars := indicatorTestBars(250)

	got := data.ComputeAllIndicators(bars)
	if len(got) != 14 {
		t.Fatalf("ComputeAllIndicators() map len = %d, want 14", len(got))
	}

	sma, ok := got["SMA"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[SMA] has unexpected type %T", got["SMA"])
	}
	assertClose(t, sma, data.SMA(bars, 20), 1e-6)

	ema, ok := got["EMA"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[EMA] has unexpected type %T", got["EMA"])
	}
	assertClose(t, ema, data.EMA(bars, 12), 1e-6)

	macd, ok := got["MACD"].(map[string][]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[MACD] has unexpected type %T", got["MACD"])
	}
	macdLine, signalLine, histogram := data.MACD(bars, 12, 26, 9)
	assertClose(t, macd["line"], macdLine, 1e-6)
	assertClose(t, macd["signal"], signalLine, 1e-6)
	assertClose(t, macd["histogram"], histogram, 1e-6)

	rsi, ok := got["RSI"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[RSI] has unexpected type %T", got["RSI"])
	}
	assertClose(t, rsi, data.RSI(bars, 14), 1e-6)

	mfi, ok := got["MFI"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[MFI] has unexpected type %T", got["MFI"])
	}
	assertClose(t, mfi, data.MFI(bars, 14), 1e-6)

	stochastic, ok := got["Stochastic"].(map[string][]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[Stochastic] has unexpected type %T", got["Stochastic"])
	}
	k, d := data.Stochastic(bars, 14, 3, 3)
	assertClose(t, stochastic["k"], k, 1e-6)
	assertClose(t, stochastic["d"], d, 1e-6)

	williamsR, ok := got["WilliamsR"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[WilliamsR] has unexpected type %T", got["WilliamsR"])
	}
	assertClose(t, williamsR, data.WilliamsR(bars, 14), 1e-6)

	cci, ok := got["CCI"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[CCI] has unexpected type %T", got["CCI"])
	}
	assertClose(t, cci, data.CCI(bars, 20), 1e-6)

	roc, ok := got["ROC"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[ROC] has unexpected type %T", got["ROC"])
	}
	assertClose(t, roc, data.ROC(bars, 12), 1e-6)

	bollinger, ok := got["BollingerBands"].(map[string][]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[BollingerBands] has unexpected type %T", got["BollingerBands"])
	}
	upper, middle, lower := data.BollingerBands(bars, 20, 2.0)
	assertClose(t, bollinger["upper"], upper, 1e-6)
	assertClose(t, bollinger["middle"], middle, 1e-6)
	assertClose(t, bollinger["lower"], lower, 1e-6)

	atr, ok := got["ATR"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[ATR] has unexpected type %T", got["ATR"])
	}
	assertClose(t, atr, data.ATR(bars, 14), 1e-6)

	vwma, ok := got["VWMA"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[VWMA] has unexpected type %T", got["VWMA"])
	}
	assertClose(t, vwma, data.VWMA(bars, 20), 1e-6)

	obv, ok := got["OBV"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[OBV] has unexpected type %T", got["OBV"])
	}
	assertClose(t, obv, data.OBV(bars), 1e-6)

	adl, ok := got["ADL"].([]float64)
	if !ok {
		t.Fatalf("ComputeAllIndicators()[ADL] has unexpected type %T", got["ADL"])
	}
	assertClose(t, adl, data.ADL(bars), 1e-6)
}

func TestComputeAllIndicatorsWithInsufficientDataPreservesContract(t *testing.T) {
	cases := []struct {
		name string
		bars []domain.OHLCV
	}{
		{name: "empty", bars: nil},
		{name: "small", bars: indicatorTestBars(10)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := data.ComputeAllIndicators(tc.bars)
			if len(got) != 14 {
				t.Fatalf("ComputeAllIndicators() map len = %d, want 14", len(got))
			}

			macd, ok := got["MACD"].(map[string][]float64)
			if !ok {
				t.Fatalf("ComputeAllIndicators()[MACD] has unexpected type %T", got["MACD"])
			}
			if _, ok := macd["line"]; !ok {
				t.Fatal("ComputeAllIndicators()[MACD] missing line key")
			}
			if _, ok := macd["signal"]; !ok {
				t.Fatal("ComputeAllIndicators()[MACD] missing signal key")
			}
			if _, ok := macd["histogram"]; !ok {
				t.Fatal("ComputeAllIndicators()[MACD] missing histogram key")
			}

			stochastic, ok := got["Stochastic"].(map[string][]float64)
			if !ok {
				t.Fatalf("ComputeAllIndicators()[Stochastic] has unexpected type %T", got["Stochastic"])
			}
			if _, ok := stochastic["k"]; !ok {
				t.Fatal("ComputeAllIndicators()[Stochastic] missing k key")
			}
			if _, ok := stochastic["d"]; !ok {
				t.Fatal("ComputeAllIndicators()[Stochastic] missing d key")
			}

			bollinger, ok := got["BollingerBands"].(map[string][]float64)
			if !ok {
				t.Fatalf("ComputeAllIndicators()[BollingerBands] has unexpected type %T", got["BollingerBands"])
			}
			if _, ok := bollinger["upper"]; !ok {
				t.Fatal("ComputeAllIndicators()[BollingerBands] missing upper key")
			}
			if _, ok := bollinger["middle"]; !ok {
				t.Fatal("ComputeAllIndicators()[BollingerBands] missing middle key")
			}
			if _, ok := bollinger["lower"]; !ok {
				t.Fatal("ComputeAllIndicators()[BollingerBands] missing lower key")
			}
		})
	}
}

func TestIndicatorEdgeCasesWithFlatAndZeroVolumeData(t *testing.T) {
	tests := []struct {
		name   string
		assert func(t *testing.T, bars []domain.OHLCV)
	}{
		{
			name: "RSI returns neutral values for flat closes",
			assert: func(t *testing.T, bars []domain.OHLCV) {
				assertClose(t, data.RSI(bars, 14), repeatFloat(50, len(bars)-14), 1e-6)
			},
		},
		{
			name: "MFI returns neutral values for flat typical prices",
			assert: func(t *testing.T, bars []domain.OHLCV) {
				assertClose(t, data.MFI(bars, 14), repeatFloat(50, len(bars)-14), 1e-6)
			},
		},
		{
			name: "Stochastic returns zero when window range is flat",
			assert: func(t *testing.T, bars []domain.OHLCV) {
				k, d := data.Stochastic(bars, 14, 3, 3)
				assertClose(t, k, repeatFloat(0, len(k)), 1e-6)
				assertClose(t, d, repeatFloat(0, len(d)), 1e-6)
			},
		},
		{
			name: "WilliamsR returns zero when window range is flat",
			assert: func(t *testing.T, bars []domain.OHLCV) {
				assertClose(t, data.WilliamsR(bars, 14), repeatFloat(0, len(bars)-13), 1e-6)
			},
		},
		{
			name: "CCI returns zero when mean deviation is zero",
			assert: func(t *testing.T, bars []domain.OHLCV) {
				assertClose(t, data.CCI(bars, 20), repeatFloat(0, len(bars)-19), 1e-6)
			},
		},
		{
			name: "VWMA returns zero when every window volume is zero",
			assert: func(t *testing.T, _ []domain.OHLCV) {
				zeroVolumeBars := flatIndicatorTestBars(25, 100, 0)
				assertClose(t, data.VWMA(zeroVolumeBars, 20), repeatFloat(0, 6), 1e-6)
			},
		},
	}

	bars := flatIndicatorTestBars(25, 100, 1_000)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.assert(t, bars)
		})
	}
}

func BenchmarkComputeAllIndicators(b *testing.B) {
	bars := indicatorTestBars(500)
	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		computeAllIndicatorsSink = data.ComputeAllIndicators(bars)
	}
}

func flatIndicatorTestBars(count int, price, volume float64) []domain.OHLCV {
	bars := make([]domain.OHLCV, count)
	start := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range bars {
		bars[i] = domain.OHLCV{
			Timestamp: start.Add(time.Duration(i) * time.Minute),
			Open:      price,
			High:      price,
			Low:       price,
			Close:     price,
			Volume:    volume,
		}
	}

	return bars
}

func repeatFloat(value float64, count int) []float64 {
	values := make([]float64, count)
	for i := range values {
		values[i] = value
	}

	return values
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

func volumeIndicatorTestBars() []domain.OHLCV {
	start := time.Unix(0, 0).UTC()

	return []domain.OHLCV{
		{Timestamp: start, Open: 8.5, High: 10, Low: 8, Close: 9, Volume: 100},
		{Timestamp: start.Add(time.Hour), Open: 9.5, High: 11, Low: 9, Close: 10.5, Volume: 150},
		{Timestamp: start.Add(2 * time.Hour), Open: 10.75, High: 12, Low: 10, Close: 10.25, Volume: 120},
		{Timestamp: start.Add(3 * time.Hour), Open: 10, High: 11, Low: 9, Close: 9, Volume: 130},
		{Timestamp: start.Add(4 * time.Hour), Open: 11.5, High: 13, Low: 11, Close: 12.5, Volume: 160},
		{Timestamp: start.Add(5 * time.Hour), Open: 13, High: 13, Low: 13, Close: 13, Volume: 200},
	}
}

func assertClose(t *testing.T, got, want []float64, delta float64) {
	t.Helper()

	if len(got) != len(want) {
		t.Fatalf("got %d values, want %d", len(got), len(want))
	}

	for i, expected := range want {
		actual := got[i]
		if math.Abs(actual-expected) > delta {
			t.Fatalf("value[%d] = %.6f, want %.6f (delta %.6f)", i, actual, expected, delta)
		}
	}
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
