package data

import "github.com/PatrickFanella/get-rich-quick/internal/domain"

// SMA returns the simple moving average of closing prices for each completed window.
func SMA(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period {
		return nil
	}

	closes := closePrices(data)
	return smaSeries(closes, period)
}

// EMA returns the exponential moving average of closing prices for each completed window.
func EMA(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period {
		return nil
	}

	closes := closePrices(data)
	return emaSeries(closes, period)
}

// MACD computes the Moving Average Convergence Divergence indicator for OHLCV bars.
// It derives closing prices from the provided data and returns three slices:
//   - macdLine: aligned to each completed slow EMA window.
//   - signalLine: EMA of macdLine, aligned to each completed signal window.
//   - histogram: difference between the corresponding aligned MACD and signal values.
//
// All slices preserve the input time order. If the parameters are invalid or there is
// insufficient data, all three return values are nil.
func MACD(data []domain.OHLCV, fast, slow, signal int) (macdLine, signalLine, histogram []float64) {
	if fast <= 0 || slow <= 0 || signal <= 0 || fast >= slow || len(data) < slow+signal-1 {
		return nil, nil, nil
	}

	closes := closePrices(data)
	fastEMA := emaSeries(closes, fast)
	slowEMA := emaSeries(closes, slow)
	if len(fastEMA) == 0 || len(slowEMA) == 0 {
		return nil, nil, nil
	}

	offset := slow - fast
	macdLine = make([]float64, len(slowEMA))
	for i := range slowEMA {
		macdLine[i] = fastEMA[i+offset] - slowEMA[i]
	}

	signalLine = emaSeries(macdLine, signal)
	if len(signalLine) == 0 {
		return nil, nil, nil
	}

	histogram = make([]float64, len(signalLine))
	for i := range signalLine {
		histogram[i] = macdLine[i+signal-1] - signalLine[i]
	}

	return macdLine, signalLine, histogram
}

func closePrices(data []domain.OHLCV) []float64 {
	closes := make([]float64, len(data))
	for i, bar := range data {
		closes[i] = bar.Close
	}

	return closes
}

func smaSeries(values []float64, period int) []float64 {
	if period <= 0 || len(values) < period {
		return nil
	}

	series := make([]float64, len(values)-period+1)
	sum := 0.0
	for _, value := range values[:period] {
		sum += value
	}
	series[0] = sum / float64(period)

	for i := period; i < len(values); i++ {
		sum += values[i] - values[i-period]
		series[i-period+1] = sum / float64(period)
	}

	return series
}

func emaSeries(values []float64, period int) []float64 {
	if period <= 0 || len(values) < period {
		return nil
	}

	series := make([]float64, len(values)-period+1)
	multiplier := 2.0 / float64(period+1)

	ema := 0.0
	for _, value := range values[:period] {
		ema += value
	}
	ema /= float64(period)
	series[0] = ema

	for i := period; i < len(values); i++ {
		ema = (values[i]-ema)*multiplier + ema
		series[i-period+1] = ema
	}

	return series
}
