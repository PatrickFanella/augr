package backtest

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

var (
	// ErrNilClock is returned when a nil clock is passed to
	// NewHistoricalDataProvider.
	ErrNilClock = errors.New("backtest: clock is required")

	// ErrEmptyTicker is returned when a blank ticker is passed to a
	// HistoricalDataProvider query method.
	ErrEmptyTicker = errors.New("backtest: ticker must not be empty")

	// ErrInvalidRange is returned when from is after to.
	ErrInvalidRange = errors.New("backtest: from must not be after to")
)

// HistoricalDataProvider implements data.DataProvider using pre-loaded
// historical data and a simulated clock. Only data with timestamps at or
// before the clock's current time is returned, preventing look-ahead bias.
type HistoricalDataProvider struct {
	bars         []domain.OHLCV       // sorted ascending by Timestamp
	news         []data.NewsArticle   // sorted ascending by PublishedAt
	fundamentals *data.Fundamentals   // static; always returned when non-nil
	social       []data.SocialSentiment // sorted ascending by MeasuredAt
	clock        Clock
}

// HistoricalDataOption configures a HistoricalDataProvider during construction.
type HistoricalDataOption func(*HistoricalDataProvider)

// WithBars supplies OHLCV bars to the provider. The bars are sorted by
// timestamp internally; callers do not need to pre-sort.
func WithBars(bars []domain.OHLCV) HistoricalDataOption {
	return func(p *HistoricalDataProvider) {
		sorted := make([]domain.OHLCV, len(bars))
		copy(sorted, bars)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].Timestamp.Before(sorted[j].Timestamp)
		})
		p.bars = sorted
	}
}

// WithNews supplies news articles to the provider. Articles are sorted by
// PublishedAt internally.
func WithNews(news []data.NewsArticle) HistoricalDataOption {
	return func(p *HistoricalDataProvider) {
		sorted := make([]data.NewsArticle, len(news))
		copy(sorted, news)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].PublishedAt.Before(sorted[j].PublishedAt)
		})
		p.news = sorted
	}
}

// WithFundamentals supplies static fundamental data to the provider.
func WithFundamentals(f *data.Fundamentals) HistoricalDataOption {
	return func(p *HistoricalDataProvider) {
		p.fundamentals = f
	}
}

// WithSocialSentiment supplies social-sentiment snapshots to the provider.
// Snapshots are sorted by MeasuredAt internally.
func WithSocialSentiment(social []data.SocialSentiment) HistoricalDataOption {
	return func(p *HistoricalDataProvider) {
		sorted := make([]data.SocialSentiment, len(social))
		copy(sorted, social)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].MeasuredAt.Before(sorted[j].MeasuredAt)
		})
		p.social = sorted
	}
}

// NewHistoricalDataProvider creates a provider backed by historical data. The
// clock determines the upper bound for accessible data, ensuring no future data
// is leaked. Use With* options to supply bars, news, fundamentals, and social
// sentiment. Returns ErrNilClock if clock is nil.
func NewHistoricalDataProvider(clock Clock, opts ...HistoricalDataOption) (*HistoricalDataProvider, error) {
	if clock == nil {
		return nil, ErrNilClock
	}
	p := &HistoricalDataProvider{clock: clock}
	for _, opt := range opts {
		opt(p)
	}
	return p, nil
}

// Compile-time check: HistoricalDataProvider implements data.DataProvider.
var _ data.DataProvider = (*HistoricalDataProvider)(nil)

// GetOHLCV returns bars within [from, to] whose timestamps do not exceed the
// current simulated time. The timeframe parameter is accepted for interface
// compliance but does not filter results; callers should pre-load bars of the
// desired timeframe.
func (p *HistoricalDataProvider) GetOHLCV(_ context.Context, ticker string, _ data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if err := validateQuery(ticker, from, to); err != nil {
		return nil, err
	}
	now := p.clock.Now()
	// Use sort.Search to find the visible upper bound (<= now) in O(log n).
	visible := sort.Search(len(p.bars), func(i int) bool {
		return p.bars[i].Timestamp.After(now)
	})
	// Binary-search for the [from, to] window within the visible slice.
	start := sort.Search(visible, func(i int) bool {
		return !p.bars[i].Timestamp.Before(from)
	})
	end := sort.Search(visible, func(i int) bool {
		return p.bars[i].Timestamp.After(to)
	})
	if start >= end {
		return nil, nil
	}
	result := make([]domain.OHLCV, end-start)
	copy(result, p.bars[start:end])
	return result, nil
}

// GetFundamentals returns the pre-loaded fundamentals. If none were supplied,
// data.ErrNotImplemented is returned so that a provider chain can fall through.
func (p *HistoricalDataProvider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p.fundamentals == nil {
		return data.Fundamentals{}, data.ErrNotImplemented
	}
	return *p.fundamentals, nil
}

// GetNews returns articles within [from, to] whose PublishedAt does not exceed
// the current simulated time.
func (p *HistoricalDataProvider) GetNews(_ context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
	if err := validateQuery(ticker, from, to); err != nil {
		return nil, err
	}
	now := p.clock.Now()
	visible := sort.Search(len(p.news), func(i int) bool {
		return p.news[i].PublishedAt.After(now)
	})
	start := sort.Search(visible, func(i int) bool {
		return !p.news[i].PublishedAt.Before(from)
	})
	end := sort.Search(visible, func(i int) bool {
		return p.news[i].PublishedAt.After(to)
	})
	if start >= end {
		return nil, nil
	}
	result := make([]data.NewsArticle, end-start)
	copy(result, p.news[start:end])
	return result, nil
}

// GetSocialSentiment returns sentiment snapshots within [from, to] whose
// MeasuredAt does not exceed the current simulated time.
func (p *HistoricalDataProvider) GetSocialSentiment(_ context.Context, ticker string, from, to time.Time) ([]data.SocialSentiment, error) {
	if err := validateQuery(ticker, from, to); err != nil {
		return nil, err
	}
	now := p.clock.Now()
	visible := sort.Search(len(p.social), func(i int) bool {
		return p.social[i].MeasuredAt.After(now)
	})
	start := sort.Search(visible, func(i int) bool {
		return !p.social[i].MeasuredAt.Before(from)
	})
	end := sort.Search(visible, func(i int) bool {
		return p.social[i].MeasuredAt.After(to)
	})
	if start >= end {
		return nil, nil
	}
	result := make([]data.SocialSentiment, end-start)
	copy(result, p.social[start:end])
	return result, nil
}

// validateQuery checks that the ticker is non-empty and from <= to.
func validateQuery(ticker string, from, to time.Time) error {
	if ticker == "" {
		return ErrEmptyTicker
	}
	if from.After(to) {
		return fmt.Errorf("%w: from=%s to=%s", ErrInvalidRange, from, to)
	}
	return nil
}
