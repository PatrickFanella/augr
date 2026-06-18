package kalshi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
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

	yesBid, err := raw.quoteProbability(raw.YesBidDollars, raw.YesBid)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: yes_bid: %w", err)
	}
	yesAsk, err := raw.quoteProbability(raw.YesAskDollars, raw.YesAsk)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: yes_ask: %w", err)
	}
	noBid, err := raw.quoteProbability(raw.NoBidDollars, raw.NoBid)
	if err != nil {
		return executionkalshi.Snapshot{}, fmt.Errorf("kalshi: no_bid: %w", err)
	}
	noAsk, err := raw.quoteProbability(raw.NoAskDollars, raw.NoAsk)
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
		Volume:       raw.volume(),
		OpenInterest: raw.openInterest(),
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
	Ticker         string  `json:"ticker"`
	Title          string  `json:"title"`
	Status         string  `json:"status"`
	YesBid         float64 `json:"yes_bid"`
	YesBidDollars  string  `json:"yes_bid_dollars"`
	YesAsk         float64 `json:"yes_ask"`
	YesAskDollars  string  `json:"yes_ask_dollars"`
	NoBid          float64 `json:"no_bid"`
	NoBidDollars   string  `json:"no_bid_dollars"`
	NoAsk          float64 `json:"no_ask"`
	NoAskDollars   string  `json:"no_ask_dollars"`
	Volume         float64 `json:"volume"`
	VolumeFP       string  `json:"volume_fp"`
	OpenInterest   float64 `json:"open_interest"`
	OpenInterestFP string  `json:"open_interest_fp"`
	CloseTime      string  `json:"close_time"`
	yesBidSet      bool
	yesAskSet      bool
	noBidSet       bool
	noAskSet       bool
}

func (m *marketSnapshot) UnmarshalJSON(data []byte) error {
	type alias marketSnapshot
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return err
	}
	decoded.yesBidSet = jsonFieldPresent(fields, "yes_bid")
	decoded.yesAskSet = jsonFieldPresent(fields, "yes_ask")
	decoded.noBidSet = jsonFieldPresent(fields, "no_bid")
	decoded.noAskSet = jsonFieldPresent(fields, "no_ask")
	*m = marketSnapshot(decoded)
	return nil
}

func (m marketSnapshot) hasFullBook() bool {
	return (m.yesBidSet || strings.TrimSpace(m.YesBidDollars) != "") &&
		(m.yesAskSet || strings.TrimSpace(m.YesAskDollars) != "") &&
		(m.noBidSet || strings.TrimSpace(m.NoBidDollars) != "") &&
		(m.noAskSet || strings.TrimSpace(m.NoAskDollars) != "")
}

func (m *marketSnapshot) mergeBook(book marketSnapshot) {
	if !m.yesBidSet && strings.TrimSpace(m.YesBidDollars) == "" {
		m.YesBid = book.YesBid
		m.YesBidDollars = book.YesBidDollars
		m.yesBidSet = book.yesBidSet
	}
	if !m.yesAskSet && strings.TrimSpace(m.YesAskDollars) == "" {
		m.YesAsk = book.YesAsk
		m.YesAskDollars = book.YesAskDollars
		m.yesAskSet = book.yesAskSet
	}
	if !m.noBidSet && strings.TrimSpace(m.NoBidDollars) == "" {
		m.NoBid = book.NoBid
		m.NoBidDollars = book.NoBidDollars
		m.noBidSet = book.noBidSet
	}
	if !m.noAskSet && strings.TrimSpace(m.NoAskDollars) == "" {
		m.NoAsk = book.NoAsk
		m.NoAskDollars = book.NoAskDollars
		m.noAskSet = book.noAskSet
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
	return strings.TrimSpace(m.Ticker) == "" && strings.TrimSpace(m.Title) == "" && strings.TrimSpace(m.Status) == "" && m.YesBid == 0 && strings.TrimSpace(m.YesBidDollars) == "" && m.YesAsk == 0 && strings.TrimSpace(m.YesAskDollars) == "" && m.NoBid == 0 && strings.TrimSpace(m.NoBidDollars) == "" && m.NoAsk == 0 && strings.TrimSpace(m.NoAskDollars) == "" && m.Volume == 0 && strings.TrimSpace(m.VolumeFP) == "" && m.OpenInterest == 0 && strings.TrimSpace(m.OpenInterestFP) == "" && strings.TrimSpace(m.CloseTime) == ""
}

func jsonFieldPresent(fields map[string]json.RawMessage, key string) bool {
	raw, ok := fields[key]
	return ok && len(strings.TrimSpace(string(raw))) > 0 && strings.TrimSpace(string(raw)) != "null"
}

func (m marketSnapshot) quoteProbability(dollars string, cents float64) (float64, error) {
	if strings.TrimSpace(dollars) != "" {
		value, err := strconv.ParseFloat(strings.TrimSpace(dollars), 64)
		if err != nil {
			return 0, err
		}
		if value < 0 || value > 1 {
			return 0, fmt.Errorf("quote dollars %.4f out of range [0,1]", value)
		}
		return value, nil
	}
	return QuoteCentsToProbability(cents)
}

func (m marketSnapshot) volume() float64 {
	if strings.TrimSpace(m.VolumeFP) != "" {
		if value, err := strconv.ParseFloat(strings.TrimSpace(m.VolumeFP), 64); err == nil {
			return value
		}
	}
	return m.Volume
}

func (m marketSnapshot) openInterest() float64 {
	if strings.TrimSpace(m.OpenInterestFP) != "" {
		if value, err := strconv.ParseFloat(strings.TrimSpace(m.OpenInterestFP), 64); err == nil {
			return value
		}
	}
	return m.OpenInterest
}
