package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

const defaultTimeInForce = "day"

// Broker implements the execution.Broker interface for Alpaca.
type Broker struct {
	client *Client
}

type submitOrderRequest struct {
	Symbol      string `json:"symbol"`
	Qty         string `json:"qty"`
	Side        string `json:"side"`
	Type        string `json:"type"`
	TimeInForce string `json:"time_in_force"`

	LimitPrice   string `json:"limit_price,omitempty"`
	StopPrice    string `json:"stop_price,omitempty"`
	TrailPrice   string `json:"trail_price,omitempty"`
	TrailPercent string `json:"trail_percent,omitempty"`
}

type submitOrderResponse struct {
	ID string `json:"id"`
}

// NewBroker constructs an Alpaca broker adapter.
func NewBroker(client *Client) *Broker {
	return &Broker{client: client}
}

// SubmitOrder sends an order to Alpaca and returns the external order ID.
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil || b.client == nil {
		return "", errors.New("alpaca: broker client is required")
	}
	if order == nil {
		return "", errors.New("alpaca: order is required")
	}

	request, err := mapSubmitOrderRequest(order)
	if err != nil {
		return "", err
	}

	responseBody, err := b.client.Post(ctx, "/v2/orders", request)
	if err != nil {
		return "", fmt.Errorf("alpaca: submit order: %w", err)
	}

	var response submitOrderResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("alpaca: decode submit order response: %w", err)
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", errors.New("alpaca: submit order response missing id")
	}

	return response.ID, nil
}

// CancelOrder cancels an existing Alpaca order by external ID.
func (b *Broker) CancelOrder(ctx context.Context, externalID string) error {
	if b == nil || b.client == nil {
		return errors.New("alpaca: broker client is required")
	}

	orderID := strings.TrimSpace(externalID)
	if orderID == "" {
		return errors.New("alpaca: external order id is required")
	}

	if _, err := b.client.Delete(ctx, "/v2/orders/"+url.PathEscape(orderID), nil); err != nil {
		return fmt.Errorf("alpaca: cancel order: %w", err)
	}

	return nil
}

// GetOrderStatus returns an unsupported error until Alpaca order lookups are implemented.
func (*Broker) GetOrderStatus(context.Context, string) (domain.OrderStatus, error) {
	return "", errors.New("alpaca: get order status not implemented")
}

// GetPositions returns an unsupported error until Alpaca positions are implemented.
func (*Broker) GetPositions(context.Context) ([]domain.Position, error) {
	return nil, errors.New("alpaca: get positions not implemented")
}

// GetAccountBalance returns an unsupported error until Alpaca balances are implemented.
func (*Broker) GetAccountBalance(context.Context) (execution.Balance, error) {
	return execution.Balance{}, errors.New("alpaca: get account balance not implemented")
}

func mapSubmitOrderRequest(order *domain.Order) (submitOrderRequest, error) {
	symbol := strings.TrimSpace(order.Ticker)
	if symbol == "" {
		return submitOrderRequest{}, errors.New("alpaca: order ticker is required")
	}
	side := strings.TrimSpace(order.Side.String())
	if side == "" {
		return submitOrderRequest{}, errors.New("alpaca: order side is required")
	}
	orderType := strings.TrimSpace(order.OrderType.String())
	if orderType == "" {
		return submitOrderRequest{}, errors.New("alpaca: order type is required")
	}
	if order.Quantity <= 0 {
		return submitOrderRequest{}, errors.New("alpaca: order quantity must be greater than zero")
	}

	request := submitOrderRequest{
		Symbol:      symbol,
		Qty:         formatFloat(order.Quantity),
		Side:        side,
		Type:        orderType,
		TimeInForce: defaultTimeInForce,
	}

	switch order.OrderType {
	case domain.OrderTypeMarket:
		return request, nil
	case domain.OrderTypeLimit:
		if order.LimitPrice == nil {
			return submitOrderRequest{}, errors.New("alpaca: limit order requires limit price")
		}
		request.LimitPrice = formatFloat(*order.LimitPrice)
	case domain.OrderTypeStop:
		if order.StopPrice == nil {
			return submitOrderRequest{}, errors.New("alpaca: stop order requires stop price")
		}
		request.StopPrice = formatFloat(*order.StopPrice)
	case domain.OrderTypeStopLimit:
		if order.LimitPrice == nil {
			return submitOrderRequest{}, errors.New("alpaca: stop limit order requires limit price")
		}
		if order.StopPrice == nil {
			return submitOrderRequest{}, errors.New("alpaca: stop limit order requires stop price")
		}
		request.LimitPrice = formatFloat(*order.LimitPrice)
		request.StopPrice = formatFloat(*order.StopPrice)
	case domain.OrderTypeTrailingStop:
		if order.StopPrice != nil && order.LimitPrice != nil {
			return submitOrderRequest{}, errors.New("alpaca: trailing stop order requires exactly one trail value")
		}
		if order.StopPrice != nil {
			request.TrailPrice = formatFloat(*order.StopPrice)
			return request, nil
		}
		if order.LimitPrice != nil {
			request.TrailPercent = formatFloat(*order.LimitPrice)
			return request, nil
		}
		return submitOrderRequest{}, errors.New("alpaca: trailing stop order requires trail price or trail percent")
	default:
		return submitOrderRequest{}, fmt.Errorf("alpaca: unsupported order type %q", order.OrderType)
	}

	return request, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}
