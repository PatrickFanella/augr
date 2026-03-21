package domain

import (
	"time"

	"github.com/google/uuid"
)

// OrderSide represents the direction of an order.
type OrderSide string

const (
	OrderSideBuy  OrderSide = "buy"
	OrderSideSell OrderSide = "sell"
)

// String returns the string representation of an OrderSide.
func (s OrderSide) String() string {
	return string(s)
}

// OrderType represents the type of an order.
type OrderType string

const (
	OrderTypeMarket    OrderType = "market"
	OrderTypeLimit     OrderType = "limit"
	OrderTypeStop      OrderType = "stop"
	OrderTypeStopLimit OrderType = "stop_limit"
)

// String returns the string representation of an OrderType.
func (t OrderType) String() string {
	return string(t)
}

// OrderStatus represents the current state of an order.
type OrderStatus string

const (
	OrderStatusPending   OrderStatus = "pending"
	OrderStatusSubmitted OrderStatus = "submitted"
	OrderStatusPartial   OrderStatus = "partial"
	OrderStatusFilled    OrderStatus = "filled"
	OrderStatusCancelled OrderStatus = "cancelled"
	OrderStatusRejected  OrderStatus = "rejected"
)

// String returns the string representation of an OrderStatus.
func (s OrderStatus) String() string {
	return string(s)
}

// Order represents a trading order sent to a broker.
type Order struct {
	ID             uuid.UUID   `json:"id"`
	StrategyID     *uuid.UUID  `json:"strategy_id,omitempty"`
	PipelineRunID  *uuid.UUID  `json:"pipeline_run_id,omitempty"`
	ExternalID     string      `json:"external_id,omitempty"`
	Ticker         string      `json:"ticker"`
	Side           OrderSide   `json:"side"`
	OrderType      OrderType   `json:"order_type"`
	Quantity       float64     `json:"quantity"`
	LimitPrice     *float64    `json:"limit_price,omitempty"`
	StopPrice      *float64    `json:"stop_price,omitempty"`
	FilledQuantity float64     `json:"filled_quantity"`
	FilledAvgPrice *float64    `json:"filled_avg_price,omitempty"`
	Status         OrderStatus `json:"status"`
	Broker         string      `json:"broker"`
	SubmittedAt    *time.Time  `json:"submitted_at,omitempty"`
	FilledAt       *time.Time  `json:"filled_at,omitempty"`
	CreatedAt      time.Time   `json:"created_at"`
}
