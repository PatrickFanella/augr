package universe

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"

	"github.com/PatrickFanella/get-rich-quick/internal/data/polygon"
)

// PreMarketConfig holds scoring parameters for the pre-market screener.
type PreMarketConfig struct {
	MinADV           float64 // default 500000
	MinPrice         float64 // default 5.0
	MaxPrice         float64 // default 500.0
	TopN             int     // default 30
	VolumeWeight     float64 // default 0.4
	MomentumWeight   float64 // default 0.3
	VolatilityWeight float64 // default 0.3
}

// ScoredTicker is the result of the pre-market screener for a single ticker.
type ScoredTicker struct {
	Ticker    string   `json:"ticker"`
	Score     float64  `json:"score"`
	Reasons   []string `json:"reasons"`
	DayVolume float64  `json:"day_volume"`
	DayClose  float64  `json:"day_close"`
	ChangePct float64  `json:"change_pct"`
	GapPct    float64  `json:"gap_pct"`
}

// DefaultPreMarketConfig returns sensible defaults for the screener.
func DefaultPreMarketConfig() PreMarketConfig {
	return PreMarketConfig{
		MinADV:           500_000,
		MinPrice:         5.0,
		MaxPrice:         500.0,
		TopN:             30,
		VolumeWeight:     0.4,
		MomentumWeight:   0.3,
		VolatilityWeight: 0.3,
	}
}

// RunPreMarketScreen fetches bulk snapshot from Polygon, scores each ticker,
// updates watch_score in universe, and returns the top N.
func RunPreMarketScreen(
	ctx context.Context,
	polygonClient *polygon.Client,
	repo UniverseRepository,
	cfg PreMarketConfig,
	logger *slog.Logger,
) ([]ScoredTicker, error) {
	if logger == nil {
		logger = slog.Default()
	}

	// 1. Get all active tickers from repo.
	active := true
	tickers, err := repo.List(ctx, ListFilter{Active: &active}, 5000, 0)
	if err != nil {
		return nil, fmt.Errorf("screener: list active tickers: %w", err)
	}

	if len(tickers) == 0 {
		logger.Warn("screener: no active tickers in universe")
		return nil, nil
	}

	// 2. Build a set of known tickers for filtering.
	knownTickers := make(map[string]bool, len(tickers))
	for _, t := range tickers {
		knownTickers[t.Ticker] = true
	}

	// 3. Call BulkSnapshot with empty list to get ALL tickers in one call
	// (avoids URL length limit when universe has thousands of tickers).
	snapshots, err := polygonClient.BulkSnapshot(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("screener: bulk snapshot: %w", err)
	}

	logger.Info("screener: received snapshots", slog.Int("count", len(snapshots)))

	// 4. Score each snapshot (only tickers in our universe).
	scored := make([]ScoredTicker, 0, len(snapshots))
	for _, snap := range snapshots {
		if !knownTickers[snap.Ticker] {
			continue
		}
		dayVolume := snap.Day.Volume
		dayClose := snap.Day.Close
		dayOpen := snap.Day.Open
		prevClose := snap.PrevDay.Close
		prevVolume := snap.PrevDay.Volume
		changePct := snap.TodaysChangePct

		// Skip if below ADV, price out of range, or missing data.
		if dayVolume < cfg.MinADV {
			continue
		}
		if dayClose < cfg.MinPrice || dayClose > cfg.MaxPrice {
			continue
		}

		// GapPct: (DayOpen - PrevClose) / PrevClose * 100
		var gapPct float64
		if prevClose > 0 {
			gapPct = (dayOpen - prevClose) / prevClose * 100
		}

		// VolumeRatio: DayVolume / PrevVolume (handle zero)
		var volumeRatio float64
		if prevVolume > 0 {
			volumeRatio = dayVolume / prevVolume
		}

		// Score components.
		volScore := math.Min(volumeRatio/3, 1)
		momScore := math.Min(math.Abs(gapPct)/5, 1)
		volatScore := math.Min(math.Abs(changePct)/3, 1)

		score := cfg.VolumeWeight*volScore + cfg.MomentumWeight*momScore + cfg.VolatilityWeight*volatScore

		// Build reasons.
		var reasons []string
		if volumeRatio >= 1.5 {
			reasons = append(reasons, fmt.Sprintf("Volume surge %.1fx", volumeRatio))
		}
		if gapPct > 0.5 {
			reasons = append(reasons, fmt.Sprintf("Gap up %.1f%%", gapPct))
		} else if gapPct < -0.5 {
			reasons = append(reasons, fmt.Sprintf("Gap down %.1f%%", gapPct))
		}
		if changePct > 1.0 {
			reasons = append(reasons, fmt.Sprintf("Up %.1f%% today", changePct))
		} else if changePct < -1.0 {
			reasons = append(reasons, fmt.Sprintf("Down %.1f%% today", changePct))
		}

		scored = append(scored, ScoredTicker{
			Ticker:    snap.Ticker,
			Score:     score,
			Reasons:   reasons,
			DayVolume: dayVolume,
			DayClose:  dayClose,
			ChangePct: changePct,
			GapPct:    gapPct,
		})
	}

	// 5. Sort by score descending.
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})

	// 6. Update repo watch_score for each scored ticker.
	for _, s := range scored {
		if err := repo.UpdateScore(ctx, s.Ticker, s.Score); err != nil {
			logger.Warn("screener: failed to update score",
				slog.String("ticker", s.Ticker),
				slog.Any("error", err),
			)
		}
	}

	// 7. Return top N.
	if cfg.TopN > 0 && len(scored) > cfg.TopN {
		scored = scored[:cfg.TopN]
	}

	logger.Info("screener: complete", slog.Int("scored", len(scored)))
	return scored, nil
}
