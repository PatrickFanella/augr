package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OHLCV represents a single candlestick bar of market data.
type OHLCV struct {
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

// HistoricalOHLCV represents a persisted OHLCV bar keyed by ticker and time.
type HistoricalOHLCV struct {
	Ticker    string    `json:"ticker"`
	Provider  string    `json:"provider"`
	Timeframe string    `json:"timeframe"`
	Timestamp time.Time `json:"timestamp"`
	Open      float64   `json:"open"`
	High      float64   `json:"high"`
	Low       float64   `json:"low"`
	Close     float64   `json:"close"`
	Volume    float64   `json:"volume"`
}

// HistoricalOHLCVCoverage tracks fetched date ranges so incremental syncs can
// skip windows that were already downloaded, even when no bars existed.
type HistoricalOHLCVCoverage struct {
	Ticker    string    `json:"ticker"`
	Provider  string    `json:"provider"`
	Timeframe string    `json:"timeframe"`
	DateFrom  time.Time `json:"date_from"`
	DateTo    time.Time `json:"date_to"`
	FetchedAt time.Time `json:"fetched_at"`
}

// Indicator represents a computed technical indicator value.
type Indicator struct {
	Name      string    `json:"name"`
	Value     float64   `json:"value"`
	Timestamp time.Time `json:"timestamp"`
}

// MarketData represents a cached bundle of market data for a ticker.
type MarketData struct {
	ID        uuid.UUID       `json:"id"`
	Ticker    string          `json:"ticker"`
	Provider  string          `json:"provider"`
	DataType  string          `json:"data_type"`
	Timeframe string          `json:"timeframe,omitempty"`
	DateFrom  *time.Time      `json:"date_from,omitempty"`
	DateTo    *time.Time      `json:"date_to,omitempty"`
	Data      json.RawMessage `json:"data"`
	FetchedAt time.Time       `json:"fetched_at"`
	ExpiresAt time.Time       `json:"expires_at"`
}
