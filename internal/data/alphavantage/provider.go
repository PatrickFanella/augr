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
	functionOverview           = "OVERVIEW"
	functionIncomeStatement    = "INCOME_STATEMENT"
	functionBalanceSheet       = "BALANCE_SHEET"
	functionNewsSentiment      = "NEWS_SENTIMENT"
	newsTimestampLayout        = "20060102T150405"
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

type overviewResponse struct {
	Symbol               string `json:"Symbol"`
	MarketCapitalization string `json:"MarketCapitalization"`
	PERatio              string `json:"PERatio"`
	EPS                  string `json:"EPS"`
	DividendYield        string `json:"DividendYield"`
}

type incomeStatementResponse struct {
	AnnualReports    []incomeStatementReport `json:"annualReports"`
	QuarterlyReports []incomeStatementReport `json:"quarterlyReports"`
}

type incomeStatementReport struct {
	FiscalDateEnding string `json:"fiscalDateEnding"`
	TotalRevenue     string `json:"totalRevenue"`
	GrossProfit      string `json:"grossProfit"`
}

type balanceSheetResponse struct {
	AnnualReports    []balanceSheetReport `json:"annualReports"`
	QuarterlyReports []balanceSheetReport `json:"quarterlyReports"`
}

type balanceSheetReport struct {
	FiscalDateEnding       string `json:"fiscalDateEnding"`
	TotalLiabilities       string `json:"totalLiabilities"`
	TotalShareholderEquity string `json:"totalShareholderEquity"`
}

type newsResponse struct {
	Feed []newsFeedItem `json:"feed"`
}

type newsFeedItem struct {
	Title                 string                `json:"title"`
	URL                   string                `json:"url"`
	TimePublished         string                `json:"time_published"`
	Summary               string                `json:"summary"`
	Source                string                `json:"source"`
	OverallSentimentScore optionalFloat64       `json:"overall_sentiment_score"`
	TickerSentiment       []newsTickerSentiment `json:"ticker_sentiment"`
}

type newsTickerSentiment struct {
	Ticker               string          `json:"ticker"`
	TickerSentimentScore optionalFloat64 `json:"ticker_sentiment_score"`
}

type optionalFloat64 float64

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

// GetFundamentals returns fundamentals data from Alpha Vantage OVERVIEW,
// INCOME_STATEMENT, and BALANCE_SHEET endpoints.
func (p *Provider) GetFundamentals(ctx context.Context, ticker string) (data.Fundamentals, error) {
	if p == nil {
		return data.Fundamentals{}, errors.New("alphavantage: provider is nil")
	}
	if p.client == nil {
		return data.Fundamentals{}, errors.New("alphavantage: client is nil")
	}

	ticker = strings.TrimSpace(ticker)
	if ticker == "" {
		return data.Fundamentals{}, errors.New("alphavantage: ticker is required")
	}

	overview, err := p.fetchOverview(ctx, ticker)
	if err != nil {
		return data.Fundamentals{}, fmt.Errorf("alphavantage: GetFundamentals: %w", err)
	}

	incomeStatement, err := p.fetchIncomeStatement(ctx, ticker)
	if err != nil {
		return data.Fundamentals{}, fmt.Errorf("alphavantage: GetFundamentals: %w", err)
	}

	balanceSheet, err := p.fetchBalanceSheet(ctx, ticker)
	if err != nil {
		return data.Fundamentals{}, fmt.Errorf("alphavantage: GetFundamentals: %w", err)
	}

	fundamentals := data.Fundamentals{
		Ticker:        ticker,
		MarketCap:     parseOptionalFundamentalFloat(overview.MarketCapitalization),
		PERatio:       parseOptionalFundamentalFloat(overview.PERatio),
		EPS:           parseOptionalFundamentalFloat(overview.EPS),
		DividendYield: parseOptionalFundamentalFloat(overview.DividendYield),
		FetchedAt:     time.Now().UTC(),
	}

	incomeReports := annualOrQuarterlyIncomeReports(incomeStatement)
	if len(incomeReports) > 0 {
		latestIncomeReport := incomeReports[0]
		fundamentals.Revenue = parseOptionalFundamentalFloat(latestIncomeReport.TotalRevenue)

		grossProfit := parseOptionalFundamentalFloat(latestIncomeReport.GrossProfit)
		if fundamentals.Revenue != 0 {
			fundamentals.GrossMargin = grossProfit / fundamentals.Revenue
		}

		if len(incomeReports) > 1 {
			previousRevenue := parseOptionalFundamentalFloat(incomeReports[1].TotalRevenue)
			if fundamentals.Revenue != 0 && previousRevenue != 0 {
				fundamentals.RevenueGrowthYoY = (fundamentals.Revenue - previousRevenue) / previousRevenue
			}
		}
	}

	if latest, ok := latestBalanceSheetReport(balanceSheet); ok {
		totalLiabilities := parseOptionalFundamentalFloat(latest.TotalLiabilities)
		totalEquity := parseOptionalFundamentalFloat(latest.TotalShareholderEquity)
		if totalEquity != 0 {
			fundamentals.DebtToEquity = totalLiabilities / totalEquity
		}
	}

	return fundamentals, nil
}

// GetNews returns news articles from Alpha Vantage NEWS_SENTIMENT.
func (p *Provider) GetNews(ctx context.Context, ticker string, from, to time.Time) ([]data.NewsArticle, error) {
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

	body, err := p.client.Get(ctx, url.Values{
		"function":  []string{functionNewsSentiment},
		"tickers":   []string{ticker},
		"time_from": []string{formatNewsTimestamp(from.UTC())},
		"time_to":   []string{formatNewsTimestamp(to.UTC())},
		"sort":      []string{"EARLIEST"},
	})
	if err != nil {
		return nil, err
	}

	var response newsResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("alphavantage: decode news response: %w", err)
	}

	articles := make([]data.NewsArticle, 0, len(response.Feed))
	for _, item := range response.Feed {
		publishedAt, err := time.Parse(newsTimestampLayout, strings.TrimSpace(item.TimePublished))
		if err != nil {
			return nil, fmt.Errorf("alphavantage: parse news time_published %q: %w", item.TimePublished, err)
		}

		publishedAt = publishedAt.UTC()
		if publishedAt.Before(from.UTC()) || publishedAt.After(to.UTC()) {
			continue
		}

		articles = append(articles, data.NewsArticle{
			Title:       item.Title,
			Summary:     item.Summary,
			URL:         item.URL,
			Source:      item.Source,
			PublishedAt: publishedAt,
			Sentiment:   newsSentimentForTicker(ticker, item),
		})
	}

	sort.Slice(articles, func(i, j int) bool {
		return articles[i].PublishedAt.Before(articles[j].PublishedAt)
	})

	return articles, nil
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

func (p *Provider) fetchOverview(ctx context.Context, ticker string) (overviewResponse, error) {
	body, err := p.client.Get(ctx, url.Values{
		"function": []string{functionOverview},
		"symbol":   []string{ticker},
	})
	if err != nil {
		return overviewResponse{}, fmt.Errorf("overview request: %w", err)
	}

	var overview overviewResponse
	if err := json.Unmarshal(body, &overview); err != nil {
		return overviewResponse{}, fmt.Errorf("decode overview response: %w", err)
	}

	return overview, nil
}

func (p *Provider) fetchIncomeStatement(ctx context.Context, ticker string) (incomeStatementResponse, error) {
	body, err := p.client.Get(ctx, url.Values{
		"function": []string{functionIncomeStatement},
		"symbol":   []string{ticker},
	})
	if err != nil {
		return incomeStatementResponse{}, fmt.Errorf("income statement request: %w", err)
	}

	var incomeStatement incomeStatementResponse
	if err := json.Unmarshal(body, &incomeStatement); err != nil {
		return incomeStatementResponse{}, fmt.Errorf("decode income statement response: %w", err)
	}

	return incomeStatement, nil
}

func (p *Provider) fetchBalanceSheet(ctx context.Context, ticker string) (balanceSheetResponse, error) {
	body, err := p.client.Get(ctx, url.Values{
		"function": []string{functionBalanceSheet},
		"symbol":   []string{ticker},
	})
	if err != nil {
		return balanceSheetResponse{}, fmt.Errorf("balance sheet request: %w", err)
	}

	var balanceSheet balanceSheetResponse
	if err := json.Unmarshal(body, &balanceSheet); err != nil {
		return balanceSheetResponse{}, fmt.Errorf("decode balance sheet response: %w", err)
	}

	return balanceSheet, nil
}

func annualOrQuarterlyIncomeReports(response incomeStatementResponse) []incomeStatementReport {
	if len(response.AnnualReports) > 0 {
		return response.AnnualReports
	}

	return response.QuarterlyReports
}

func latestBalanceSheetReport(response balanceSheetResponse) (balanceSheetReport, bool) {
	if len(response.AnnualReports) > 0 {
		return response.AnnualReports[0], true
	}
	if len(response.QuarterlyReports) > 0 {
		return response.QuarterlyReports[0], true
	}

	return balanceSheetReport{}, false
}

func parseOptionalFundamentalFloat(value string) float64 {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0
	}

	switch strings.ToUpper(trimmed) {
	case "N/A", "NA", "NONE", "NULL", "-":
		return 0
	}

	parsed, err := strconv.ParseFloat(trimmed, 64)
	if err != nil {
		return 0
	}

	return parsed
}

func formatNewsTimestamp(t time.Time) string {
	return t.UTC().Format(newsTimestampLayout)
}

func newsSentimentForTicker(ticker string, item newsFeedItem) float64 {
	for _, sentiment := range item.TickerSentiment {
		if strings.EqualFold(strings.TrimSpace(sentiment.Ticker), ticker) {
			return float64(sentiment.TickerSentimentScore)
		}
	}

	return float64(item.OverallSentimentScore)
}

func (f *optionalFloat64) UnmarshalJSON(data []byte) error {
	var stringValue string
	if err := json.Unmarshal(data, &stringValue); err == nil {
		*f = optionalFloat64(parseOptionalFundamentalFloat(stringValue))
		return nil
	}

	var numberValue float64
	if err := json.Unmarshal(data, &numberValue); err == nil {
		*f = optionalFloat64(numberValue)
		return nil
	}

	var nilValue any
	if err := json.Unmarshal(data, &nilValue); err == nil && nilValue == nil {
		*f = 0
		return nil
	}

	return fmt.Errorf("invalid float value %s", strings.TrimSpace(string(data)))
}
