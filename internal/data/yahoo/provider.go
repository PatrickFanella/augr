package yahoo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
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
	logger     *slog.Logger
}

var _ data.DataProvider = (*Provider)(nil)

type timeframeMapping struct {
	interval string
	duration time.Duration
}

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
	Timestamp  []int64           `json:"timestamp"`
	Indicators chartIndicators   `json:"indicators"`
	Meta       json.RawMessage   `json:"meta"`
	Events     json.RawMessage   `json:"events"`
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

	return &Provider{
		baseURL: defaultBaseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
		logger: logger,
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

	requestURL, err := p.buildChartURL(ticker, mapping, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("yahoo: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUA)

	startedAt := time.Now()
	p.logger.Info("yahoo: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Warn("yahoo: request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("yahoo: do request: %w", err)
	}
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			p.logger.Warn("yahoo: failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("yahoo: read response body: %w", err)
	}

	durationMS := time.Since(startedAt).Milliseconds()
	p.logger.Info("yahoo: received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
	)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		message := strings.TrimSpace(string(body))
		if message == "" {
			message = http.StatusText(resp.StatusCode)
		}
		return nil, fmt.Errorf("yahoo: request failed with status %d: %s", resp.StatusCode, message)
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
		open, high, low, close, ok := quote.bar(index)
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
			Close:     close,
			Volume:    volume,
		})
	}

	return bars, nil
}

// GetFundamentals is not supported by the Yahoo provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("yahoo: provider is nil")
	}

	return data.Fundamentals{}, fmt.Errorf("yahoo: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews is not supported by the Yahoo provider yet.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("yahoo: provider is nil")
	}

	return nil, fmt.Errorf("yahoo: GetNews: %w", data.ErrNotImplemented)
}

// GetSocialSentiment is not supported by the Yahoo provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string) (data.SocialSentiment, error) {
	if p == nil {
		return data.SocialSentiment{}, errors.New("yahoo: provider is nil")
	}

	return data.SocialSentiment{}, fmt.Errorf("yahoo: GetSocialSentiment: %w", data.ErrNotImplemented)
}

func (p *Provider) buildChartURL(ticker string, mapping timeframeMapping, from, to time.Time) (string, error) {
	baseURL, err := url.Parse(p.baseURL)
	if err != nil {
		return "", fmt.Errorf("yahoo: parse base url: %w", err)
	}

	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/v8/finance/chart/" + url.PathEscape(ticker)

	query := baseURL.Query()
	query.Set("interval", mapping.interval)
	query.Set("includePrePost", "false")
	query.Set("period1", fmt.Sprintf("%d", from.Unix()))
	query.Set("period2", fmt.Sprintf("%d", to.Add(mapping.duration).Unix()))
	baseURL.RawQuery = query.Encode()

	return baseURL.String(), nil
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
