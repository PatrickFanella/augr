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

const (
	polygonMaxPageSize     = 50000
	polygonNewsMaxPageSize = 1000
)

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

type newsResponse struct {
	NextURL string       `json:"next_url"`
	Results []newsResult `json:"results"`
}

type newsResult struct {
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	ArticleURL   string        `json:"article_url"`
	PublishedUTC string        `json:"published_utc"`
	Publisher    newsPublisher `json:"publisher"`
	Insights     []newsInsight `json:"insights"`
}

type newsPublisher struct {
	Name string `json:"name"`
}

type newsInsight struct {
	Ticker    string `json:"ticker"`
	Sentiment string `json:"sentiment"`
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
	if p.client == nil {
		return nil, errors.New("polygon: client is nil")
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
	baseParams := url.Values{
		"adjusted": []string{"true"},
		"sort":     []string{"asc"},
		"limit":    []string{strconv.Itoa(polygonMaxPageSize)},
	}
	params := cloneQueryValues(baseParams)

	bars := make([]domain.OHLCV, 0, 128)
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

		requestPath, params, err = nextPageRequest(response.NextURL, baseParams)
		if err != nil {
			return nil, err
		}
	}

	return bars, nil
}

// GetFundamentals is not supported by the Polygon provider yet.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("polygon: provider is nil")
	}

	return data.Fundamentals{}, fmt.Errorf("polygon: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews returns news articles from Polygon's ticker news endpoint.
func (p *Provider) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("polygon: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("polygon: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("polygon: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("polygon: from must be before or equal to to")
	}

	requestPath := "/v2/reference/news"
	baseParams := url.Values{
		"ticker":            []string{ticker},
		"published_utc.gte": []string{from.UTC().Format(time.RFC3339Nano)},
		"published_utc.lte": []string{to.UTC().Format(time.RFC3339Nano)},
		"sort":              []string{"published_utc"},
		"order":             []string{"asc"},
		"limit":             []string{strconv.Itoa(polygonNewsMaxPageSize)},
	}
	params := cloneQueryValues(baseParams)

	articles := make([]data.NewsArticle, 0, 16)
	for {
		body, err := p.client.Get(ctx, requestPath, params)
		if err != nil {
			return nil, err
		}

		var response newsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("polygon: decode news response: %w", err)
		}

		for _, result := range response.Results {
			publishedAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(result.PublishedUTC))
			if err != nil {
				return nil, fmt.Errorf("polygon: parse news published_utc %q: %w", result.PublishedUTC, err)
			}

			articles = append(articles, data.NewsArticle{
				Title:       result.Title,
				Summary:     result.Description,
				URL:         result.ArticleURL,
				Source:      result.Publisher.Name,
				PublishedAt: publishedAt.UTC(),
				Sentiment:   mapNewsSentiment(ticker, result.Insights),
			})
		}

		if strings.TrimSpace(response.NextURL) == "" {
			break
		}

		requestPath, params, err = nextPageRequest(response.NextURL, baseParams)
		if err != nil {
			return nil, err
		}
	}

	return articles, nil
}

// GetSocialSentiment is not supported by the Polygon provider yet.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string) (data.SocialSentiment, error) {
	if p == nil {
		return data.SocialSentiment{}, errors.New("polygon: provider is nil")
	}

	return data.SocialSentiment{}, fmt.Errorf("polygon: GetSocialSentiment: %w", data.ErrNotImplemented)
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

func nextPageRequest(nextURL string, baseParams url.Values) (string, url.Values, error) {
	parsedURL, err := url.Parse(strings.TrimSpace(nextURL))
	if err != nil {
		return "", nil, fmt.Errorf("polygon: parse next url: %w", err)
	}

	params := cloneQueryValues(baseParams)
	for key, values := range parsedURL.Query() {
		params.Del(key)
		for _, value := range values {
			params.Add(key, value)
		}
	}
	params.Del("apiKey")

	return parsedURL.Path, params, nil
}

func cloneQueryValues(values url.Values) url.Values {
	cloned := make(url.Values, len(values))
	for key, entries := range values {
		cloned[key] = append([]string(nil), entries...)
	}
	return cloned
}

func mapNewsSentiment(ticker string, insights []newsInsight) float64 {
	for _, insight := range insights {
		if !strings.EqualFold(strings.TrimSpace(insight.Ticker), ticker) {
			continue
		}

		switch strings.ToLower(strings.TrimSpace(insight.Sentiment)) {
		case "positive":
			return 1
		case "negative":
			return -1
		case "neutral":
			return 0
		}
	}

	return 0
}
