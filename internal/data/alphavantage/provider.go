package alphavantage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const (
	functionTimeSeriesDaily    = "TIME_SERIES_DAILY"
	functionTimeSeriesIntraday = "TIME_SERIES_INTRADAY"
)

// Provider retrieves market data from Alpha Vantage.
type Provider struct {
	client *Client
}

var _ data.DataProvider = (*Provider)(nil)

type timeframeMapping struct {
	function string
	interval string
}

type timeSeriesBar struct {
	Open   string `json:"1. open"`
	High   string `json:"2. high"`
	Low    string `json:"3. low"`
	Close  string `json:"4. close"`
	Volume string `json:"5. volume"`
}

// NewProvider constructs an Alpha Vantage market-data provider.
func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

// GetOHLCV returns candlestick data from Alpha Vantage TIME_SERIES endpoints.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("alphavantage: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("alphavantage: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("alphavantage: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("alphavantage: from must be before or equal to to")
	}

	mapping, err := mapTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"function":   []string{mapping.function},
		"symbol":     []string{ticker},
		"outputsize": []string{"full"},
	}
	if mapping.interval != "" {
		params.Set("interval", mapping.interval)
	}

	body, err := p.client.Get(ctx, params)
	if err != nil {
		return nil, err
	}

	bars, err := decodeOHLCV(body, from.UTC(), to.UTC())
	if err != nil {
		return nil, err
	}

	return bars, nil
}

// GetFundamentals is not supported by the Alpha Vantage provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("alphavantage: provider is nil")
	}

	return data.Fundamentals{}, fmt.Errorf("alphavantage: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews is not supported by the Alpha Vantage provider yet.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("alphavantage: provider is nil")
	}

	return nil, fmt.Errorf("alphavantage: GetNews: %w", data.ErrNotImplemented)
}

// GetSocialSentiment is not supported by the Alpha Vantage provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string) (data.SocialSentiment, error) {
	if p == nil {
		return data.SocialSentiment{}, errors.New("alphavantage: provider is nil")
	}

	return data.SocialSentiment{}, fmt.Errorf("alphavantage: GetSocialSentiment: %w", data.ErrNotImplemented)
}

func mapTimeframe(timeframe data.Timeframe) (timeframeMapping, error) {
	switch timeframe {
	case data.Timeframe1m:
		return timeframeMapping{function: functionTimeSeriesIntraday, interval: "1min"}, nil
	case data.Timeframe5m:
		return timeframeMapping{function: functionTimeSeriesIntraday, interval: "5min"}, nil
	case data.Timeframe15m:
		return timeframeMapping{function: functionTimeSeriesIntraday, interval: "15min"}, nil
	case data.Timeframe1h:
		return timeframeMapping{function: functionTimeSeriesIntraday, interval: "60min"}, nil
	case data.Timeframe1d:
		return timeframeMapping{function: functionTimeSeriesDaily}, nil
	default:
		return timeframeMapping{}, fmt.Errorf("alphavantage: unsupported timeframe %q", timeframe)
	}
}

func decodeOHLCV(body []byte, from, to time.Time) ([]domain.OHLCV, error) {
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("alphavantage: decode time series response: %w", err)
	}

	location, err := responseLocation(payload)
	if err != nil {
		return nil, err
	}

	seriesKey, ok := timeSeriesKey(payload)
	if !ok {
		return nil, errors.New("alphavantage: time series data not found in response")
	}

	var series map[string]timeSeriesBar
	if err := json.Unmarshal(payload[seriesKey], &series); err != nil {
		return nil, fmt.Errorf("alphavantage: decode time series bars: %w", err)
	}

	bars := make([]domain.OHLCV, 0, len(series))
	for timestamp, bar := range series {
		barTime, err := parseBarTime(timestamp, location)
		if err != nil {
			return nil, fmt.Errorf("alphavantage: parse timestamp %q: %w", timestamp, err)
		}
		if barTime.Before(from) || barTime.After(to) {
			continue
		}

		decodedBar, err := decodeBar(barTime, bar)
		if err != nil {
			return nil, fmt.Errorf("alphavantage: %w", err)
		}
		bars = append(bars, decodedBar)
	}

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp.Before(bars[j].Timestamp)
	})

	return bars, nil
}

func responseLocation(payload map[string]json.RawMessage) (*time.Location, error) {
	rawMeta, ok := payload["Meta Data"]
	if !ok {
		return time.UTC, nil
	}

	var meta map[string]string
	if err := json.Unmarshal(rawMeta, &meta); err != nil {
		return nil, fmt.Errorf("alphavantage: decode metadata: %w", err)
	}

	for key, value := range meta {
		if !strings.Contains(key, "Time Zone") {
			continue
		}

		timeZone := strings.TrimSpace(value)
		if timeZone == "" {
			continue
		}

		location, err := time.LoadLocation(timeZone)
		if err != nil {
			return nil, fmt.Errorf("alphavantage: load time zone %q: %w", timeZone, err)
		}

		return location, nil
	}

	return time.UTC, nil
}

func timeSeriesKey(payload map[string]json.RawMessage) (string, bool) {
	for key := range payload {
		if strings.HasPrefix(key, "Time Series") {
			return key, true
		}
	}

	return "", false
}

func parseBarTime(timestamp string, location *time.Location) (time.Time, error) {
	timestamp = strings.TrimSpace(timestamp)
	if len(timestamp) == len("2006-01-02") {
		parsed, err := time.Parse("2006-01-02", timestamp)
		if err != nil {
			return time.Time{}, err
		}

		return parsed.UTC(), nil
	}

	parsed, err := time.ParseInLocation("2006-01-02 15:04:05", timestamp, location)
	if err != nil {
		return time.Time{}, err
	}

	return parsed.UTC(), nil
}

func decodeBar(timestamp time.Time, bar timeSeriesBar) (domain.OHLCV, error) {
	open, err := parseBarValue("open", timestamp, bar.Open)
	if err != nil {
		return domain.OHLCV{}, err
	}
	high, err := parseBarValue("high", timestamp, bar.High)
	if err != nil {
		return domain.OHLCV{}, err
	}
	low, err := parseBarValue("low", timestamp, bar.Low)
	if err != nil {
		return domain.OHLCV{}, err
	}
	closePrice, err := parseBarValue("close", timestamp, bar.Close)
	if err != nil {
		return domain.OHLCV{}, err
	}
	volume, err := parseBarValue("volume", timestamp, bar.Volume)
	if err != nil {
		return domain.OHLCV{}, err
	}

	return domain.OHLCV{
		Timestamp: timestamp,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closePrice,
		Volume:    volume,
	}, nil
}

func parseBarValue(field string, timestamp time.Time, value string) (float64, error) {
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s for %s: %w", field, timestamp.Format(time.RFC3339), err)
	}

	return parsed, nil
}
