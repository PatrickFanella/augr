package data

import "time"

// Timeframe represents a candlestick bar duration.
type Timeframe string

const (
	// Timeframe1m is a one-minute bar.
	Timeframe1m Timeframe = "1m"
	// Timeframe5m is a five-minute bar.
	Timeframe5m Timeframe = "5m"
	// Timeframe15m is a fifteen-minute bar.
	Timeframe15m Timeframe = "15m"
	// Timeframe1h is a one-hour bar.
	Timeframe1h Timeframe = "1h"
	// Timeframe1d is a one-day bar.
	Timeframe1d Timeframe = "1d"
)

// String returns the string representation of a Timeframe.
func (t Timeframe) String() string {
	return string(t)
}

// Fundamentals holds key financial fundamentals for a ticker.
type Fundamentals struct {
	Ticker           string    `json:"ticker"`
	MarketCap        float64   `json:"market_cap"`
	PERatio          float64   `json:"pe_ratio"`
	EPS              float64   `json:"eps"`
	Revenue          float64   `json:"revenue"`
	RevenueGrowthYoY float64   `json:"revenue_growth_yoy"`
	GrossMargin      float64   `json:"gross_margin"`
	DebtToEquity     float64   `json:"debt_to_equity"`
	FreeCashFlow     float64   `json:"free_cash_flow"`
	DividendYield    float64   `json:"dividend_yield"`
	FetchedAt        time.Time `json:"fetched_at"`
}

// NewsArticle represents a single news item relevant to a ticker.
type NewsArticle struct {
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`
	PublishedAt time.Time `json:"published_at"`
	Sentiment   float64   `json:"sentiment"`
}

// SocialSentiment aggregates social-media sentiment signals for a ticker.
type SocialSentiment struct {
	Ticker       string    `json:"ticker"`
	Score        float64   `json:"score"`
	Bullish      float64   `json:"bullish"`
	Bearish      float64   `json:"bearish"`
	PostCount    int       `json:"post_count"`
	CommentCount int       `json:"comment_count"`
	MeasuredAt   time.Time `json:"measured_at"`
}
