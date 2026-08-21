package copytrading

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

// CanonicalQuoteProvider combines nonauthoritative OHLCV liquidity context
// with exact OVR-201 point-in-time quote evidence. Only the latter can make a
// copy intent executable.
type CanonicalQuoteProvider struct {
	Instruments          repository.InstrumentRepository
	Quotes               repository.QuoteSnapshotRepository
	Liquidity            PriceProvider
	AliasProvider        string
	QuoteProvider        string
	Venue                string
	ObservationNamespace string
}

func (p CanonicalQuoteProvider) Snapshots(ctx context.Context, tickers []string, asOf time.Time) (map[string]PriceSnapshot, error) {
	if p.Instruments == nil || p.Quotes == nil || strings.TrimSpace(p.AliasProvider) == "" || strings.TrimSpace(p.QuoteProvider) == "" || strings.TrimSpace(p.Venue) == "" || strings.TrimSpace(p.ObservationNamespace) == "" || asOf.IsZero() {
		return nil, fmt.Errorf("copy canonical quote provider is not configured")
	}
	values := make(map[string]PriceSnapshot, len(tickers))
	if p.Liquidity != nil {
		liquidity, err := p.Liquidity.Snapshots(ctx, tickers, asOf)
		if err != nil {
			return nil, err
		}
		for key, value := range liquidity {
			values[key] = value
		}
	}
	for _, raw := range tickers {
		ticker := strings.ToUpper(strings.TrimSpace(raw))
		if ticker == "" {
			continue
		}
		value := values[ticker]
		value.Ticker = ticker
		reference, err := p.Instruments.ResolveAlias(ctx, p.AliasProvider, instrument.AliasTicker, ticker, asOf)
		if errors.Is(err, repository.ErrNotFound) {
			values[ticker] = value
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("resolve copy instrument %s: %w", ticker, err)
		}
		snapshot, err := p.Quotes.LatestQuoteSnapshotAt(ctx, marketdata.QuoteSelector{InstrumentID: reference.ID, Provider: p.QuoteProvider, Venue: p.Venue, ObservationNamespace: p.ObservationNamespace, AsOf: asOf})
		if errors.Is(err, repository.ErrNotFound) {
			values[ticker] = value
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("select copy quote %s: %w", ticker, err)
		}
		value.QuoteSnapshotID = snapshot.ID
		if snapshot.Bid != nil {
			value.Bid = snapshot.Bid.String()
		}
		if snapshot.Ask != nil {
			value.Ask = snapshot.Ask.String()
		}
		if snapshot.AvailableAt != nil {
			available := snapshot.AvailableAt.UTC().Truncate(time.Microsecond)
			value.AvailableAt = &available
		}
		value.MarketStatus = snapshot.MarketStatus
		value.SessionStatus = snapshot.SessionStatus
		values[ticker] = value
	}
	return values, nil
}
