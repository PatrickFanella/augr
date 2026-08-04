package yahoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	defaultBaseURL = "https://query1.finance.yahoo.com"
	defaultTimeout = 30 * time.Second
	defaultUA      = "get-rich-quick/1.0"
)

// Provider retrieves market data from Yahoo Finance's chart API.
type Provider struct {
	baseURL    string
	httpClient *http.Client
	api        *data.APIClient
	logger     *slog.Logger
}

var _ data.DataProvider = (*Provider)(nil)

type timeframeMapping struct {
	interval string
	duration time.Duration
}

// ── Search/News response types ──────────────────────────────────────────

type searchResponse struct {
	News []searchNewsItem `json:"news"`
}

type searchNewsItem struct {
	Title               string   `json:"title"`
	Link                string   `json:"link"`
	Publisher           string   `json:"publisher"`
	ProviderPublishTime int      `json:"providerPublishTime"`
	RelatedTickers      []string `json:"relatedTickers"`
}

// ── Chart response types ────────────────────────────────────────────────

type chartResponse struct {
	Chart chartEnvelope `json:"chart"`
}

type chartEnvelope struct {
	Result []chartResult `json:"result"`
	Error  *chartError   `json:"error"`
}

type chartError struct {
	Code        string `json:"code"`
	Description string `json:"description"`
}

type chartResult struct {
	Timestamp  []int64         `json:"timestamp"`
	Indicators chartIndicators `json:"indicators"`
	Meta       json.RawMessage `json:"meta"`
	Events     json.RawMessage `json:"events"`
}

type chartIndicators struct {
	Quote []chartQuote `json:"quote"`
}

type chartQuote struct {
	Open   []*float64 `json:"open"`
	High   []*float64 `json:"high"`
	Low    []*float64 `json:"low"`
	Close  []*float64 `json:"close"`
	Volume []*float64 `json:"volume"`
}

// NewProvider constructs a Yahoo Finance provider.
// If logger is nil, slog.Default() is used.
func NewProvider(logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}

	httpClient := &http.Client{
		Timeout: defaultTimeout,
	}

	api := data.NewAPIClient(data.APIClientConfig{
		BaseURL: defaultBaseURL,
		Auth:    data.AuthConfig{Style: data.AuthStyleNone},
		Headers: http.Header{
			"Accept":     []string{"application/json"},
			"User-Agent": []string{defaultUA},
		},
		Timeout: defaultTimeout,
		Logger:  logger,
		Prefix:  "yahoo",
	})
	api.SetHTTPClient(httpClient)

	return &Provider{
		baseURL:    defaultBaseURL,
		httpClient: httpClient,
		api:        api,
		logger:     logger,
	}
}

// GetOHLCV returns candlestick data from Yahoo Finance's chart endpoint.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("yahoo: provider is nil")
	}
	if p.httpClient == nil {
		return nil, errors.New("yahoo: http client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("yahoo: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("yahoo: from must be before or equal to to")
	}

	mapping, err := mapTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	// Sync baseURL in case tests changed it directly.
	if p.baseURL != p.api.BaseURL() {
		p.api.SetBaseURL(p.baseURL)
	}
	// Sync httpClient in case tests changed it directly.
	p.api.SetHTTPClient(p.httpClient)

	chartPath := "/v8/finance/chart/" + url.PathEscape(normalizeChartTicker(ticker))
	params := url.Values{
		"interval":       []string{mapping.interval},
		"includePrePost": []string{"false"},
		"period1":        []string{fmt.Sprintf("%d", from.UTC().Unix())},
		"period2":        []string{fmt.Sprintf("%d", to.UTC().Add(mapping.duration).Unix())},
	}

	body, _, err := p.api.Get(ctx, chartPath, params)
	if err != nil {
		var apiErr *data.APIError
		if errors.As(err, &apiErr) {
			message := strings.TrimSpace(string(apiErr.Body))
			if message == "" {
				message = http.StatusText(apiErr.StatusCode)
			}
			return nil, fmt.Errorf("yahoo: request failed with status %d: %s", apiErr.StatusCode, message)
		}
		return nil, err
	}

	var response chartResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("yahoo: decode chart response: %w", err)
	}
	if response.Chart.Error != nil {
		message := strings.TrimSpace(response.Chart.Error.Description)
		if message == "" {
			message = strings.TrimSpace(response.Chart.Error.Code)
		}
		if message == "" {
			message = "chart request failed"
		}

		return nil, fmt.Errorf("yahoo: %s", message)
	}
	if len(response.Chart.Result) == 0 {
		return []domain.OHLCV{}, nil
	}

	quote := firstQuote(response.Chart.Result[0].Indicators.Quote)
	if quote == nil {
		return []domain.OHLCV{}, nil
	}

	bars := make([]domain.OHLCV, 0, len(response.Chart.Result[0].Timestamp))
	for index, timestamp := range response.Chart.Result[0].Timestamp {
		open, high, low, closePrice, ok := quote.bar(index)
		if !ok {
			continue
		}

		barTime := time.Unix(timestamp, 0).UTC()
		if barTime.Before(from.UTC()) || barTime.After(to.UTC()) {
			continue
		}

		volume := quote.volume(index)
		bars = append(bars, domain.OHLCV{
			Timestamp: barTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closePrice,
			Volume:    volume,
		})
	}

	return bars, nil
}

func normalizeChartTicker(ticker string) string {
	if !strings.Contains(ticker, ".") {
		return ticker
	}
	return strings.ReplaceAll(ticker, ".", "-")
}

// GetFundamentals is not supported by the Yahoo provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("yahoo: provider is nil")
	}

	return data.Fundamentals{}, fmt.Errorf("yahoo: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews returns news articles from Yahoo Finance's search endpoint.
func (p *Provider) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("yahoo: provider is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("yahoo: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("yahoo: from must be before or equal to to")
	}

	// Sync baseURL in case tests changed it directly.
	if p.baseURL != p.api.BaseURL() {
		p.api.SetBaseURL(p.baseURL)
	}
	p.api.SetHTTPClient(p.httpClient)

	params := url.Values{
		"q":           []string{ticker},
		"newsCount":   []string{"20"},
		"quotesCount": []string{"0"},
	}

	body, _, err := p.api.Get(ctx, "/v1/finance/search", params)
	if err != nil {
		var apiErr *data.APIError
		if errors.As(err, &apiErr) {
			message := strings.TrimSpace(string(apiErr.Body))
			if message == "" {
				message = http.StatusText(apiErr.StatusCode)
			}
			return nil, fmt.Errorf("yahoo: news request failed with status %d: %s", apiErr.StatusCode, message)
		}
		return nil, err
	}

	var resp searchResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("yahoo: decode search response: %w", err)
	}

	fromUTC := from.UTC()
	toUTC := to.UTC()
	articles := make([]data.NewsArticle, 0, len(resp.News))
	for _, item := range resp.News {
		publishedAt := time.Unix(int64(item.ProviderPublishTime), 0).UTC()
		if publishedAt.Before(fromUTC) || publishedAt.After(toUTC) {
			continue
		}
		articles = append(articles, data.NewsArticle{
			Title:          item.Title,
			URL:            item.Link,
			Source:         item.Publisher,
			PublishedAt:    publishedAt,
			RelatedTickers: append([]string(nil), item.RelatedTickers...),
		})
	}

	sort.Slice(articles, func(i, j int) bool {
		return articles[i].PublishedAt.Before(articles[j].PublishedAt)
	})

	return articles, nil
}

// GetSocialSentiment is not supported by the Yahoo provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]data.SocialSentiment, error) {
	if p == nil {
		return nil, errors.New("yahoo: provider is nil")
	}

	return nil, fmt.Errorf("yahoo: GetSocialSentiment: %w", data.ErrNotImplemented)
}

func mapTimeframe(timeframe data.Timeframe) (timeframeMapping, error) {
	switch timeframe {
	case data.Timeframe1m:
		return timeframeMapping{interval: "1m", duration: time.Minute}, nil
	case data.Timeframe5m:
		return timeframeMapping{interval: "5m", duration: 5 * time.Minute}, nil
	case data.Timeframe15m:
		return timeframeMapping{interval: "15m", duration: 15 * time.Minute}, nil
	case data.Timeframe1h:
		return timeframeMapping{interval: "1h", duration: time.Hour}, nil
	case data.Timeframe1d:
		return timeframeMapping{interval: "1d", duration: 24 * time.Hour}, nil
	default:
		return timeframeMapping{}, fmt.Errorf("yahoo: unsupported timeframe %q", timeframe)
	}
}

func firstQuote(quotes []chartQuote) *chartQuote {
	if len(quotes) == 0 {
		return nil
	}

	return &quotes[0]
}

func (q *chartQuote) bar(index int) (float64, float64, float64, float64, bool) {
	if q == nil || index >= len(q.Open) || index >= len(q.High) || index >= len(q.Low) || index >= len(q.Close) {
		return 0, 0, 0, 0, false
	}
	if q.Open[index] == nil || q.High[index] == nil || q.Low[index] == nil || q.Close[index] == nil {
		return 0, 0, 0, 0, false
	}

	return *q.Open[index], *q.High[index], *q.Low[index], *q.Close[index], true
}

func (q *chartQuote) volume(index int) float64 {
	if q == nil || index >= len(q.Volume) || q.Volume[index] == nil {
		return 0
	}

	return *q.Volume[index]
}
