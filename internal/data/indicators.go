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

// RSI returns the Relative Strength Index of closing prices using Wilder smoothing.
func RSI(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period+1 {
		return nil
	}

	series := make([]float64, len(data)-period)
	avgGain := 0.0
	avgLoss := 0.0

	for i := 1; i <= period; i++ {
		change := data[i].Close - data[i-1].Close
		if change > 0 {
			avgGain += change
		} else {
			avgLoss -= change
		}
	}

	avgGain /= float64(period)
	avgLoss /= float64(period)
	series[0] = relativeStrengthIndex(avgGain, avgLoss)

	for i := period + 1; i < len(data); i++ {
		change := data[i].Close - data[i-1].Close
		gain := 0.0
		loss := 0.0
		if change > 0 {
			gain = change
		} else {
			loss = -change
		}

		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)
		series[i-period] = relativeStrengthIndex(avgGain, avgLoss)
	}

	return series
}

// MFI returns the Money Flow Index based on typical price and volume.
func MFI(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period+1 {
		return nil
	}

	typicalPrices := typicalPrices(data)
	series := make([]float64, len(data)-period)

	for end := period; end < len(data); end++ {
		positiveFlow := 0.0
		negativeFlow := 0.0

		for i := end - period + 1; i <= end; i++ {
			flow := typicalPrices[i] * data[i].Volume
			switch {
			case typicalPrices[i] > typicalPrices[i-1]:
				positiveFlow += flow
			case typicalPrices[i] < typicalPrices[i-1]:
				negativeFlow += flow
			}
		}

		series[end-period] = moneyFlowIndex(positiveFlow, negativeFlow)
	}

	return series
}

// Stochastic computes the smoothed %K and %D stochastic oscillator values.
func Stochastic(data []domain.OHLCV, kPeriod, dPeriod, smooth int) (k, d []float64) {
	if kPeriod <= 0 || dPeriod <= 0 || smooth <= 0 || len(data) < kPeriod+smooth+dPeriod-2 {
		return nil, nil
	}

	rawK := make([]float64, len(data)-kPeriod+1)
	for end := kPeriod - 1; end < len(data); end++ {
		high, low := windowHighLow(data[end-kPeriod+1 : end+1])
		if high == low {
			rawK[end-kPeriod+1] = 0
			continue
		}

		rawK[end-kPeriod+1] = (data[end].Close - low) / (high - low) * 100
	}

	k = smaSeries(rawK, smooth)
	d = smaSeries(k, dPeriod)
	if len(k) == 0 || len(d) == 0 {
		return nil, nil
	}

	return k, d
}

// WilliamsR returns the Williams %R oscillator for each completed window.
func WilliamsR(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period {
		return nil
	}

	series := make([]float64, len(data)-period+1)
	for end := period - 1; end < len(data); end++ {
		high, low := windowHighLow(data[end-period+1 : end+1])
		if high == low {
			series[end-period+1] = 0
			continue
		}

		series[end-period+1] = (high - data[end].Close) / (high - low) * -100
	}

	return series
}

// CCI returns the Commodity Channel Index for each completed window.
func CCI(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period {
		return nil
	}

	typicalPrices := typicalPrices(data)
	sma := smaSeries(typicalPrices, period)
	if len(sma) == 0 {
		return nil
	}

	series := make([]float64, len(sma))
	for i, average := range sma {
		meanDeviation := 0.0
		for _, value := range typicalPrices[i : i+period] {
			if value > average {
				meanDeviation += value - average
				continue
			}
			meanDeviation += average - value
		}

		meanDeviation /= float64(period)
		if meanDeviation == 0 {
			series[i] = 0
			continue
		}

		series[i] = (typicalPrices[i+period-1] - average) / (0.015 * meanDeviation)
	}

	return series
}

// ROC returns the percentage rate of change of closing prices.
func ROC(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period+1 {
		return nil
	}

	series := make([]float64, len(data)-period)
	for i := period; i < len(data); i++ {
		base := data[i-period].Close
		if base == 0 {
			series[i-period] = 0
			continue
		}

		series[i-period] = (data[i].Close - base) / base * 100
	}

	return series
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

func typicalPrices(data []domain.OHLCV) []float64 {
	prices := make([]float64, len(data))
	for i, bar := range data {
		prices[i] = (bar.High + bar.Low + bar.Close) / 3
	}

	return prices
}

func windowHighLow(data []domain.OHLCV) (high, low float64) {
	high = data[0].High
	low = data[0].Low

	for _, bar := range data[1:] {
		if bar.High > high {
			high = bar.High
		}
		if bar.Low < low {
			low = bar.Low
		}
	}

	return high, low
}

func relativeStrengthIndex(avgGain, avgLoss float64) float64 {
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}

		return 100
	}

	rs := avgGain / avgLoss
	return 100 - (100 / (1 + rs))
}

func moneyFlowIndex(positiveFlow, negativeFlow float64) float64 {
	if negativeFlow == 0 {
		if positiveFlow == 0 {
			return 50
		}

		return 100
	}

	ratio := positiveFlow / negativeFlow
	return 100 - (100 / (1 + ratio))
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
