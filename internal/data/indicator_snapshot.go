package data

import (
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// IndicatorSnapshotFromBars computes the standard set of 21 technical indicator
// scalars from the provided OHLCV bars. Each indicator uses the last value of
// its series. Returns nil if bars is empty.
func IndicatorSnapshotFromBars(bars []domain.OHLCV) []domain.Indicator {
	if len(bars) == 0 {
		return nil
	}

	timestamp := bars[len(bars)-1].Timestamp
	indicators := make([]domain.Indicator, 0, 21)
	appendLatestIndicator(&indicators, "sma_20", SMA(bars, 20), timestamp)
	appendLatestIndicator(&indicators, "sma_50", SMA(bars, 50), timestamp)
	appendLatestIndicator(&indicators, "sma_200", SMA(bars, 200), timestamp)
	appendLatestIndicator(&indicators, "ema_12", EMA(bars, 12), timestamp)
	appendLatestIndicator(&indicators, "rsi_14", RSI(bars, 14), timestamp)
	appendLatestIndicator(&indicators, "mfi_14", MFI(bars, 14), timestamp)
	appendLatestIndicator(&indicators, "williams_r_14", WilliamsR(bars, 14), timestamp)
	appendLatestIndicator(&indicators, "cci_20", CCI(bars, 20), timestamp)
	appendLatestIndicator(&indicators, "roc_12", ROC(bars, 12), timestamp)
	appendLatestIndicator(&indicators, "atr_14", ATR(bars, 14), timestamp)
	appendLatestIndicator(&indicators, "vwma_20", VWMA(bars, 20), timestamp)
	appendLatestIndicator(&indicators, "obv", OBV(bars), timestamp)
	appendLatestIndicator(&indicators, "adl", ADL(bars), timestamp)

	macdLine, macdSignal, macdHistogram := MACD(bars, 12, 26, 9)
	appendLatestIndicator(&indicators, "macd_line", macdLine, timestamp)
	appendLatestIndicator(&indicators, "macd_signal", macdSignal, timestamp)
	appendLatestIndicator(&indicators, "macd_histogram", macdHistogram, timestamp)

	stochasticK, stochasticD := Stochastic(bars, 14, 3, 3)
	appendLatestIndicator(&indicators, "stochastic_k", stochasticK, timestamp)
	appendLatestIndicator(&indicators, "stochastic_d", stochasticD, timestamp)

	bollingerUpper, bollingerMiddle, bollingerLower := BollingerBands(bars, 20, 2)
	appendLatestIndicator(&indicators, "bollinger_upper", bollingerUpper, timestamp)
	appendLatestIndicator(&indicators, "bollinger_middle", bollingerMiddle, timestamp)
	appendLatestIndicator(&indicators, "bollinger_lower", bollingerLower, timestamp)

	return indicators
}

func appendLatestIndicator(indicators *[]domain.Indicator, name string, series []float64, timestamp time.Time) {
	if len(series) == 0 {
		return
	}
	*indicators = append(*indicators, domain.Indicator{
		Name:      name,
		Value:     series[len(series)-1],
		Timestamp: timestamp,
	})
}
