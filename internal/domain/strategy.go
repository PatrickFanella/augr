package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// MarketType represents the type of market a strategy operates in.
type MarketType string

const (
	MarketTypeStock      MarketType = "stock"
	MarketTypeCrypto     MarketType = "crypto"
	MarketTypePolymarket MarketType = "polymarket"
)

// String returns the string representation of a MarketType.
func (m MarketType) String() string {
	return string(m)
}

// Normalize returns the market type in lowercase with surrounding whitespace removed.
func (m MarketType) Normalize() MarketType {
	return MarketType(strings.ToLower(strings.TrimSpace(string(m))))
}

// IsValid returns true if the market type is a defined MarketType constant.
func (m MarketType) IsValid() bool {
	switch m {
	case MarketTypeStock, MarketTypeCrypto, MarketTypePolymarket:
		return true
	}
	return false
}

// Validate checks that the strategy has valid required fields.
func (s *Strategy) Validate() error {
	if err := requireNonEmpty("name", s.Name); err != nil {
		return err
	}
	if err := requireNonEmpty("ticker", s.Ticker); err != nil {
		return err
	}
	if !s.MarketType.IsValid() {
		return fmt.Errorf("invalid market type: %q", s.MarketType)
	}
	return nil
}

// StrategyConfig holds strategy-specific parameters stored as flexible JSON.
type StrategyConfig = json.RawMessage

// Strategy represents a trading strategy configuration.
type Strategy struct {
	ID           uuid.UUID      `json:"id"`
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Ticker       string         `json:"ticker"`
	MarketType   MarketType     `json:"market_type"`
	ScheduleCron string         `json:"schedule_cron,omitempty"`
	Config       StrategyConfig `json:"config"`
	IsActive     bool           `json:"is_active"`
	IsPaper      bool           `json:"is_paper"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
