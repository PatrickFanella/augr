package kalshi

import "context"

// LiveClient is the narrow Kalshi live-execution boundary used by the broker.
type LiveClient interface {
	CreateOrder(ctx context.Context, req CreateOrderRequest) (CreateOrderResponse, error)
	CancelOrder(ctx context.Context, orderID string) error
	GetOrder(ctx context.Context, orderID string) (OrderResponse, error)
	ListPositions(ctx context.Context) ([]PositionResponse, error)
	GetBalance(ctx context.Context) (BalanceResponse, error)
}

// CreateOrderRequest is the minimal live order payload for later mapping work.
type CreateOrderRequest struct {
	Ticker        string
	Side          string
	Action        string
	Count         int64
	Type          string
	YesPrice      *int64
	NoPrice       *int64
	ClientOrderID string
}

// CreateOrderResponse captures the external order identifier.
type CreateOrderResponse struct {
	OrderID string
}

// OrderResponse captures the minimal live order state needed by the broker.
type OrderResponse struct {
	OrderID string
	Status  string
}

// PositionResponse captures the minimal live position payload.
type PositionResponse struct {
	Ticker     string
	Side       string
	Count      int64
	ValueCents int64
}

// BalanceResponse captures the minimal live account balance payload.
type BalanceResponse struct {
	CashCents        int64
	BuyingPowerCents int64
	EquityCents      int64
}
