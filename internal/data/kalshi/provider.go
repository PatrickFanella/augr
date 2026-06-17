package kalshi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	executionkalshi "github.com/PatrickFanella/get-rich-quick/internal/execution/kalshi"
)

// Provider implements data.DataProvider for Kalshi prediction markets.
// OHLCV and other non-market-data methods are intentionally unsupported.
type Provider struct {
	client *Client
	logger *slog.Logger
}

var _ data.DataProvider = (*Provider)(nil)

// NewProvider creates a Kalshi provider backed by the public Trade API.
func NewProvider(baseURL string, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	client, err := NewClient(baseURL, "", "", logger)
	if err != nil {
		logger.Warn("kalshi provider falling back to demo base url", slog.Any("error", err))
		client = &Client{baseURL: defaultBaseURL, httpClient: &http.Client{Timeout: defaultTimeout}, now: time.Now, logger: logger}
	}
	return &Provider{client: client, logger: logger}
}

// LoadSnapshot fetches the latest market metadata and order book for a ticker.
func (p *Provider) LoadSnapshot(ctx context.Context, ticker string) (executionkalshi.Snapshot, error) {
	if p == nil || p.client == nil {
		return executionkalshi.Snapshot{}, errors.New("kalshi: provider is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return executionkalshi.Snapshot{}, errors.New("kalshi: ticker is required")
	}

	raw, err := p.fetchMarket(ctx, ticker)
	if err != nil {
		return executionkalshi.Snapshot{}, err
	}

	if !raw.hasFullBook() {
		book, bookErr := p.fetchOrderbook(ctx, ticker)
		if bookErr != nil {
			return executionkalshi.Snapshot{}, bookErr
		}
		raw.mergeBook(book)
	}

	closeTime, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw.CloseTime))
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: parse close_time %q: %w", raw.CloseTime, err)
	}

	fetchedAt := time.Now().UTC()
	if p.client != nil && p.client.now != nil {
		fetchedAt = p.client.now().UTC()
	}

	yesBid, err := toProbability(raw.YesBid)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: yes_bid: %w", err)
	}
	yesAsk, err := toProbability(raw.YesAsk)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: yes_ask: %w", err)
	}
	noBid, err := toProbability(raw.NoBid)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: no_bid: %w", err)
	}
	noAsk, err := toProbability(raw.NoAsk)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: no_ask: %w", err)
	}

	return executionkalshi.Snapshot{
		Ticker:       raw.Ticker,
		Title:        raw.Title,
		Status:       raw.Status,
		BestBidYes:   yesBid,
		BestAskYes:   yesAsk,
		BestBidNo:    noBid,
		BestAskNo:    noAsk,
		Volume:       raw.Volume,
		OpenInterest: raw.OpenInterest,
		CloseTime:    closeTime.UTC(),
		FetchedAt:    fetchedAt,
	}, nil
}

// GetOHLCV is intentionally unsupported for Kalshi.
func (p *Provider) GetOHLCV(_ context.Context, _ string, _ data.Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	return nil, fmt.Errorf("kalshi: GetOHLCV: %w", data.ErrNotImplemented)
}

// GetFundamentals is intentionally unsupported for Kalshi.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	return data.Fundamentals{}, fmt.Errorf("kalshi: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews is intentionally unsupported for Kalshi.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	return nil, fmt.Errorf("kalshi: GetNews: %w", data.ErrNotImplemented)
}

// GetSocialSentiment is intentionally unsupported for Kalshi.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]data.SocialSentiment, error) {
	return nil, fmt.Errorf("kalshi: GetSocialSentiment: %w", data.ErrNotImplemented)
}

func (p *Provider) fetchMarket(ctx context.Context, ticker string) (marketSnapshot, error) {
	body, err := p.client.Get(ctx, fmt.Sprintf("/markets/%s", url.PathEscape(ticker)), nil, false)
	if err != nil {
		return marketSnapshot{}, err
	}
	return decodeMarketSnapshot(body)
}

func (p *Provider) fetchOrderbook(ctx context.Context, ticker string) (marketSnapshot, error) {
	body, err := p.client.Get(ctx, fmt.Sprintf("/markets/%s/orderbook", url.PathEscape(ticker)), nil, false)
	if err != nil {
		return marketSnapshot{}, err
	}
	return decodeMarketSnapshot(body)
}

type marketEnvelope struct {
	Market marketSnapshot `json:"market"`
}

type marketSnapshot struct {
	Ticker       string  `json:"ticker"`
	Title        string  `json:"title"`
	Status       string  `json:"status"`
	YesBid       float64 `json:"yes_bid"`
	YesAsk       float64 `json:"yes_ask"`
	NoBid        float64 `json:"no_bid"`
	NoAsk        float64 `json:"no_ask"`
	Volume       float64 `json:"volume"`
	OpenInterest float64 `json:"open_interest"`
	CloseTime    string  `json:"close_time"`
}

func (m marketSnapshot) hasFullBook() bool {
	return m.YesBid > 0 && m.YesAsk > 0 && m.NoBid > 0 && m.NoAsk > 0
}

func (m *marketSnapshot) mergeBook(book marketSnapshot) {
	if m.YesBid <= 0 {
		m.YesBid = book.YesBid
	}
	if m.YesAsk <= 0 {
		m.YesAsk = book.YesAsk
	}
	if m.NoBid <= 0 {
		m.NoBid = book.NoBid
	}
	if m.NoAsk <= 0 {
		m.NoAsk = book.NoAsk
	}
	if m.Volume <= 0 {
		m.Volume = book.Volume
	}
	if m.OpenInterest <= 0 {
		m.OpenInterest = book.OpenInterest
	}
	if strings.TrimSpace(m.CloseTime) == "" {
		m.CloseTime = book.CloseTime
	}
}

func decodeMarketSnapshot(body []byte) (marketSnapshot, error) {
	var wrapped marketEnvelope
	if err := json.Unmarshal(body, &wrapped); err == nil && !wrapped.Market.isZero() {
		return wrapped.Market, nil
	}

	var direct marketSnapshot
	if err := json.Unmarshal(body, &direct); err == nil && !direct.isZero() {
		return direct, nil
	}

	return marketSnapshot{}, fmt.Errorf("kalshi: decode market snapshot: %s", strings.TrimSpace(string(body)))
}

func (m marketSnapshot) isZero() bool {
	return strings.TrimSpace(m.Ticker) == "" && strings.TrimSpace(m.Title) == "" && strings.TrimSpace(m.Status) == "" && m.YesBid == 0 && m.YesAsk == 0 && m.NoBid == 0 && m.NoAsk == 0 && m.Volume == 0 && m.OpenInterest == 0 && strings.TrimSpace(m.CloseTime) == ""
}

func toProbability(v float64) (float64, error) {
	if v < 0 || v > 100 {
		return 0, fmt.Errorf("quote cents %.4f out of range [0,100]", v)
	}
	return v / 100, nil
}
