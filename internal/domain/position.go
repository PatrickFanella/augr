package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PositionSide represents whether a position is long or short.
type PositionSide string

const (
	PositionSideLong  PositionSide = "long"
	PositionSideShort PositionSide = "short"
)

// String returns the string representation of a PositionSide.
func (s PositionSide) String() string {
	return string(s)
}

// IsValid returns true if the side is a defined PositionSide constant.
func (s PositionSide) IsValid() bool {
	switch s {
	case PositionSideLong, PositionSideShort:
		return true
	}
	return false
}

// NewPosition creates a Position with basic field validation.
func NewPosition(ticker string, side PositionSide, quantity, avgEntry float64) (*Position, error) {
	if err := requireNonEmpty("ticker", ticker); err != nil {
		return nil, err
	}
	if !side.IsValid() {
		return nil, fmt.Errorf("invalid position side: %q", side)
	}
	if err := requirePositive("quantity", quantity); err != nil {
		return nil, err
	}
	if err := requirePositive("avg_entry", avgEntry); err != nil {
		return nil, err
	}
	return &Position{
		Ticker:   ticker,
		Side:     side,
		Quantity: quantity,
		AvgEntry: avgEntry,
	}, nil
}

// Position represents an open or closed trading position.
type Position struct {
	ID            uuid.UUID    `json:"id"`
	StrategyID    *uuid.UUID   `json:"strategy_id,omitempty"`
	Ticker        string       `json:"ticker"`
	Side          PositionSide `json:"side"`
	Quantity      float64      `json:"quantity"`
	AvgEntry      float64      `json:"avg_entry"`
	CurrentPrice  *float64     `json:"current_price,omitempty"`
	UnrealizedPnL *float64     `json:"unrealized_pnl,omitempty"`
	RealizedPnL   float64      `json:"realized_pnl"`
	StopLoss      *float64     `json:"stop_loss,omitempty"`
	TakeProfit    *float64     `json:"take_profit,omitempty"`
	OpenedAt      time.Time    `json:"opened_at"`
	ClosedAt      *time.Time   `json:"closed_at,omitempty"`
}
