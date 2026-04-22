package domain

import (
	"time"

	"github.com/google/uuid"
)

// Trade represents an executed fill against an order.
type Trade struct {
	ID         uuid.UUID  `json:"id"`
	OrderID    *uuid.UUID `json:"order_id,omitempty"`
	PositionID *uuid.UUID `json:"position_id,omitempty"`
	ExternalID string     `json:"external_id,omitempty"`
	Ticker     string     `json:"ticker"`
	Side       OrderSide  `json:"side"`
	Quantity   float64    `json:"quantity"`
	Price      float64    `json:"price"`
	Fee        float64    `json:"fee"`
	ExecutedAt time.Time  `json:"executed_at"`
	CreatedAt  time.Time  `json:"created_at"`

	// Options fields (nil/zero for equity trades).
	AssetClass         AssetClass `json:"asset_class,omitempty"`
	OpenClose          string     `json:"open_close,omitempty"` // "open" or "close"
	ContractMultiplier float64    `json:"contract_multiplier,omitempty"`
	Premium            float64    `json:"premium,omitempty"` // price per contract
	ExitReason         string     `json:"exit_reason,omitempty"`
}
