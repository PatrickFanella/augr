package options

import (
	"math"
	"sort"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsScoringConfig controls the scoring weights.
type OptionsScoringConfig struct {
	IVRankWeight        float64 // default 0.30
	UnusualVolumeWeight float64 // default 0.25
	PutCallSkewWeight   float64 // default 0.20
	LiquidityWeight     float64 // default 0.15
	EarningsWeight      float64 // default 0.10
	MinScore            float64 // filter threshold (default 0.2)
}

// DefaultOptionsScoringConfig returns sensible defaults.
func DefaultOptionsScoringConfig() OptionsScoringConfig {
	return OptionsScoringConfig{
		IVRankWeight:        0.30,
		UnusualVolumeWeight: 0.25,
		PutCallSkewWeight:   0.20,
		LiquidityWeight:     0.15,
		EarningsWeight:      0.10,
		MinScore:            0.2,
	}
}

// OptionsScoredCandidate is a screened ticker with options-specific scores.
type OptionsScoredCandidate struct {
	OptionsScreenResult // embed
	IVRank              float64
	IVPercentile        float64
	ATMIV               float64
	PutCallRatio        float64
	VolumeRatio         float64
	Score               float64
}

// ScoreOptionsCandidates scores screened candidates by options-specific metrics.
// It computes IV rank from realized volatility (v1 approach) and chain metrics.
func ScoreOptionsCandidates(candidates []OptionsScreenResult, cfg OptionsScoringConfig) []OptionsScoredCandidate {
	if len(candidates) == 0 {
		return nil
	}

	var scored []OptionsScoredCandidate

	for _, c := range candidates {
		// Compute realized volatility for IV rank approximation.
		rv252 := realizedVol(c.Bars, 252)

		// Compute chain metrics.
		atmIV, putCallRatio := chainMetrics(c.Chain, c.Close)

		// IV rank: where is current ATM IV relative to realized vol range?
		var ivRank float64
		if rv252 > 0 && atmIV > 0 {
			minVol := rv252 * 0.7 // approximate historical IV low
			maxVol := rv252 * 1.5 // approximate historical IV high
			if maxVol > minVol {
				ivRank = clamp((atmIV-minVol)/(maxVol-minVol)*100, 0, 100)
			}
		}

		// Volume ratio: today's options volume vs average.
		var totalVol, totalOI float64
		for _, snap := range c.Chain {
			totalVol += snap.Volume
			totalOI += snap.OpenInterest
		}
		volRatio := 1.0
		if totalOI > 0 {
			volRatio = totalVol / (totalOI * 0.05) // 5% of OI as "normal" daily volume
		}

		// Score components.
		ivScore := normalize(ivRank, 0, 100)
		volScore := normalize(volRatio, 1, 5)
		skewScore := skewScoreFn(putCallRatio)
		liqScore := liquidityScore(c.ATMOI, c.ChainDepth)

		score := cfg.IVRankWeight*ivScore +
			cfg.UnusualVolumeWeight*volScore +
			cfg.PutCallSkewWeight*skewScore +
			cfg.LiquidityWeight*liqScore +
			cfg.EarningsWeight*0.5 // default mid-score for earnings (v1: no calendar lookup)

		if score < cfg.MinScore {
			continue
		}

		scored = append(scored, OptionsScoredCandidate{
			OptionsScreenResult: c,
			IVRank:              ivRank,
			IVPercentile:        ivRank, // v1: same as rank
			ATMIV:               atmIV,
			PutCallRatio:        putCallRatio,
			VolumeRatio:         volRatio,
			Score:               score,
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	return scored
}

// realizedVol computes annualised realized volatility from daily returns.
func realizedVol(bars []domain.OHLCV, lookback int) float64 {
	if len(bars) < 2 {
		return 0
	}
	n := min(lookback, len(bars)-1)
	start := len(bars) - n - 1

	var sum, sumSq float64
	for i := start + 1; i < len(bars); i++ {
		if bars[i-1].Close <= 0 {
			continue
		}
		ret := math.Log(bars[i].Close / bars[i-1].Close)
		sum += ret
		sumSq += ret * ret
	}

	count := float64(n)
	if count < 2 {
		return 0
	}
	mean := sum / count
	variance := sumSq/count - mean*mean
	if variance <= 0 {
		return 0
	}
	return math.Sqrt(variance * 252) // annualise
}

// chainMetrics extracts ATM IV and put/call ratio from a chain.
func chainMetrics(chain []domain.OptionSnapshot, close float64) (atmIV, putCallRatio float64) {
	var putVol, callVol float64
	bestDist := math.Inf(1)

	for _, snap := range chain {
		switch snap.Contract.OptionType {
		case domain.OptionTypePut:
			putVol += snap.Volume
		case domain.OptionTypeCall:
			callVol += snap.Volume
			// ATM IV from call nearest to close.
			dist := math.Abs(snap.Contract.Strike - close)
			if dist < bestDist {
				bestDist = dist
				atmIV = snap.Greeks.IV
			}
		}
	}

	if callVol > 0 {
		putCallRatio = putVol / callVol
	}
	return atmIV, putCallRatio
}

func normalize(v, lo, hi float64) float64 {
	if hi <= lo {
		return 0
	}
	return clamp((v-lo)/(hi-lo), 0, 1)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// skewScoreFn rewards put/call ratios that indicate directional conviction.
// Extreme values (> 2 or < 0.3) score highest.
func skewScoreFn(ratio float64) float64 {
	if ratio <= 0 {
		return 0
	}
	// Distance from neutral (1.0), normalised to [0, 1].
	dist := math.Abs(ratio - 1.0)
	return clamp(dist/1.5, 0, 1)
}

// liquidityScore rewards deep, liquid chains.
func liquidityScore(atmOI float64, chainDepth int) float64 {
	oiScore := normalize(atmOI, 100, 10000)
	depthScore := normalize(float64(chainDepth), 10, 200)
	return 0.6*oiScore + 0.4*depthScore
}
