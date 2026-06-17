package kalshi

import (
	"context"
	"errors"
	"fmt"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

var errLiveExecutionDisabled = errors.New("kalshi live order submission is disabled; paper trading only")

// Broker implements execution.Broker for Kalshi phase-1 paper/data runs.
type Broker struct{}

// NewBroker constructs a disabled Kalshi live broker stub.
func NewBroker() *Broker { return &Broker{} }

// SubmitOrder always rejects live order submission for phase 1.
func (b *Broker) SubmitOrder(ctx context.Context, _ *domain.Order) (string, error) {
	if b == nil {
		return "", errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("kalshi: submit order: %w", err)
	}
	return "", errLiveExecutionDisabled
}

// CancelOrder is unsupported for the disabled live broker.
func (b *Broker) CancelOrder(ctx context.Context, _ string) error {
	if b == nil {
		return errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("kalshi: cancel order: %w", err)
	}
	return errors.New("kalshi live order cancellation is disabled; paper trading only")
}

// GetOrderStatus is unsupported for the disabled live broker.
func (b *Broker) GetOrderStatus(ctx context.Context, _ string) (domain.OrderStatus, error) {
	if b == nil {
		return "", errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("kalshi: get order status: %w", err)
	}
	return "", errors.New("kalshi live order status is unavailable; paper trading only")
}

// GetPositions is unsupported for the disabled live broker.
func (b *Broker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil {
		return nil, errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("kalshi: get positions: %w", err)
	}
	return nil, errors.New("kalshi live positions are unavailable; paper trading only")
}

// GetAccountBalance is unsupported for the disabled live broker.
func (b *Broker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil {
		return execution.Balance{}, errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return execution.Balance{}, fmt.Errorf("kalshi: get account balance: %w", err)
	}
	return execution.Balance{}, errors.New("kalshi live account balance is unavailable; paper trading only")
}
