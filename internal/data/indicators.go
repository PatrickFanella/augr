package data

import (
	"math"
	"sort"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	defaultSMAPeriod          = 20
	defaultEMAPeriod          = 12
	defaultMACDFastPeriod     = 12
	defaultMACDSlowPeriod     = 26
	defaultMACDSignalPeriod   = 9
	defaultRSIPeriod          = 14
	defaultMFIPeriod          = 14
	defaultStochasticKPeriod  = 14
	defaultStochasticDPeriod  = 3
	defaultStochasticSmooth   = 3
	defaultWilliamsRPeriod    = 14
	defaultCCIPeriod          = 20
	defaultROCPeriod          = 12
	defaultBollingerPeriod    = 20
	defaultBollingerStdDev    = 2.0
	defaultATRPeriod          = 14
	defaultVWMAPeriod         = 20
)

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
	positiveFlows := make([]float64, len(data))
	negativeFlows := make([]float64, len(data))
	for i := 1; i < len(data); i++ {
		flow := typicalPrices[i] * data[i].Volume
		switch {
		case typicalPrices[i] > typicalPrices[i-1]:
			positiveFlows[i] = flow
		case typicalPrices[i] < typicalPrices[i-1]:
			negativeFlows[i] = flow
		}
	}

	series := make([]float64, len(data)-period)
	positiveFlow := 0.0
	negativeFlow := 0.0
	for i := 1; i <= period; i++ {
		positiveFlow += positiveFlows[i]
		negativeFlow += negativeFlows[i]
	}
	series[0] = moneyFlowIndex(positiveFlow, negativeFlow)

	for end := period + 1; end < len(data); end++ {
		positiveFlow += positiveFlows[end] - positiveFlows[end-period]
		negativeFlow += negativeFlows[end] - negativeFlows[end-period]
		series[end-period] = moneyFlowIndex(positiveFlow, negativeFlow)
	}

	return series
}

// Stochastic computes the smoothed %K and %D stochastic oscillator values.
func Stochastic(data []domain.OHLCV, kPeriod, dPeriod, smooth int) (k, d []float64) {
	if kPeriod <= 0 || dPeriod <= 0 || smooth <= 0 || len(data) < kPeriod+smooth+dPeriod-2 {
		return nil, nil
	}

	highs, lows := rollingHighLowSeries(data, kPeriod)
	rawK := make([]float64, len(highs))
	for i := range rawK {
		high := highs[i]
		low := lows[i]
		if high == low {
			rawK[i] = 0
			continue
		}

		rawK[i] = (data[i+kPeriod-1].Close - low) / (high - low) * 100
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

	highs, lows := rollingHighLowSeries(data, period)
	series := make([]float64, len(highs))
	for i := range series {
		high := highs[i]
		low := lows[i]
		if high == low {
			series[i] = 0
			continue
		}

		series[i] = (high - data[i+period-1].Close) / (high - low) * -100
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

	meanDeviations := meanAbsoluteDeviationSeries(typicalPrices, period, sma)
	series := make([]float64, len(sma))
	for i, average := range sma {
		meanDeviation := meanDeviations[i]
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

// BollingerBands returns the upper, middle, and lower bands using an SMA basis
// and population standard deviation over each completed window.
func BollingerBands(data []domain.OHLCV, period int, stdDev float64) (upper, middle, lower []float64) {
	if period <= 0 || stdDev < 0 || len(data) < period {
		return nil, nil, nil
	}

	closes := closePrices(data)
	middle = smaSeries(closes, period)
	if len(middle) == 0 {
		return nil, nil, nil
	}

	upper = make([]float64, len(middle))
	lower = make([]float64, len(middle))
	windowSumSquares := 0.0
	for _, close := range closes[:period] {
		windowSumSquares += close * close
	}

	for i, average := range middle {
		variance := windowSumSquares/float64(period) - average*average
		if variance < 0 {
			variance = 0
		}

		offset := math.Sqrt(variance) * stdDev
		upper[i] = average + offset
		lower[i] = average - offset

		end := i + period
		if end < len(closes) {
			windowSumSquares += closes[end]*closes[end] - closes[i]*closes[i]
		}
	}

	return upper, middle, lower
}

// ATR returns the Average True Range using Wilder smoothing.
func ATR(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period {
		return nil
	}

	trueRanges := make([]float64, len(data))
	trueRanges[0] = data[0].High - data[0].Low
	for i := 1; i < len(data); i++ {
		highLow := data[i].High - data[i].Low
		highClose := math.Abs(data[i].High - data[i-1].Close)
		lowClose := math.Abs(data[i].Low - data[i-1].Close)
		trueRanges[i] = math.Max(highLow, math.Max(highClose, lowClose))
	}

	series := make([]float64, len(data)-period+1)
	atr := 0.0
	for _, trueRange := range trueRanges[:period] {
		atr += trueRange
	}
	atr /= float64(period)
	series[0] = atr

	for i := period; i < len(trueRanges); i++ {
		atr = (atr*float64(period-1) + trueRanges[i]) / float64(period)
		series[i-period+1] = atr
	}

	return series
}

// VWMA returns the volume-weighted moving average of closing prices for each completed window.
func VWMA(data []domain.OHLCV, period int) []float64 {
	if period <= 0 || len(data) < period {
		return nil
	}

	series := make([]float64, len(data)-period+1)
	priceVolumeSum := 0.0
	volumeSum := 0.0
	for _, bar := range data[:period] {
		priceVolumeSum += bar.Close * bar.Volume
		volumeSum += bar.Volume
	}
	series[0] = volumeWeightedAverage(priceVolumeSum, volumeSum)

	for i := period; i < len(data); i++ {
		priceVolumeSum += data[i].Close*data[i].Volume - data[i-period].Close*data[i-period].Volume
		volumeSum += data[i].Volume - data[i-period].Volume
		series[i-period+1] = volumeWeightedAverage(priceVolumeSum, volumeSum)
	}

	return series
}

// OBV returns the cumulative On-Balance Volume series.
func OBV(data []domain.OHLCV) []float64 {
	if len(data) == 0 {
		return nil
	}

	series := make([]float64, len(data))
	for i := 1; i < len(data); i++ {
		series[i] = series[i-1]
		switch {
		case data[i].Close > data[i-1].Close:
			series[i] += data[i].Volume
		case data[i].Close < data[i-1].Close:
			series[i] -= data[i].Volume
		}
	}

	return series
}

// ADL returns the cumulative Accumulation/Distribution Line series.
func ADL(data []domain.OHLCV) []float64 {
	if len(data) == 0 {
		return nil
	}

	series := make([]float64, len(data))
	for i, bar := range data {
		if i > 0 {
			series[i] = series[i-1]
		}

		rangeWidth := bar.High - bar.Low
		if rangeWidth == 0 {
			continue
		}

		multiplier := ((bar.Close - bar.Low) - (bar.High - bar.Close)) / rangeWidth
		series[i] += multiplier * bar.Volume
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

// ComputeAllIndicators computes all 14 technical indicators using standard
// default periods and returns them in a single map.
func ComputeAllIndicators(bars []domain.OHLCV) map[string]any {
	macdLine, signalLine, histogram := MACD(bars, defaultMACDFastPeriod, defaultMACDSlowPeriod, defaultMACDSignalPeriod)
	stochasticK, stochasticD := Stochastic(bars, defaultStochasticKPeriod, defaultStochasticDPeriod, defaultStochasticSmooth)
	bollingerUpper, bollingerMiddle, bollingerLower := BollingerBands(bars, defaultBollingerPeriod, defaultBollingerStdDev)

	return map[string]any{
		"SMA": SMA(bars, defaultSMAPeriod),
		"EMA": EMA(bars, defaultEMAPeriod),
		"MACD": map[string][]float64{
			"line":      macdLine,
			"signal":    signalLine,
			"histogram": histogram,
		},
		"RSI":        RSI(bars, defaultRSIPeriod),
		"MFI":        MFI(bars, defaultMFIPeriod),
		"Stochastic": map[string][]float64{"k": stochasticK, "d": stochasticD},
		"WilliamsR":  WilliamsR(bars, defaultWilliamsRPeriod),
		"CCI":        CCI(bars, defaultCCIPeriod),
		"ROC":        ROC(bars, defaultROCPeriod),
		"BollingerBands": map[string][]float64{
			"upper":  bollingerUpper,
			"middle": bollingerMiddle,
			"lower":  bollingerLower,
		},
		"ATR":  ATR(bars, defaultATRPeriod),
		"VWMA": VWMA(bars, defaultVWMAPeriod),
		"OBV":  OBV(bars),
		"ADL":  ADL(bars),
	}
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

// rollingHighLowSeries computes each completed window's highest high and lowest
// low in O(n) time using monotonic deques of source indices.
func rollingHighLowSeries(data []domain.OHLCV, period int) (highs, lows []float64) {
	if period <= 0 || len(data) < period {
		return nil, nil
	}

	highs = make([]float64, len(data)-period+1)
	lows = make([]float64, len(data)-period+1)
	// Maintain monotonic deques of indices so each window can read its current
	// highest high and lowest low in O(1) amortized time.
	highDeque := make([]int, 0, period)
	lowDeque := make([]int, 0, period)

	for i, bar := range data {
		for len(highDeque) > 0 && bar.High >= data[highDeque[len(highDeque)-1]].High {
			highDeque = highDeque[:len(highDeque)-1]
		}
		highDeque = append(highDeque, i)

		for len(lowDeque) > 0 && bar.Low <= data[lowDeque[len(lowDeque)-1]].Low {
			lowDeque = lowDeque[:len(lowDeque)-1]
		}
		lowDeque = append(lowDeque, i)

		windowStart := i - period + 1
		if windowStart < 0 {
			continue
		}

		for len(highDeque) > 0 && highDeque[0] < windowStart {
			highDeque = highDeque[1:]
		}
		for len(lowDeque) > 0 && lowDeque[0] < windowStart {
			lowDeque = lowDeque[1:]
		}

		highs[windowStart] = data[highDeque[0]].High
		lows[windowStart] = data[lowDeque[0]].Low
	}

	return highs, lows
}

// meanAbsoluteDeviationSeries computes rolling mean absolute deviations around
// the provided window means in O(n log n) time. It coordinate-compresses the
// input values and uses Fenwick trees for window counts and sums so each
// rolling update avoids rescanning the full window.
func meanAbsoluteDeviationSeries(values []float64, period int, means []float64) []float64 {
	if period <= 0 || len(values) < period || len(means) != len(values)-period+1 {
		return nil
	}

	sortedValues := uniqueSorted(values)
	indices := make([]int, len(values))
	for i, value := range values {
		// Every value in the input is present in sortedValues because uniqueSorted
		// is built directly from values. The +1 converts the 0-based search result
		// to the 1-based indexing used by the Fenwick tree.
		indices[i] = sort.SearchFloat64s(sortedValues, value) + 1
	}

	counts := newFenwickTree(len(sortedValues))
	sums := newFenwickTree(len(sortedValues))
	for i := 0; i < period; i++ {
		counts.add(indices[i], 1)
		sums.add(indices[i], values[i])
	}

	deviations := make([]float64, len(means))
	for start, mean := range means {
		deviations[start] = meanAbsoluteDeviation(counts, sums, sortedValues, mean, float64(period))
		if start == len(means)-1 {
			break
		}

		counts.add(indices[start], -1)
		sums.add(indices[start], -values[start])

		end := start + period
		counts.add(indices[end], 1)
		sums.add(indices[end], values[end])
	}

	return deviations
}

// meanAbsoluteDeviation computes the mean absolute deviation around the window mean from the
// Fenwick-backed counts and sums for the current rolling window.
func meanAbsoluteDeviation(counts, sums fenwickTree, sortedValues []float64, mean, totalCount float64) float64 {
	// Find the first value strictly greater than the mean so values [0:leftIndex)
	// are <= mean and values [leftIndex:] are > mean.
	leftIndex := sort.Search(len(sortedValues), func(i int) bool {
		return sortedValues[i] > mean
	})
	leftCount := counts.sum(leftIndex)
	leftSum := sums.sum(leftIndex)
	totalSum := sums.sum(len(sortedValues))
	rightCount := totalCount - leftCount
	rightSum := totalSum - leftSum

	// Sum of absolute deviations split around the mean:
	//   sum(mean-value) for value <= mean  => mean*leftCount - leftSum
	//   sum(value-mean) for value > mean   => rightSum - mean*rightCount
	return (mean*leftCount - leftSum + rightSum - mean*rightCount) / totalCount
}

// uniqueSorted returns a sorted, deduplicated copy of values for coordinate
// compression in the Fenwick-tree rolling deviation calculation.
func uniqueSorted(values []float64) []float64 {
	cloned := append([]float64(nil), values...)
	sort.Float64s(cloned)

	unique := make([]float64, 0, len(cloned))
	for _, value := range cloned {
		if len(unique) == 0 || value != unique[len(unique)-1] {
			unique = append(unique, value)
		}
	}

	return unique
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

func volumeWeightedAverage(priceVolumeSum, volumeSum float64) float64 {
	if volumeSum == 0 {
		return 0
	}

	return priceVolumeSum / volumeSum
}

type fenwickTree []float64

// newFenwickTree allocates one extra slot because Fenwick trees use 1-based
// indexing and leave index 0 unused.
func newFenwickTree(size int) fenwickTree {
	return make(fenwickTree, size+1)
}

func (tree fenwickTree) add(index int, value float64) {
	if index <= 0 || index >= len(tree) {
		panic("fenwickTree.add: index out of bounds")
	}

	for index < len(tree) {
		tree[index] += value
		index += index & -index
	}
}

func (tree fenwickTree) sum(index int) float64 {
	if index < 0 || index >= len(tree) {
		panic("fenwickTree.sum: index out of bounds")
	}

	total := 0.0
	for index > 0 {
		total += tree[index]
		index -= index & -index
	}

	return total
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
