package domain

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

const (
	KalshiDiscoveryStatusRunning   = "running"
	KalshiDiscoveryStatusCompleted = "completed"
	KalshiDiscoveryStatusFailed    = "failed"
)

// KalshiWatchedMarket tracks a Kalshi ticker that should be monitored.
type KalshiWatchedMarket struct {
	Ticker      string     `json:"ticker"`
	EventTicker string     `json:"event_ticker,omitempty"`
	Title       string     `json:"title,omitempty"`
	Category    string     `json:"category,omitempty"`
	Status      string     `json:"status,omitempty"`
	CloseTime   *time.Time `json:"close_time,omitempty"`
	Enabled     bool       `json:"enabled"`
	AddedAt     time.Time  `json:"added_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// KalshiMarketSnapshot captures a point-in-time market view.
type KalshiMarketSnapshot struct {
	ID           uuid.UUID       `json:"id"`
	Ticker       string          `json:"ticker"`
	Title        string          `json:"title,omitempty"`
	Status       string          `json:"status,omitempty"`
	YesBid       float64         `json:"yes_bid"`
	YesAsk       float64         `json:"yes_ask"`
	NoBid        float64         `json:"no_bid"`
	NoAsk        float64         `json:"no_ask"`
	Volume       float64         `json:"volume"`
	OpenInterest float64         `json:"open_interest"`
	CloseTime    *time.Time      `json:"close_time,omitempty"`
	Raw          json.RawMessage `json:"raw,omitempty"`
	CapturedAt   time.Time       `json:"captured_at"`
}

// KalshiDiscoveryResult stores the counters and output metadata for a run.
type KalshiDiscoveryResult struct {
	Fetched  int             `json:"fetched"`
	Screened int             `json:"screened"`
	Proposed int             `json:"proposed"`
	Deployed int             `json:"deployed"`
	Errors   []string        `json:"errors,omitempty"`
	Summary  json.RawMessage `json:"summary,omitempty"`
}

// KalshiDiscoveryRun tracks a discovery execution lifecycle.
type KalshiDiscoveryRun struct {
	ID         uuid.UUID             `json:"id"`
	Status     string                `json:"status"`
	Result     KalshiDiscoveryResult `json:"result"`
	StartedAt  time.Time             `json:"started_at"`
	FinishedAt *time.Time            `json:"finished_at,omitempty"`
	UpdatedAt  time.Time             `json:"updated_at"`
}
