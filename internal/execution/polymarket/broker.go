package polymarket

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

// OrderTimeInForce represents the time-in-force for a Polymarket order.
type OrderTimeInForce string

const (
	// TimeInForceGTC is a good-til-cancelled order.
	TimeInForceGTC OrderTimeInForce = "GTC"
	// TimeInForceFOK is a fill-or-kill order.
	TimeInForceFOK OrderTimeInForce = "FOK"
	// TimeInForceGTD is a good-til-date order.
	TimeInForceGTD OrderTimeInForce = "GTD"

	defaultTimeInForce = TimeInForceGTC
)

// Broker implements the execution.Broker interface for Polymarket CLOB.
type Broker struct {
	client *Client
}

type submitOrderRequest struct {
	TokenID     string `json:"tokenID"`
	Price       string `json:"price"`
	Size        string `json:"size"`
	Side        string `json:"side"`
	TimeInForce string `json:"timeInForce"`
}

type submitOrderResponse struct {
	OrderID string `json:"orderID"`
	Status  string `json:"status,omitempty"`
}

type orderStatusResponse struct {
	Status string `json:"status"`
}

type positionResponse struct {
	Asset    string `json:"asset"`
	Size     string `json:"size"`
	AvgPrice string `json:"avgPrice"`
	Outcome  string `json:"outcome"`
}

type accountBalanceResponse struct {
	Balance string `json:"balance"`
}

// NewBroker constructs a Polymarket broker adapter.
func NewBroker(client *Client) *Broker {
	return &Broker{client: client}
}

// SubmitOrder sends an order to Polymarket CLOB and returns the external order ID.
// Only limit orders are supported on Polymarket. The Ticker field is used as the
// token ID for the binary outcome market.
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil || b.client == nil {
		return "", errors.New("polymarket: broker client is required")
	}
	if order == nil {
		return "", errors.New("polymarket: order is required")
	}

	request, err := mapSubmitOrderRequest(order)
	if err != nil {
		return "", err
	}

	responseBody, err := b.client.Post(ctx, "/order", request)
	if err != nil {
		return "", fmt.Errorf("polymarket: submit order: %w", err)
	}

	var response submitOrderResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("polymarket: decode submit order response: %w", err)
	}
	if strings.TrimSpace(response.OrderID) == "" {
		return "", errors.New("polymarket: submit order response missing order id")
	}

	return response.OrderID, nil
}

// CancelOrder cancels an existing Polymarket order by external ID.
func (b *Broker) CancelOrder(ctx context.Context, externalID string) error {
	if b == nil || b.client == nil {
		return errors.New("polymarket: broker client is required")
	}

	orderID := strings.TrimSpace(externalID)
	if orderID == "" {
		return errors.New("polymarket: external order id is required")
	}

	if _, err := b.client.Delete(ctx, "/order", map[string]string{
		"orderID": orderID,
	}); err != nil {
		return fmt.Errorf("polymarket: cancel order: %w", err)
	}

	return nil
}

// GetOrderStatus fetches a Polymarket order by external ID and maps its status.
func (b *Broker) GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error) {
	if b == nil || b.client == nil {
		return "", errors.New("polymarket: broker client is required")
	}

	orderID := strings.TrimSpace(externalID)
	if orderID == "" {
		return "", errors.New("polymarket: external order id is required")
	}

	responseBody, err := b.client.Get(ctx, "/order/"+url.PathEscape(orderID), nil)
	if err != nil {
		return "", fmt.Errorf("polymarket: get order status: %w", err)
	}

	var response orderStatusResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("polymarket: decode order status response: %w", err)
	}

	status, err := mapOrderStatus(response.Status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// GetPositions returns current Polymarket positions mapped to domain positions.
// Polymarket positions are binary outcomes (Yes/No) tracked per token.
func (b *Broker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("polymarket: broker client is required")
	}

	responseBody, err := b.client.Get(ctx, "/positions", nil)
	if err != nil {
		return nil, fmt.Errorf("polymarket: get positions: %w", err)
	}

	var response []positionResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("polymarket: decode positions response: %w", err)
	}

	positions := make([]domain.Position, 0, len(response))
	for _, apiPosition := range response {
		position, err := mapPosition(apiPosition)
		if err != nil {
			return nil, err
		}
		positions = append(positions, position)
	}

	return positions, nil
}

// GetAccountBalance returns the Polymarket account balance mapped to the shared balance shape.
func (b *Broker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil || b.client == nil {
		return execution.Balance{}, errors.New("polymarket: broker client is required")
	}

	responseBody, err := b.client.Get(ctx, "/balance", nil)
	if err != nil {
		return execution.Balance{}, fmt.Errorf("polymarket: get account balance: %w", err)
	}

	var response accountBalanceResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return execution.Balance{}, fmt.Errorf("polymarket: decode account balance response: %w", err)
	}

	balance, err := parseRequiredFloat("balance", response.Balance)
	if err != nil {
		return execution.Balance{}, err
	}

	return execution.Balance{
		Currency:    "USDC",
		Cash:        balance,
		BuyingPower: balance,
		Equity:      balance,
	}, nil
}

func mapSubmitOrderRequest(order *domain.Order) (submitOrderRequest, error) {
	tokenID := strings.TrimSpace(order.Ticker)
	if tokenID == "" {
		return submitOrderRequest{}, errors.New("polymarket: order ticker (token ID) is required")
	}

	rawSide := strings.TrimSpace(order.Side.String())
	if rawSide == "" {
		return submitOrderRequest{}, errors.New("polymarket: order side is required")
	}
	side := strings.ToUpper(rawSide)
	switch domain.OrderSide(strings.ToLower(side)) {
	case domain.OrderSideBuy, domain.OrderSideSell:
	default:
		return submitOrderRequest{}, fmt.Errorf("polymarket: unsupported order side %q", order.Side)
	}

	if order.Quantity <= 0 {
		return submitOrderRequest{}, errors.New("polymarket: order quantity must be greater than zero")
	}

	switch order.OrderType {
	case domain.OrderTypeLimit:
		if order.LimitPrice == nil {
			return submitOrderRequest{}, errors.New("polymarket: limit order requires limit price")
		}
		if *order.LimitPrice < 0 || *order.LimitPrice > 1 {
			return submitOrderRequest{}, errors.New("polymarket: limit price must be between 0 and 1")
		}
	default:
		return submitOrderRequest{}, fmt.Errorf("polymarket: unsupported order type %q (only limit orders are supported)", order.OrderType)
	}

	return submitOrderRequest{
		TokenID:     tokenID,
		Price:       formatFloat(*order.LimitPrice),
		Size:        formatFloat(order.Quantity),
		Side:        side,
		TimeInForce: string(defaultTimeInForce),
	}, nil
}

func mapOrderStatus(rawStatus string) (domain.OrderStatus, error) {
	status := strings.ToLower(strings.TrimSpace(rawStatus))
	switch status {
	case "":
		return "", errors.New("polymarket: order status is required")
	case "live", "delayed":
		return domain.OrderStatusSubmitted, nil
	case "matched":
		return domain.OrderStatusPartial, nil
	case "filled":
		return domain.OrderStatusFilled, nil
	case "cancelled":
		return domain.OrderStatusCancelled, nil
	case "rejected":
		return domain.OrderStatusRejected, nil
	default:
		return "", fmt.Errorf("polymarket: unsupported order status %q", rawStatus)
	}
}

func mapPosition(response positionResponse) (domain.Position, error) {
	asset := strings.TrimSpace(response.Asset)
	if asset == "" {
		return domain.Position{}, errors.New("polymarket: position asset is required")
	}

	size, err := parseRequiredFloat("size", response.Size)
	if err != nil {
		return domain.Position{}, err
	}

	avgPrice, err := parseRequiredFloat("avgPrice", response.AvgPrice)
	if err != nil {
		return domain.Position{}, err
	}

	outcome := strings.TrimSpace(response.Outcome)
	ticker := asset
	if outcome != "" {
		ticker = asset + ":" + outcome
	}

	return domain.Position{
		Ticker:   ticker,
		Side:     domain.PositionSideLong,
		Quantity: size,
		AvgEntry: avgPrice,
	}, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func parseRequiredFloat(fieldName, value string) (float64, error) {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return 0, fmt.Errorf("polymarket: %s is required", fieldName)
	}

	parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
	if err != nil {
		return 0, fmt.Errorf("polymarket: parse %s: %w", fieldName, err)
	}

	return parsedValue, nil
}
