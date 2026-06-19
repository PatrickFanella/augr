package kalshi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

var errLiveExecutionDisabled = errors.New("kalshi live order submission is disabled; paper trading only")

// Broker implements execution.Broker for Kalshi live execution when a client is supplied.
type Broker struct {
	client LiveClient
}

// NewBroker constructs a Kalshi broker. A nil client preserves disabled paper-only behavior.
func NewBroker(client LiveClient) *Broker { return &Broker{client: client} }

// SubmitOrder submits through the live client when configured, otherwise rejects live submission.
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil {
		return "", errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("kalshi: submit order: %w", err)
	}
	if b.client == nil {
		return "", errLiveExecutionDisabled
	}
	if order == nil {
		return "", errors.New("kalshi: order is required")
	}
	req, err := mapCreateOrderRequest(order)
	if err != nil {
		return "", err
	}
	resp, err := b.client.CreateOrder(ctx, req)
	if err != nil {
		return "", fmt.Errorf("kalshi: submit order: %w", err)
	}
	orderID := strings.TrimSpace(resp.OrderID)
	if orderID == "" {
		return "", errors.New("kalshi: submit order response missing order id")
	}
	return orderID, nil
}

// CancelOrder cancels through the live client when configured.
func (b *Broker) CancelOrder(ctx context.Context, orderID string) error {
	if b == nil {
		return errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("kalshi: cancel order: %w", err)
	}
	if b.client != nil {
		if err := b.client.CancelOrder(ctx, orderID); err != nil {
			return fmt.Errorf("kalshi: cancel order: %w", err)
		}
		return nil
	}
	return errors.New("kalshi live order cancellation is disabled; paper trading only")
}

// GetOrderStatus reads through the live client when configured.
func (b *Broker) GetOrderStatus(ctx context.Context, orderID string) (domain.OrderStatus, error) {
	if b == nil {
		return "", errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return "", fmt.Errorf("kalshi: get order status: %w", err)
	}
	if b.client != nil {
		resp, err := b.client.GetOrder(ctx, orderID)
		if err != nil {
			return "", fmt.Errorf("kalshi: get order status: %w", err)
		}
		return mapOrderStatus(resp.Status)
	}
	return "", errors.New("kalshi live order status is unavailable; paper trading only")
}

// GetPositions reads positions through the live client when configured.
func (b *Broker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil {
		return nil, errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("kalshi: get positions: %w", err)
	}
	if b.client != nil {
		resp, err := b.client.ListPositions(ctx)
		if err != nil {
			return nil, fmt.Errorf("kalshi: get positions: %w", err)
		}
		positions := make([]domain.Position, 0, len(resp))
		for _, position := range resp {
			positions = append(positions, mapPosition(position))
		}
		return positions, nil
	}
	return nil, errors.New("kalshi live positions are unavailable; paper trading only")
}

// GetAccountBalance reads account balance through the live client when configured.
func (b *Broker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil {
		return execution.Balance{}, errors.New("kalshi: broker is required")
	}
	if err := ctx.Err(); err != nil {
		return execution.Balance{}, fmt.Errorf("kalshi: get account balance: %w", err)
	}
	if b.client != nil {
		resp, err := b.client.GetBalance(ctx)
		if err != nil {
			return execution.Balance{}, fmt.Errorf("kalshi: get account balance: %w", err)
		}
		return execution.Balance{
			Currency:    "USD",
			Cash:        centsToDollars(resp.CashCents),
			BuyingPower: centsToDollars(resp.BuyingPowerCents),
			Equity:      centsToDollars(resp.EquityCents),
		}, nil
	}
	return execution.Balance{}, errors.New("kalshi live account balance is unavailable; paper trading only")
}

func mapPosition(resp PositionResponse) domain.Position {
	position := domain.Position{
		Ticker:   strings.TrimSpace(resp.Ticker),
		Quantity: float64(resp.Count),
	}
	if strings.EqualFold(strings.TrimSpace(resp.Side), "no") {
		position.Side = domain.PositionSideShort
	} else {
		position.Side = domain.PositionSideLong
	}
	if resp.Count > 0 {
		avg := centsToDollars(resp.ValueCents) / float64(resp.Count)
		position.AvgEntry = avg
		position.CurrentPrice = &avg
	}
	return position
}

func centsToDollars(cents int64) float64 {
	return float64(cents) / 100
}
