package discovery

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// ScreenerConfig controls which tickers to evaluate and the minimum
// thresholds a candidate must meet to pass screening.
type ScreenerConfig struct {
	Tickers      []string
	MinADV       float64 // min average daily volume (default 100000)
	MinATR       float64 // min ATR value (default 0.5)
	MarketType   domain.MarketType
	LookbackDays int // bars to fetch (default 60)
}

// ScreenResult holds the data and computed stats for a single ticker
// that passed the screening filters.
type ScreenResult struct {
	Ticker     string
	Bars       []domain.OHLCV
	Indicators []domain.Indicator
	Close      float64
	ADV        float64 // average daily volume over lookback
	ATR        float64
}

const (
	defaultScreenerMinADV = 100_000
	defaultScreenerMinATR = 0.5
)

func normalizeScreenerConfig(cfg ScreenerConfig) ScreenerConfig {
	if cfg.MinADV <= 0 {
		cfg.MinADV = defaultScreenerMinADV
	}
	if cfg.MinATR <= 0 {
		cfg.MinATR = defaultScreenerMinATR
	}
	return cfg
}

// Screen fetches data for all tickers concurrently (bounded at 10 goroutines),
// computes indicators, and filters by MinADV and MinATR.
func Screen(ctx context.Context, dataService *data.DataService, cfg ScreenerConfig, logger *slog.Logger) ([]ScreenResult, error) {
	if logger == nil {
		logger = slog.Default()
	}
	cfg = normalizeScreenerConfig(cfg)
	lookback := cfg.LookbackDays
	if lookback == 0 {
		lookback = 60
	}

	now := time.Now().UTC()
	// Extra calendar days for indicator warmup.
	from := now.AddDate(0, 0, -lookback*2)
	to := now

	type result struct {
		res ScreenResult
		ok  bool
		err error
	}

	sem := make(chan struct{}, 10)
	results := make([]result, len(cfg.Tickers))
	var wg sync.WaitGroup

	for i, ticker := range cfg.Tickers {
		wg.Add(1)
		go func(idx int, t string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			bars, err := dataService.GetOHLCV(ctx, cfg.MarketType, t, data.Timeframe1d, from, to)
			if err != nil {
				logger.Warn("screener: failed to fetch OHLCV",
					slog.String("ticker", t),
					slog.Any("error", err),
				)
				results[idx] = result{err: fmt.Errorf("screener: %s: %w", t, err)}
				return
			}
			if len(bars) == 0 {
				results[idx] = result{}
				return
			}

			indicators := data.IndicatorSnapshotFromBars(bars)

			// Use the tail of bars (up to lookback) for ADV.
			tail := bars
			if len(tail) > lookback {
				tail = tail[len(tail)-lookback:]
			}
			var volSum float64
			for _, b := range tail {
				volSum += b.Volume
			}
			adv := volSum / float64(len(tail))

			// Find ATR from computed indicators.
			var atr float64
			for _, ind := range indicators {
				if ind.Name == "atr_14" {
					atr = ind.Value
					break
				}
			}

			if adv < cfg.MinADV || atr < cfg.MinATR {
				logger.Debug("screener: filtered out",
					slog.String("ticker", t),
					slog.Float64("adv", adv),
					slog.Float64("atr", atr),
				)
				results[idx] = result{}
				return
			}

			results[idx] = result{
				res: ScreenResult{
					Ticker:     t,
					Bars:       bars,
					Indicators: indicators,
					Close:      bars[len(bars)-1].Close,
					ADV:        adv,
					ATR:        atr,
				},
				ok: true,
			}
		}(i, ticker)
	}
	wg.Wait()

	var out []ScreenResult
	var firstErr error
	for _, r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
		if r.ok {
			out = append(out, r.res)
		}
	}

	// Return results even when some tickers failed; only return an error if
	// every ticker errored out.
	if len(out) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return out, nil
}
