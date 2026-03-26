package backtest

import (
	"math"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	// annualTradingDays is the standard number of trading days used when
	// annualising return and volatility metrics.
	annualTradingDays = 252
)

// Metrics holds computed performance statistics derived from an equity curve.
type Metrics struct {
	TotalReturn      float64 // (final equity − initial equity) / initial equity
	BuyAndHoldReturn float64 // passive return from first to last benchmark close
	MaxDrawdown      float64 // worst peak-to-trough drawdown (positive value)
	CalmarRatio      float64 // annualised return / max drawdown
	SharpeRatio      float64 // annualised risk-adjusted return (risk-free = 0)
	SortinoRatio     float64 // annualised downside risk-adjusted return
	Alpha            float64 // annualised excess return beyond benchmark exposure
	Beta             float64 // covariance(strategy, benchmark) / variance(benchmark)
	InformationRatio float64 // annualised mean active return / tracking error
	WinRate          float64 // fraction of bars with positive returns
	ProfitFactor     float64 // gross profits / gross losses (Inf when no losses)
	AvgWinLossRatio  float64 // average positive return / average absolute negative return
	Volatility       float64 // annualised standard deviation of returns
	StartEquity      float64
	EndEquity        float64
	StartTime        time.Time
	EndTime          time.Time
	TotalBars        int
	RealizedPnL      float64
	UnrealizedPnL    float64
}

// ComputeMetrics calculates performance metrics from an equity curve.
// At least two equity points are required to compute return-based metrics;
// with fewer points the struct is returned with zero-value fields and the
// start/end equity filled from whatever points are available.
func ComputeMetrics(curve []EquityPoint, benchmarkBars []domain.OHLCV) Metrics {
	if len(curve) == 0 {
		return Metrics{}
	}

	m := Metrics{
		TotalBars:     len(curve),
		StartEquity:   curve[0].Equity,
		EndEquity:     curve[len(curve)-1].Equity,
		StartTime:     curve[0].Timestamp,
		EndTime:       curve[len(curve)-1].Timestamp,
		RealizedPnL:   curve[len(curve)-1].RealizedPnL,
		UnrealizedPnL: curve[len(curve)-1].UnrealizedPnL,
	}

	if len(curve) < 2 {
		return m
	}

	// Total return.
	if m.StartEquity != 0 {
		m.TotalReturn = (m.EndEquity - m.StartEquity) / m.StartEquity
	}

	// Per-bar simple returns.
	returns := make([]float64, 0, len(curve)-1)
	for i := 1; i < len(curve); i++ {
		prev := curve[i-1].Equity
		if prev == 0 {
			returns = append(returns, 0)
			continue
		}
		returns = append(returns, (curve[i].Equity-prev)/prev)
	}

	sortedBenchmarkBars := sortedBarsByTimestamp(benchmarkBars)
	m.BuyAndHoldReturn = buyAndHoldReturn(sortedBenchmarkBars)
	benchmarkReturns := barReturns(sortedBenchmarkBars)
	alignedStrategy, alignedBenchmark := alignReturnSeries(returns, benchmarkReturns)
	if len(alignedStrategy) >= 2 {
		b, benchmarkVariance := beta(alignedStrategy, alignedBenchmark)
		m.Beta = b
		m.Alpha = alpha(alignedStrategy, alignedBenchmark, b, benchmarkVariance)
		m.InformationRatio = informationRatio(alignedStrategy, alignedBenchmark)
	}

	// Max drawdown.
	peak := curve[0].Equity
	for i := 1; i < len(curve); i++ {
		if curve[i].Equity > peak {
			peak = curve[i].Equity
		}
		if peak > 0 {
			dd := (peak - curve[i].Equity) / peak
			if dd > m.MaxDrawdown {
				m.MaxDrawdown = dd
			}
		}
	}

	// Mean and standard deviation of returns.
	meanRet := mean(returns)
	stdDev := stddev(returns, meanRet)

	m.Volatility = stdDev * math.Sqrt(annualTradingDays)

	// Sharpe ratio (risk-free rate assumed 0).
	if stdDev > 0 {
		m.SharpeRatio = (meanRet / stdDev) * math.Sqrt(annualTradingDays)
	}

	// Sortino ratio (downside deviation).
	downDev := downsideDeviation(returns, 0)
	if downDev > 0 {
		m.SortinoRatio = (meanRet / downDev) * math.Sqrt(annualTradingDays)
	}

	// Calmar ratio: CAGR / max drawdown.
	if m.MaxDrawdown > 0 && m.StartEquity > 0 && !m.StartTime.IsZero() && !m.EndTime.IsZero() && m.EndTime.After(m.StartTime) {
		years := m.EndTime.Sub(m.StartTime).Hours() / (24.0 * 365.25)
		if years > 0 {
			equityRatio := m.EndEquity / m.StartEquity
			if equityRatio > 0 {
				cagr := math.Pow(equityRatio, 1.0/years) - 1.0
				m.CalmarRatio = cagr / m.MaxDrawdown
			}
		}
	}

	// Win rate and profit factor.
	var wins, losses int
	var grossProfit, grossLoss float64
	for _, r := range returns {
		if r > 0 {
			wins++
			grossProfit += r
		} else if r < 0 {
			losses++
			grossLoss += math.Abs(r)
		}
	}

	total := wins + losses
	if total > 0 {
		m.WinRate = float64(wins) / float64(total)
	}
	if grossLoss > 0 {
		m.ProfitFactor = grossProfit / grossLoss
	} else if grossProfit > 0 {
		m.ProfitFactor = math.Inf(1)
	}
	if wins > 0 && losses > 0 {
		avgWin := grossProfit / float64(wins)
		avgLoss := grossLoss / float64(losses)
		if avgLoss > 0 {
			m.AvgWinLossRatio = avgWin / avgLoss
		}
	} else if wins > 0 {
		m.AvgWinLossRatio = math.Inf(1)
	}

	return m
}

func buyAndHoldReturn(bars []domain.OHLCV) float64 {
	if len(bars) < 2 || bars[0].Close == 0 {
		return 0
	}
	return (bars[len(bars)-1].Close - bars[0].Close) / bars[0].Close
}

func sortedBarsByTimestamp(bars []domain.OHLCV) []domain.OHLCV {
	sorted := append([]domain.OHLCV(nil), bars...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].Timestamp.Before(sorted[j].Timestamp)
	})
	return sorted
}

func barReturns(bars []domain.OHLCV) []float64 {
	if len(bars) < 2 {
		return nil
	}
	returns := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		prev := bars[i-1].Close
		if prev == 0 {
			returns = append(returns, 0)
			continue
		}
		returns = append(returns, (bars[i].Close-prev)/prev)
	}
	return returns
}

func alignReturnSeries(a, b []float64) ([]float64, []float64) {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	if n <= 0 {
		return nil, nil
	}
	return a[:n], b[:n]
}

func beta(strategyReturns, benchmarkReturns []float64) (float64, float64) {
	meanStrategy := mean(strategyReturns)
	meanBenchmark := mean(benchmarkReturns)

	var covariance float64
	var variance float64
	for i := range strategyReturns {
		s := strategyReturns[i] - meanStrategy
		b := benchmarkReturns[i] - meanBenchmark
		covariance += s * b
		variance += b * b
	}
	if variance == 0 {
		return 0, 0
	}
	return covariance / variance, variance
}

func alpha(strategyReturns, benchmarkReturns []float64, b float64, benchmarkVariance float64) float64 {
	meanStrategy := mean(strategyReturns)
	meanBenchmark := mean(benchmarkReturns)

	if benchmarkVariance == 0 {
		perBarAlpha := meanStrategy - meanBenchmark
		return perBarAlpha * annualTradingDays
	}

	perBarAlpha := meanStrategy - (b * meanBenchmark)
	return perBarAlpha * annualTradingDays
}

func informationRatio(strategyReturns, benchmarkReturns []float64) float64 {
	active := make([]float64, len(strategyReturns))
	for i := range strategyReturns {
		active[i] = strategyReturns[i] - benchmarkReturns[i]
	}
	meanActive := mean(active)
	trackingError := stddev(active, meanActive)
	if trackingError == 0 {
		return 0
	}
	return (meanActive / trackingError) * math.Sqrt(annualTradingDays)
}

// mean returns the arithmetic mean of a float64 slice.
func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// stddev computes the population standard deviation given a pre-computed mean.
func stddev(values []float64, avg float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sumSq float64
	for _, v := range values {
		d := v - avg
		sumSq += d * d
	}
	return math.Sqrt(sumSq / float64(len(values)))
}

// downsideDeviation computes the root-mean-square of returns below the target.
func downsideDeviation(returns []float64, target float64) float64 {
	if len(returns) == 0 {
		return 0
	}
	var sumSq float64
	var count int
	for _, r := range returns {
		if r < target {
			d := r - target
			sumSq += d * d
			count++
		}
	}
	if count == 0 {
		return 0
	}
	return math.Sqrt(sumSq / float64(count))
}
