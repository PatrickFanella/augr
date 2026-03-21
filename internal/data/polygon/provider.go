package polygon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

const polygonMaxPageSize = 50000

// Provider retrieves market data from Polygon.io.
type Provider struct {
	client *Client
}

var _ data.DataProvider = (*Provider)(nil)

type aggregateResponse struct {
	NextURL string            `json:"next_url"`
	Results []aggregateResult `json:"results"`
}

type aggregateResult struct {
	Open      float64 `json:"o"`
	High      float64 `json:"h"`
	Low       float64 `json:"l"`
	Close     float64 `json:"c"`
	Volume    float64 `json:"v"`
	Timestamp int64   `json:"t"`
}

type timeframeMapping struct {
	multiplier int
	timespan   string
}

// NewProvider constructs a Polygon market-data provider.
func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

// GetOHLCV returns candlestick data from Polygon's aggregates endpoint.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("polygon: provider is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("polygon: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("polygon: from must be before or equal to to")
	}

	mapping, err := mapTimeframe(timeframe)
	if err != nil {
		return nil, err
	}

	requestPath := fmt.Sprintf(
		"/v2/aggs/ticker/%s/range/%d/%s/%d/%d",
		url.PathEscape(ticker),
		mapping.multiplier,
		mapping.timespan,
		from.UTC().UnixMilli(),
		to.UTC().UnixMilli(),
	)
	params := url.Values{
		"adjusted": []string{"true"},
		"sort":     []string{"asc"},
		"limit":    []string{strconv.Itoa(polygonMaxPageSize)},
	}

	bars := make([]domain.OHLCV, 0)
	for {
		body, err := p.client.Get(ctx, requestPath, params)
		if err != nil {
			return nil, err
		}

		var response aggregateResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("polygon: decode aggregates response: %w", err)
		}

		for _, result := range response.Results {
			bars = append(bars, domain.OHLCV{
				Timestamp: time.UnixMilli(result.Timestamp).UTC(),
				Open:      result.Open,
				High:      result.High,
				Low:       result.Low,
				Close:     result.Close,
				Volume:    result.Volume,
			})
		}

		if strings.TrimSpace(response.NextURL) == "" {
			break
		}

		requestPath, params, err = nextPageRequest(response.NextURL)
		if err != nil {
			return nil, err
		}
	}

	if len(bars) == 0 {
		return []domain.OHLCV{}, nil
	}

	return bars, nil
}

// GetFundamentals is not supported by the Polygon provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	return data.Fundamentals{}, errors.New("polygon: GetFundamentals not supported")
}

// GetNews is not supported by the Polygon provider yet.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	return nil, errors.New("polygon: GetNews not supported")
}

// GetSocialSentiment is not supported by the Polygon provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string) (data.SocialSentiment, error) {
	return data.SocialSentiment{}, errors.New("polygon: GetSocialSentiment not supported")
}

func mapTimeframe(timeframe data.Timeframe) (timeframeMapping, error) {
	switch timeframe {
	case data.Timeframe1m:
		return timeframeMapping{multiplier: 1, timespan: "minute"}, nil
	case data.Timeframe5m:
		return timeframeMapping{multiplier: 5, timespan: "minute"}, nil
	case data.Timeframe15m:
		return timeframeMapping{multiplier: 15, timespan: "minute"}, nil
	case data.Timeframe1h:
		return timeframeMapping{multiplier: 1, timespan: "hour"}, nil
	case data.Timeframe1d:
		return timeframeMapping{multiplier: 1, timespan: "day"}, nil
	default:
		return timeframeMapping{}, fmt.Errorf("polygon: unsupported timeframe %q", timeframe)
	}
}

func nextPageRequest(nextURL string) (string, url.Values, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(nextURL))
	if err != nil {
		return "", nil, fmt.Errorf("polygon: parse next url: %w", err)
	}

	params := parsedURL.Query()
	params.Del("apiKey")

	return parsedURL.Path, params, nil
}
