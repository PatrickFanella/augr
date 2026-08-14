package stocktwits

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// DataProvider wraps the StockTwits Client as a data.DataProvider.
// Only GetSocialSentiment is supported.
type DataProvider struct {
	client *Client
	logger *slog.Logger
}

// Compile-time check that DataProvider satisfies data.DataProvider.
var _ data.DataProvider = (*DataProvider)(nil)

// NewDataProvider constructs a StockTwits data provider.
func NewDataProvider(logger *slog.Logger) *DataProvider {
	return &DataProvider{
		client: NewClient(logger),
		logger: logger,
	}
}

// GetOHLCV is not supported by StockTwits.
func (p *DataProvider) GetOHLCV(_ context.Context, _ string, _ data.Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	return nil, fmt.Errorf("stocktwits: GetOHLCV: %w", data.ErrNotImplemented)
}

// GetFundamentals is not supported by StockTwits.
func (p *DataProvider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	return data.Fundamentals{}, fmt.Errorf("stocktwits: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews is not supported by StockTwits.
func (p *DataProvider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	return nil, fmt.Errorf("stocktwits: GetNews: %w", data.ErrNotImplemented)
}

// GetSocialSentiment returns sentiment for a ticker from StockTwits message
// stream. The from/to parameters are accepted but ignored since StockTwits
// returns current sentiment only.
func (p *DataProvider) GetSocialSentiment(ctx context.Context, ticker string, _, _ time.Time) ([]data.SocialSentiment, error) {
	if p == nil {
		return nil, fmt.Errorf("stocktwits: provider is nil")
	}

	sentiment, err := p.client.GetSymbolSentiment(ctx, ticker)
	if err != nil {
		return nil, fmt.Errorf("stocktwits: GetSocialSentiment %s: %w", ticker, err)
	}

	if sentiment == nil || sentiment.Total == 0 {
		return nil, nil
	}

	var score, bullRatio, bearRatio float64
	if sentiment.Total > 0 {
		bullRatio = float64(sentiment.Bullish) / float64(sentiment.Total)
		bearRatio = float64(sentiment.Bearish) / float64(sentiment.Total)
		score = bullRatio - bearRatio
	}

	return []data.SocialSentiment{{
		Ticker:     ticker,
		Source:     "stocktwits",
		Score:      score,
		Bullish:    bullRatio,
		Bearish:    bearRatio,
		PostCount:  sentiment.Total,
		MeasuredAt: sentiment.MeasuredAt,
	}}, nil
}
