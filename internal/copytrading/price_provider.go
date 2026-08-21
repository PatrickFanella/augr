package copytrading

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type OHLCVSource interface {
	GetOHLCV(ctx context.Context, marketType domain.MarketType, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error)
}

type OHLCVPriceProvider struct {
	Source OHLCVSource
	Now    func() time.Time
}

func (p OHLCVPriceProvider) Snapshots(ctx context.Context, tickers []string, asOf time.Time) (map[string]PriceSnapshot, error) {
	if p.Source == nil {
		return nil, fmt.Errorf("market data source is unavailable")
	}
	now := asOf.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if p.Now != nil {
		now = p.Now().UTC()
	}
	out := make(map[string]PriceSnapshot, len(tickers))
	seen := map[string]bool{}
	for _, ticker := range tickers {
		ticker = strings.ToUpper(strings.TrimSpace(ticker))
		if ticker == "" || seen[ticker] {
			continue
		}
		seen[ticker] = true
		bars, err := p.Source.GetOHLCV(ctx, domain.MarketTypeStock, ticker, data.Timeframe1d, now.AddDate(0, 0, -45), now)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", ticker, err)
		}
		if len(bars) == 0 {
			continue
		}
		start := 0
		if len(bars) > 20 {
			start = len(bars) - 20
		}
		totalDollarVolume := 0.0
		for _, bar := range bars[start:] {
			totalDollarVolume += bar.Close * bar.Volume
		}
		out[ticker] = PriceSnapshot{Ticker: ticker, AvgDollarVolume: totalDollarVolume / float64(len(bars)-start)}
	}
	return out, nil
}
