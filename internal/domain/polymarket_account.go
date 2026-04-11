package domain

import "time"

// PolymarketAccount profiles a known Polymarket trader by wallet address.
type PolymarketAccount struct {
	Address                       string         `json:"address"`
	DisplayName                   string         `json:"display_name,omitempty"`
	FirstSeen                     time.Time      `json:"first_seen"`
	LastActive                    *time.Time     `json:"last_active,omitempty"`
	TotalTrades                   int            `json:"total_trades"`
	TotalVolume                   float64        `json:"total_volume"`
	MarketsEntered                int            `json:"markets_entered"`
	MarketsWon                    int            `json:"markets_won"`
	MarketsLost                   int            `json:"markets_lost"`
	WinRate                       float64        `json:"win_rate"`
	CategoryStats                 map[string]any `json:"category_stats,omitempty"`
	AvgPosition                   float64        `json:"avg_position"`
	MaxPosition                   float64        `json:"max_position"`
	AvgEntryHoursBeforeResolution float64        `json:"avg_entry_hours_before_resolution,omitempty"`
	EarlyEntryRate                float64        `json:"early_entry_rate"`
	Tags                          []string       `json:"tags,omitempty"`
	Tracked                       bool           `json:"tracked"`
	UpdatedAt                     time.Time      `json:"updated_at"`
}

// PolymarketAccountTrade records a single trade by a known account.
type PolymarketAccountTrade struct {
	ID             string    `json:"id"`
	AccountAddress string    `json:"account_address"`
	MarketSlug     string    `json:"market_slug"`
	Side           string    `json:"side"`   // "YES" or "NO"
	Action         string    `json:"action"` // "buy" or "sell"
	Price          float64   `json:"price"`
	SizeUSDC       float64   `json:"size_usdc"`
	Timestamp      time.Time `json:"timestamp"`
	Outcome        string    `json:"outcome,omitempty"`
	PnL            *float64  `json:"pnl,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
