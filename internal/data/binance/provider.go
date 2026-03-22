package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	defaultBaseURL            = "https://api.binance.com"
	defaultTimeout            = 30 * time.Second
	defaultUA                 = "get-rich-quick/1.0"
	defaultRateLimitPerMinute = 1200
	maxKlinesPerRequest       = 1000
)

// Provider retrieves crypto market data from Binance public market-data endpoints.
type Provider struct {
	baseURL     string
	httpClient  *http.Client
	logger      *slog.Logger
	rateLimiter *data.RateLimiter
}

var _ data.DataProvider = (*Provider)(nil)

type timeframeMapping struct {
	interval string
	duration time.Duration
}

type apiErrorResponse struct {
	Msg     string `json:"msg"`
	Message string `json:"message"`
}

// NewProvider constructs a Binance market-data provider.
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
		logger:      logger,
		rateLimiter: data.NewRateLimiter(defaultRateLimitPerMinute, time.Minute),
	}
}

// GetOHLCV returns candlestick data from Binance's klines endpoint.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("binance: provider is nil")
	}
	if p.httpClient == nil {
		return nil, errors.New("binance: http client is nil")
	}
	if p.rateLimiter == nil {
		return nil, errors.New("binance: rate limiter is nil")
	}

	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return nil, errors.New("binance: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("binance: from must be before or equal to to")
	}

	mapping, err := mapTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	fromUTC := from.UTC()
	toUTC := to.UTC()
	bars := make([]domain.OHLCV, 0, 128)

	for nextStart := fromUTC; !nextStart.After(toUTC); {
		pageBars, err := p.getKlinesPage(ctx, ticker, mapping, nextStart, toUTC)
		if err != nil {
			return nil, err
		}
		if len(pageBars) == 0 {
			break
		}

		bars = append(bars, pageBars...)

		lastBarTime := pageBars[len(pageBars)-1].Timestamp
		if len(pageBars) < maxKlinesPerRequest || !lastBarTime.Before(toUTC) {
			break
		}

		nextStart = lastBarTime.Add(mapping.duration)
	}

	return bars, nil
}

// GetFundamentals is not supported by the Binance provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("binance: provider is nil")
	}

	return data.Fundamentals{}, fmt.Errorf("binance: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews is not supported by the Binance provider yet.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("binance: provider is nil")
	}

	return nil, fmt.Errorf("binance: GetNews: %w", data.ErrNotImplemented)
}

// GetSocialSentiment is not supported by the Binance provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string) (data.SocialSentiment, error) {
	if p == nil {
		return data.SocialSentiment{}, errors.New("binance: provider is nil")
	}

	return data.SocialSentiment{}, fmt.Errorf("binance: GetSocialSentiment: %w", data.ErrNotImplemented)
}

func (p *Provider) getKlinesPage(ctx context.Context, ticker string, mapping timeframeMapping, from, to time.Time) ([]domain.OHLCV, error) {
	requestURL, err := p.buildKlinesURL(ticker, mapping.interval, from, to)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("binance: create request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", defaultUA)

	reservation, err := p.rateLimiter.Reserve(ctx)
	if err != nil {
		return nil, fmt.Errorf("binance: wait for rate limiter: %w", err)
	}
	committedReservation := false
	defer func() {
		if !committedReservation {
			reservation.Cancel()
		}
	}()

	startedAt := time.Now()
	p.logger.Info("binance: sending request",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
	)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		p.logger.Warn("binance: request failed",
			slog.String("method", req.Method),
			slog.String("path", req.URL.Path),
			slog.Any("error", err),
			slog.Int64("duration_ms", time.Since(startedAt).Milliseconds()),
		)
		return nil, fmt.Errorf("binance: do request: %w", err)
	}
	reservation.Commit()
	committedReservation = true
	defer func() {
		if closeErr := resp.Body.Close(); closeErr != nil {
			p.logger.Warn("binance: failed to close response body", slog.Any("error", closeErr))
		}
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("binance: read response body: %w", err)
	}

	durationMS := time.Since(startedAt).Milliseconds()
	p.logger.Info("binance: received response",
		slog.String("method", req.Method),
		slog.String("path", req.URL.Path),
		slog.Int("status", resp.StatusCode),
		slog.Int64("duration_ms", durationMS),
	)

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("binance: request failed with status %d: %s", resp.StatusCode, parseErrorMessage(resp.StatusCode, body))
	}

	return decodeKlines(body, from, to)
}

func (p *Provider) buildKlinesURL(ticker, interval string, from, to time.Time) (string, error) {
	baseURL, err := url.Parse(p.baseURL)
	if err != nil {
		return "", fmt.Errorf("binance: parse base url: %w", err)
	}

	baseURL.Path = strings.TrimRight(baseURL.Path, "/") + "/api/v3/klines"
	query := baseURL.Query()
	query.Set("symbol", ticker)
	query.Set("interval", interval)
	query.Set("startTime", strconv.FormatInt(from.UnixMilli(), 10))
	query.Set("endTime", strconv.FormatInt(to.UnixMilli(), 10))
	query.Set("limit", strconv.Itoa(maxKlinesPerRequest))
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
		return timeframeMapping{}, fmt.Errorf("binance: unsupported timeframe %q", timeframe)
	}
}

func decodeKlines(body []byte, from, to time.Time) ([]domain.OHLCV, error) {
	var rawKlines [][]json.RawMessage
	if err := json.Unmarshal(body, &rawKlines); err != nil {
		return nil, fmt.Errorf("binance: decode klines response: %w", err)
	}

	bars := make([]domain.OHLCV, 0, len(rawKlines))
	for index, rawKline := range rawKlines {
		if len(rawKline) < 6 {
			return nil, fmt.Errorf("binance: decode klines response: kline %d has %d fields, want at least 6", index, len(rawKline))
		}

		timestamp, err := decodeInt64Field(rawKline[0], "open time", index)
		if err != nil {
			return nil, err
		}
		open, err := decodeFloatField(rawKline[1], "open", index)
		if err != nil {
			return nil, err
		}
		high, err := decodeFloatField(rawKline[2], "high", index)
		if err != nil {
			return nil, err
		}
		low, err := decodeFloatField(rawKline[3], "low", index)
		if err != nil {
			return nil, err
		}
		closePrice, err := decodeFloatField(rawKline[4], "close", index)
		if err != nil {
			return nil, err
		}
		volume, err := decodeFloatField(rawKline[5], "volume", index)
		if err != nil {
			return nil, err
		}

		barTime := time.UnixMilli(timestamp).UTC()
		if barTime.Before(from) || barTime.After(to) {
			continue
		}

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

func decodeInt64Field(raw json.RawMessage, field string, index int) (int64, error) {
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fmt.Errorf("binance: decode klines response: parse %s for kline %d: %w", field, index, err)
	}

	return value, nil
}

func decodeFloatField(raw json.RawMessage, field string, index int) (float64, error) {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, fmt.Errorf("binance: decode klines response: parse %s for kline %d: %w", field, index, err)
	}

	parsedValue, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("binance: decode klines response: parse %s for kline %d: %w", field, index, err)
	}

	return parsedValue, nil
}

func parseErrorMessage(statusCode int, body []byte) string {
	message := strings.TrimSpace(string(body))
	if len(body) > 0 {
		var response apiErrorResponse
		if err := json.Unmarshal(body, &response); err == nil {
			if parsedMessage := strings.TrimSpace(response.Msg); parsedMessage != "" {
				message = parsedMessage
			} else if parsedMessage := strings.TrimSpace(response.Message); parsedMessage != "" {
				message = parsedMessage
			}
		}
	}
	if message == "" {
		message = http.StatusText(statusCode)
	}
	if message == "" {
		message = "request failed"
	}

	return message
}
