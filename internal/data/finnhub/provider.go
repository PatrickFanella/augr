package finnhub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// Provider retrieves market data from Finnhub.
type Provider struct {
	client *Client
}

var _ data.DataProvider = (*Provider)(nil)

type candleResponse struct {
	Close     []float64 `json:"c"`
	High      []float64 `json:"h"`
	Low       []float64 `json:"l"`
	Open      []float64 `json:"o"`
	Status    string    `json:"s"`
	Timestamp []int64   `json:"t"`
	Volume    []float64 `json:"v"`
}

type metricResponse struct {
	Metric metricFields `json:"metric"`
}

type metricFields struct {
	PEBasicExclExtraTTM      float64 `json:"peBasicExclExtraTTM"`
	EPSBasicExclExtraTTM     float64 `json:"epsBasicExclExtraTTM"`
	RevenuePerShareTTM       float64 `json:"revenuePerShareTTM"`
	DividendYieldIndicatedAn float64 `json:"dividendYieldIndicatedAnnual"`
	MarketCapitalization     float64 `json:"marketCapitalization"`
	TotalDebtEquityAnnual    float64 `json:"totalDebt/totalEquityAnnual"`
	RevenueGrowthTTMYoY      float64 `json:"revenueGrowthTTMYoy"`
	GrossMarginTTM           float64 `json:"grossMarginTTM"`
	FreeCashFlowTTM          float64 `json:"freeCashFlowTTM"`
	RevenueTTM               float64 `json:"revenueTTM"`
}

type newsItem struct {
	Headline string `json:"headline"`
	Summary  string `json:"summary"`
	URL      string `json:"url"`
	Source   string `json:"source"`
	Datetime int64  `json:"datetime"`
}

// NewProvider constructs a Finnhub market-data provider.
func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

// GetOHLCV returns candlestick data from Finnhub's stock/candle endpoint.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("finnhub: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("finnhub: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("finnhub: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("finnhub: from must be before or equal to to")
	}

	resolution, err := mapResolution(timeframe)
	if err != nil {
		return nil, err
	}

	params := url.Values{
		"symbol":     []string{ticker},
		"resolution": []string{resolution},
		"from":       []string{fmt.Sprintf("%d", from.UTC().Unix())},
		"to":         []string{fmt.Sprintf("%d", to.UTC().Unix())},
	}

	body, err := p.client.Get(ctx, "/stock/candle", params)
	if err != nil {
		return nil, err
	}

	var response candleResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("finnhub: decode candle response: %w", err)
	}

	if response.Status != "ok" {
		return nil, fmt.Errorf("finnhub: candle response status %q", response.Status)
	}

	n := len(response.Timestamp)
	if n == 0 {
		return nil, fmt.Errorf("finnhub: no candle data returned for %s", ticker)
	}
	if len(response.Open) != n || len(response.High) != n || len(response.Low) != n || len(response.Close) != n || len(response.Volume) != n {
		return nil, fmt.Errorf("finnhub: mismatched candle array lengths for %s", ticker)
	}

	bars := make([]domain.OHLCV, 0, n)
	for i := 0; i < n; i++ {
		bars = append(bars, domain.OHLCV{
			Timestamp: time.Unix(response.Timestamp[i], 0).UTC(),
			Open:      response.Open[i],
			High:      response.High[i],
			Low:       response.Low[i],
			Close:     response.Close[i],
			Volume:    response.Volume[i],
		})
	}

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp.Before(bars[j].Timestamp)
	})

	return bars, nil
}

// GetFundamentals returns fundamental data from Finnhub's stock/metric endpoint.
func (p *Provider) GetFundamentals(ctx context.Context, ticker string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("finnhub: provider is nil")
	}
	if p.client == nil {
		return data.Fundamentals{}, errors.New("finnhub: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return data.Fundamentals{}, errors.New("finnhub: ticker is required")
	}

	params := url.Values{
		"symbol": []string{ticker},
		"metric": []string{"all"},
	}

	body, err := p.client.Get(ctx, "/stock/metric", params)
	if err != nil {
		return data.Fundamentals{}, fmt.Errorf("finnhub: GetFundamentals: %w", err)
	}

	var response metricResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return data.Fundamentals{}, fmt.Errorf("finnhub: decode metric response: %w", err)
	}

	m := response.Metric
	return data.Fundamentals{
		Ticker:           ticker,
		MarketCap:        m.MarketCapitalization * 1e6, // Finnhub reports in millions
		PERatio:          m.PEBasicExclExtraTTM,
		EPS:              m.EPSBasicExclExtraTTM,
		Revenue:          m.RevenueTTM,
		RevenueGrowthYoY: m.RevenueGrowthTTMYoY / 100, // convert percentage to ratio
		GrossMargin:      m.GrossMarginTTM / 100,       // convert percentage to ratio
		DebtToEquity:     m.TotalDebtEquityAnnual,
		FreeCashFlow:     m.FreeCashFlowTTM,
		DividendYield:    m.DividendYieldIndicatedAn / 100, // convert percentage to ratio
		FetchedAt:        time.Now().UTC(),
	}, nil
}

// GetNews returns news articles from Finnhub's company-news endpoint.
func (p *Provider) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("finnhub: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("finnhub: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("finnhub: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("finnhub: from must be before or equal to to")
	}

	params := url.Values{
		"symbol": []string{ticker},
		"from":   []string{from.UTC().Format("2006-01-02")},
		"to":     []string{to.UTC().Format("2006-01-02")},
	}

	body, err := p.client.Get(ctx, "/company-news", params)
	if err != nil {
		return nil, err
	}

	var items []newsItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("finnhub: decode news response: %w", err)
	}

	articles := make([]data.NewsArticle, 0, len(items))
	for _, item := range items {
		publishedAt := time.Unix(item.Datetime, 0).UTC()
		if publishedAt.Before(from.UTC()) || publishedAt.After(to.UTC()) {
			continue
		}

		articles = append(articles, data.NewsArticle{
			Title:       item.Headline,
			Summary:     item.Summary,
			URL:         item.URL,
			Source:      item.Source,
			PublishedAt: publishedAt,
		})
	}

	sort.Slice(articles, func(i, j int) bool {
		return articles[i].PublishedAt.Before(articles[j].PublishedAt)
	})

	return articles, nil
}

// GetSocialSentiment is not supported by the Finnhub provider.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]data.SocialSentiment, error) {
	if p == nil {
		return nil, errors.New("finnhub: provider is nil")
	}

	return nil, fmt.Errorf("finnhub: GetSocialSentiment: %w", data.ErrNotImplemented)
}

func mapResolution(timeframe data.Timeframe) (string, error) {
	switch timeframe {
	case data.Timeframe1d:
		return "D", nil
	case data.Timeframe1h:
		return "60", nil
	case data.Timeframe15m:
		return "15", nil
	case data.Timeframe5m:
		return "5", nil
	case data.Timeframe1m:
		return "1", nil
	default:
		return "", fmt.Errorf("finnhub: unsupported timeframe %q", timeframe)
	}
}
