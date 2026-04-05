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
var _ data.EventsProvider = (*Provider)(nil)

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

// ── Events response types ──────────────────────────────────────────────

type earningsCalendarResponse struct {
	EarningsCalendar []earningsEntry `json:"earningsCalendar"`
}

type earningsEntry struct {
	Date            string   `json:"date"`
	EPSActual       *float64 `json:"epsActual"`
	EPSEstimate     *float64 `json:"epsEstimate"`
	Hour            string   `json:"hour"`
	Quarter         int      `json:"quarter"`
	RevenueActual   *float64 `json:"revenueActual"`
	RevenueEstimate *float64 `json:"revenueEstimate"`
	Symbol          string   `json:"symbol"`
	Year            int      `json:"year"`
}

type filingEntry struct {
	AccessNumber string `json:"accessNumber"`
	Symbol       string `json:"symbol"`
	Form         string `json:"form"`
	FiledDate    string `json:"filedDate"`
	AcceptedDate string `json:"acceptedDate"`
	ReportDate   string `json:"reportDate"`
	URL          string `json:"url"`
}

type economicCalendarResponse struct {
	EconomicCalendar []economicEntry `json:"economicCalendar"`
}

type economicEntry struct {
	Actual   *float64 `json:"actual"`
	Country  string   `json:"country"`
	Estimate *float64 `json:"estimate"`
	Event    string   `json:"event"`
	Impact   string   `json:"impact"`
	Prev     *float64 `json:"prev"`
	Time     string   `json:"time"`
	Unit     string   `json:"unit"`
}

type ipoCalendarResponse struct {
	IPOCalendar []ipoEntry `json:"ipoCalendar"`
}

type ipoEntry struct {
	Date             string `json:"date"`
	Exchange         string `json:"exchange"`
	Name             string `json:"name"`
	NumberOfShares   int64  `json:"numberOfShares"`
	Price            string `json:"price"`
	Status           string `json:"status"`
	Symbol           string `json:"symbol"`
	TotalSharesValue int64  `json:"totalSharesValue"`
}

// ── Events methods ─────────────────────────────────────────────────────

// GetEarningsCalendar returns earnings events for the given date range.
func (p *Provider) GetEarningsCalendar(ctx context.Context, from, to time.Time) ([]domain.EarningsEvent, error) {
	if p == nil {
		return nil, errors.New("finnhub: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("finnhub: client is nil")
	}

	params := url.Values{
		"from": []string{from.Format("2006-01-02")},
		"to":   []string{to.Format("2006-01-02")},
	}

	body, err := p.client.Get(ctx, "/calendar/earnings", params)
	if err != nil {
		return nil, fmt.Errorf("finnhub: GetEarningsCalendar: %w", err)
	}

	var resp earningsCalendarResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("finnhub: decode earnings calendar: %w", err)
	}

	events := make([]domain.EarningsEvent, 0, len(resp.EarningsCalendar))
	for _, e := range resp.EarningsCalendar {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		events = append(events, domain.EarningsEvent{
			Symbol:          e.Symbol,
			Date:            d,
			Hour:            e.Hour,
			EPSEstimate:     e.EPSEstimate,
			EPSActual:       e.EPSActual,
			RevenueEstimate: e.RevenueEstimate,
			RevenueActual:   e.RevenueActual,
			Quarter:         e.Quarter,
			Year:            e.Year,
		})
	}

	return events, nil
}

// GetNextEarnings returns the next upcoming earnings event for a ticker.
func (p *Provider) GetNextEarnings(ctx context.Context, ticker string) (*domain.EarningsEvent, error) {
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

	params := url.Values{
		"symbol": []string{ticker},
	}

	body, err := p.client.Get(ctx, "/calendar/earnings", params)
	if err != nil {
		return nil, fmt.Errorf("finnhub: GetNextEarnings: %w", err)
	}

	var resp earningsCalendarResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("finnhub: decode earnings calendar: %w", err)
	}

	today := time.Now().UTC().Truncate(24 * time.Hour)
	var next *domain.EarningsEvent
	for _, e := range resp.EarningsCalendar {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		if d.Before(today) {
			continue
		}
		ev := domain.EarningsEvent{
			Symbol:          e.Symbol,
			Date:            d,
			Hour:            e.Hour,
			EPSEstimate:     e.EPSEstimate,
			EPSActual:       e.EPSActual,
			RevenueEstimate: e.RevenueEstimate,
			RevenueActual:   e.RevenueActual,
			Quarter:         e.Quarter,
			Year:            e.Year,
		}
		if next == nil || d.Before(next.Date) {
			next = &ev
		}
	}

	return next, nil
}

// GetFilings returns SEC filings for a ticker filtered by form type and date range.
func (p *Provider) GetFilings(ctx context.Context, ticker, formType string, from, to time.Time) ([]domain.SECFiling, error) {
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

	params := url.Values{
		"symbol": []string{ticker},
		"from":   []string{from.Format("2006-01-02")},
		"to":     []string{to.Format("2006-01-02")},
	}
	if formType != "" {
		params.Set("form", formType)
	}

	body, err := p.client.Get(ctx, "/stock/filings", params)
	if err != nil {
		return nil, fmt.Errorf("finnhub: GetFilings: %w", err)
	}

	var entries []filingEntry
	if err := json.Unmarshal(body, &entries); err != nil {
		return nil, fmt.Errorf("finnhub: decode filings: %w", err)
	}

	filings := make([]domain.SECFiling, 0, len(entries))
	for _, e := range entries {
		filed, _ := time.Parse("2006-01-02 15:04:05", e.FiledDate)
		accepted, _ := time.Parse("2006-01-02 15:04:05", e.AcceptedDate)
		report, _ := time.Parse("2006-01-02", e.ReportDate)

		filings = append(filings, domain.SECFiling{
			Symbol:       e.Symbol,
			Form:         e.Form,
			FiledDate:    filed,
			AcceptedDate: accepted,
			ReportDate:   report,
			URL:          e.URL,
			AccessNumber: e.AccessNumber,
		})
	}

	return filings, nil
}

// GetEconomicCalendar returns upcoming economic calendar events.
func (p *Provider) GetEconomicCalendar(ctx context.Context) ([]domain.EconomicEvent, error) {
	if p == nil {
		return nil, errors.New("finnhub: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("finnhub: client is nil")
	}

	body, err := p.client.Get(ctx, "/calendar/economic", nil)
	if err != nil {
		return nil, fmt.Errorf("finnhub: GetEconomicCalendar: %w", err)
	}

	var resp economicCalendarResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("finnhub: decode economic calendar: %w", err)
	}

	events := make([]domain.EconomicEvent, 0, len(resp.EconomicCalendar))
	for _, e := range resp.EconomicCalendar {
		t, err := time.Parse(time.RFC3339, e.Time)
		if err != nil {
			t, _ = time.Parse("2006-01-02 15:04:05", e.Time)
		}
		events = append(events, domain.EconomicEvent{
			Event:    e.Event,
			Country:  e.Country,
			Time:     t,
			Impact:   e.Impact,
			Estimate: e.Estimate,
			Actual:   e.Actual,
			Previous: e.Prev,
			Unit:     e.Unit,
		})
	}

	return events, nil
}

// GetIPOCalendar returns IPO events for the given date range.
func (p *Provider) GetIPOCalendar(ctx context.Context, from, to time.Time) ([]domain.IPOEvent, error) {
	if p == nil {
		return nil, errors.New("finnhub: provider is nil")
	}
	if p.client == nil {
		return nil, errors.New("finnhub: client is nil")
	}

	params := url.Values{
		"from": []string{from.Format("2006-01-02")},
		"to":   []string{to.Format("2006-01-02")},
	}

	body, err := p.client.Get(ctx, "/calendar/ipo", params)
	if err != nil {
		return nil, fmt.Errorf("finnhub: GetIPOCalendar: %w", err)
	}

	var resp ipoCalendarResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("finnhub: decode ipo calendar: %w", err)
	}

	events := make([]domain.IPOEvent, 0, len(resp.IPOCalendar))
	for _, e := range resp.IPOCalendar {
		d, err := time.Parse("2006-01-02", e.Date)
		if err != nil {
			continue
		}
		events = append(events, domain.IPOEvent{
			Symbol:        e.Symbol,
			Date:          d,
			Exchange:      e.Exchange,
			Name:          e.Name,
			PriceRange:    e.Price,
			SharesOffered: e.NumberOfShares,
			Status:        e.Status,
		})
	}

	return events, nil
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
