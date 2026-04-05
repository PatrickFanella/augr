package fmp

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

// Provider retrieves market data from Financial Modeling Prep.
type Provider struct {
	client *Client
}

var _ data.DataProvider = (*Provider)(nil)

type historicalPriceResponse struct {
	Symbol     string           `json:"symbol"`
	Historical []historicalBar  `json:"historical"`
}

type historicalBar struct {
	Date   string  `json:"date"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

type profileEntry struct {
	MktCap        float64 `json:"mktCap"`
	PE            float64 `json:"pe"`
	EPS           float64 `json:"eps"`
	DividendYield float64 `json:"lastDiv"`
	Revenue       float64 `json:"revenue"`
	GrossMargin   float64 `json:"grossMargin"`
	DebtToEquity  float64 `json:"debtToEquity"`
	FreeCashFlow  float64 `json:"freeCashFlow"`
}

type fmpNewsItem struct {
	Title         string `json:"title"`
	Text          string `json:"text"`
	URL           string `json:"url"`
	Site          string `json:"site"`
	PublishedDate string `json:"publishedDate"`
}

// NewProvider constructs an FMP market-data provider.
func NewProvider(client *Client) *Provider {
	return &Provider{client: client}
}

// GetOHLCV returns candlestick data from FMP's historical-price-full endpoint.
func (p *Provider) GetOHLCV(ctx context.Context, ticker string, timeframe data.Timeframe, from, to time.Time) ([]domain.OHLCV, error) {
	if p == nil {
		return nil, errors.New("fmp: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("fmp: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("fmp: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("fmp: from must be before or equal to to")
	}

	if timeframe != data.Timeframe1d {
		return nil, fmt.Errorf("fmp: unsupported timeframe %q (only daily supported)", timeframe)
	}

	path := fmt.Sprintf("/historical-price-full/%s", url.PathEscape(ticker))
	params := url.Values{
		"from": []string{from.UTC().Format("2006-01-02")},
		"to":   []string{to.UTC().Format("2006-01-02")},
	}

	body, err := p.client.Get(ctx, path, params)
	if err != nil {
		return nil, err
	}

	var response historicalPriceResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("fmp: decode historical price response: %w", err)
	}

	if len(response.Historical) == 0 {
		return nil, fmt.Errorf("fmp: no historical data returned for %s", ticker)
	}

	bars := make([]domain.OHLCV, 0, len(response.Historical))
	for _, bar := range response.Historical {
		barTime, err := time.Parse("2006-01-02", strings.TrimSpace(bar.Date))
		if err != nil {
			return nil, fmt.Errorf("fmp: parse date %q: %w", bar.Date, err)
		}

		bars = append(bars, domain.OHLCV{
			Timestamp: barTime.UTC(),
			Open:      bar.Open,
			High:      bar.High,
			Low:       bar.Low,
			Close:     bar.Close,
			Volume:    bar.Volume,
		})
	}

	sort.Slice(bars, func(i, j int) bool {
		return bars[i].Timestamp.Before(bars[j].Timestamp)
	})

	return bars, nil
}

// GetFundamentals returns fundamental data from FMP's profile endpoint.
func (p *Provider) GetFundamentals(ctx context.Context, ticker string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("fmp: provider is nil")
	}
	if p.client == nil {
		return data.Fundamentals{}, errors.New("fmp: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return data.Fundamentals{}, errors.New("fmp: ticker is required")
	}

	path := fmt.Sprintf("/profile/%s", url.PathEscape(ticker))

	body, err := p.client.Get(ctx, path, nil)
	if err != nil {
		return data.Fundamentals{}, fmt.Errorf("fmp: GetFundamentals: %w", err)
	}

	var profiles []profileEntry
	if err := json.Unmarshal(body, &profiles); err != nil {
		return data.Fundamentals{}, fmt.Errorf("fmp: decode profile response: %w", err)
	}

	if len(profiles) == 0 {
		return data.Fundamentals{}, fmt.Errorf("fmp: no profile data returned for %s", ticker)
	}

	profile := profiles[0]
	return data.Fundamentals{
		Ticker:        ticker,
		MarketCap:     profile.MktCap,
		PERatio:       profile.PE,
		EPS:           profile.EPS,
		Revenue:       profile.Revenue,
		GrossMargin:   profile.GrossMargin,
		DebtToEquity:  profile.DebtToEquity,
		FreeCashFlow:  profile.FreeCashFlow,
		DividendYield: profile.DividendYield,
		FetchedAt:     time.Now().UTC(),
	}, nil
}

// GetNews returns news articles from FMP's stock_news endpoint.
func (p *Provider) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
	if p == nil {
		return nil, errors.New("fmp: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("fmp: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return nil, errors.New("fmp: ticker is required")
	}
	if from.After(to) {
		return nil, errors.New("fmp: from must be before or equal to to")
	}

	params := url.Values{
		"tickers": []string{ticker},
		"limit":   []string{"50"},
	}

	body, err := p.client.Get(ctx, "/stock_news", params)
	if err != nil {
		return nil, err
	}

	var items []fmpNewsItem
	if err := json.Unmarshal(body, &items); err != nil {
		return nil, fmt.Errorf("fmp: decode news response: %w", err)
	}

	articles := make([]data.NewsArticle, 0, len(items))
	for _, item := range items {
		publishedAt, err := time.Parse("2006-01-02 15:04:05", strings.TrimSpace(item.PublishedDate))
		if err != nil {
			// Try alternate format without time component.
			publishedAt, err = time.Parse("2006-01-02", strings.TrimSpace(item.PublishedDate))
			if err != nil {
				return nil, fmt.Errorf("fmp: parse news publishedDate %q: %w", item.PublishedDate, err)
			}
		}

		publishedAt = publishedAt.UTC()
		if publishedAt.Before(from.UTC()) || publishedAt.After(to.UTC()) {
			continue
		}

		articles = append(articles, data.NewsArticle{
			Title:       item.Title,
			Summary:     item.Text,
			URL:         item.URL,
			Source:      item.Site,
			PublishedAt: publishedAt,
		})
	}

	sort.Slice(articles, func(i, j int) bool {
		return articles[i].PublishedAt.Before(articles[j].PublishedAt)
	})

	return articles, nil
}

// GetSocialSentiment is not supported by the FMP provider.
func (p *Provider) GetSocialSentiment(_ context.Context, _ string, _, _ time.Time) ([]data.SocialSentiment, error) {
	if p == nil {
		return nil, errors.New("fmp: provider is nil")
	}

	return nil, fmt.Errorf("fmp: GetSocialSentiment: %w", data.ErrNotImplemented)
}
