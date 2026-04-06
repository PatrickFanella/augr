package options

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsScreenerConfig controls which tickers pass the options screen.
type OptionsScreenerConfig struct {
	Tickers       []string
	MinPrice      float64 // default 5.0
	MinADV        float64 // default 500_000
	MinChainWidth int     // minimum contracts in chain (default 10)
	MinOI         float64 // minimum ATM open interest (default 100)
	TargetDTE     int     // DTE centre for chain check (default 30)
}

func (c *OptionsScreenerConfig) defaults() {
	if c.MinPrice <= 0 {
		c.MinPrice = 5.0
	}
	if c.MinADV <= 0 {
		c.MinADV = 500_000
	}
	if c.MinChainWidth <= 0 {
		c.MinChainWidth = 10
	}
	if c.MinOI <= 0 {
		c.MinOI = 100
	}
	if c.TargetDTE <= 0 {
		c.TargetDTE = 30
	}
}

// OptionsScreenResult is a ticker that passed the options screen.
type OptionsScreenResult struct {
	Ticker     string
	Bars       []domain.OHLCV
	Indicators []domain.Indicator
	Close      float64
	ADV        float64
	ChainDepth int
	ATMOI      float64
	Chain      []domain.OptionSnapshot // nearest expiry chain
}

// ScreenOptions filters tickers for optionability: price, volume, and chain existence.
func ScreenOptions(
	ctx context.Context,
	dataService *data.DataService,
	optionsProvider data.OptionsDataProvider,
	cfg OptionsScreenerConfig,
	logger *slog.Logger,
) ([]OptionsScreenResult, error) {
	cfg.defaults()

	if len(cfg.Tickers) == 0 {
		return nil, nil
	}

	now := time.Now()
	from := now.AddDate(0, -3, 0) // 3 months for ADV + indicators
	targetExpiry := now.AddDate(0, 0, cfg.TargetDTE)

	type result struct {
		res OptionsScreenResult
		ok  bool
	}

	var (
		mu      sync.Mutex
		results []OptionsScreenResult
		wg      sync.WaitGroup
	)

	sem := make(chan struct{}, 10) // concurrency limit

	for _, ticker := range cfg.Tickers {
		wg.Add(1)
		go func(ticker string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			if ctx.Err() != nil {
				return
			}

			// Fetch OHLCV.
			bars, err := dataService.GetOHLCV(ctx, domain.MarketTypeStock, ticker, data.Timeframe1d, from, now)
			if err != nil || len(bars) < 20 {
				return
			}

			// Compute ADV and close price.
			close_ := bars[len(bars)-1].Close
			if close_ < cfg.MinPrice {
				return
			}

			var volSum float64
			lookback := min(20, len(bars))
			for _, b := range bars[len(bars)-lookback:] {
				volSum += b.Volume
			}
			adv := volSum / float64(lookback) * close_
			if adv < cfg.MinADV {
				return
			}

			// Check options chain exists.
			chain, err := optionsProvider.GetOptionsChain(ctx, ticker, targetExpiry, "")
			if err != nil || len(chain) < cfg.MinChainWidth {
				return
			}

			// Find ATM OI.
			var atmOI float64
			bestDist := math.Inf(1)
			for _, snap := range chain {
				if snap.Contract.OptionType != domain.OptionTypeCall {
					continue
				}
				dist := math.Abs(snap.Contract.Strike - close_)
				if dist < bestDist {
					bestDist = dist
					atmOI = snap.OpenInterest
				}
			}
			if atmOI < cfg.MinOI {
				return
			}

			indicators := data.IndicatorSnapshotFromBars(bars)

			mu.Lock()
			results = append(results, OptionsScreenResult{
				Ticker:     ticker,
				Bars:       bars,
				Indicators: indicators,
				Close:      close_,
				ADV:        adv,
				ChainDepth: len(chain),
				ATMOI:      atmOI,
				Chain:      chain,
			})
			mu.Unlock()

			logger.Info("options/screen: passed",
				slog.String("ticker", ticker),
				slog.Float64("close", close_),
				slog.Float64("adv", adv),
				slog.Int("chain_depth", len(chain)),
				slog.Float64("atm_oi", atmOI),
			)
		}(ticker)
	}

	wg.Wait()

	logger.Info("options/screen: complete",
		slog.Int("input", len(cfg.Tickers)),
		slog.Int("passed", len(results)),
	)

	return results, nil
}
