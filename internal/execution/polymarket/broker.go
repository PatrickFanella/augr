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

const defaultTimeInForce = "TIME_IN_FORCE_GOOD_TILL_CANCEL"

// Broker implements the execution.Broker interface for Polymarket retail APIs.
type Broker struct {
	client *Client
}

type amount struct {
	Value    string `json:"value"`
	Currency string `json:"currency"`
}

type createOrderRequest struct {
	MarketSlug string  `json:"marketSlug"`
	Type       string  `json:"type"`
	Price      *amount `json:"price,omitempty"`
	Quantity   float64 `json:"quantity,omitempty"`
	TIF        string  `json:"tif,omitempty"`
	Intent     string  `json:"intent"`
}

type createOrderResponse struct {
	ID string `json:"id"`
}

type getOrderResponse struct {
	Order retailOrder `json:"order"`
}

type retailOrder struct {
	ID             string  `json:"id"`
	MarketSlug     string  `json:"marketSlug"`
	State          string  `json:"state"`
	Intent         string  `json:"intent"`
	Quantity       float64 `json:"quantity"`
	CumQuantity    float64 `json:"cumQuantity"`
	LeavesQuantity float64 `json:"leavesQuantity"`
	Price          *amount `json:"price,omitempty"`
	AvgPx          *amount `json:"avgPx,omitempty"`
	MarketMetadata *struct {
		Slug    string `json:"slug"`
		Outcome string `json:"outcome"`
	} `json:"marketMetadata,omitempty"`
}

type openOrdersResponse struct {
	Orders []retailOrder `json:"orders"`
}

type accountBalancesResponse struct {
	Balances []struct {
		CurrentBalance float64 `json:"currentBalance"`
		Currency       string  `json:"currency"`
		BuyingPower    float64 `json:"buyingPower"`
		AssetNotional  float64 `json:"assetNotional"`
	} `json:"balances"`
}

type userPositionsResponse struct {
	Positions map[string]userPosition `json:"positions"`
}

type userPosition struct {
	NetPosition    string  `json:"netPosition"`
	QtyAvailable   string  `json:"qtyAvailable"`
	Cost           *amount `json:"cost,omitempty"`
	MarketMetadata *struct {
		Slug    string `json:"slug"`
		Outcome string `json:"outcome"`
		Title   string `json:"title"`
	} `json:"marketMetadata,omitempty"`
}

// NewBroker constructs a Polymarket broker adapter.
func NewBroker(client *Client) *Broker {
	return &Broker{client: client}
}

// SubmitOrder sends an order to Polymarket retail APIs and returns the external order ID.
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil || b.client == nil {
		return "", errors.New("polymarket: broker client is required")
	}
	if order == nil {
		return "", errors.New("polymarket: order is required")
	}

	request, err := mapCreateOrderRequest(order)
	if err != nil {
		return "", err
	}

	responseBody, err := b.client.Post(ctx, "/v1/orders", request)
	if err != nil {
		return "", fmt.Errorf("polymarket: submit order: %w", err)
	}

	var response createOrderResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("polymarket: decode submit order response: %w", err)
	}
	if strings.TrimSpace(response.ID) == "" {
		return "", errors.New("polymarket: submit order response missing order id")
	}

	order.PolymarketIntent = request.Intent
	return response.ID, nil
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

	if _, err := b.client.Post(ctx, "/v1/order/"+url.PathEscape(orderID)+"/cancel", map[string]string{}); err != nil {
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

	responseBody, err := b.client.Get(ctx, "/v1/order/"+url.PathEscape(orderID), nil)
	if err != nil {
		return "", fmt.Errorf("polymarket: get order status: %w", err)
	}

	var response getOrderResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("polymarket: decode order status response: %w", err)
	}

	status, err := mapOrderStatus(response.Order.State)
	if err != nil {
		return "", err
	}

	return status, nil
}

// GetPositions returns current Polymarket positions mapped to domain positions.
func (b *Broker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("polymarket: broker client is required")
	}

	responseBody, err := b.client.Get(ctx, "/v1/portfolio/positions", nil)
	if err != nil {
		return nil, fmt.Errorf("polymarket: get positions: %w", err)
	}

	var response userPositionsResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("polymarket: decode positions response: %w", err)
	}

	positions := make([]domain.Position, 0, len(response.Positions))
	for slug, apiPosition := range response.Positions {
		position, err := mapPosition(slug, apiPosition)
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

	responseBody, err := b.client.Get(ctx, "/v1/account/balances", nil)
	if err != nil {
		return execution.Balance{}, fmt.Errorf("polymarket: get account balance: %w", err)
	}

	var response accountBalancesResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return execution.Balance{}, fmt.Errorf("polymarket: decode account balance response: %w", err)
	}
	if len(response.Balances) == 0 {
		return execution.Balance{}, errors.New("polymarket: account balance response missing balances")
	}

	balance := response.Balances[0]
	return execution.Balance{
		Currency:    strings.TrimSpace(balance.Currency),
		Cash:        balance.CurrentBalance,
		BuyingPower: balance.BuyingPower,
		Equity:      balance.CurrentBalance + balance.AssetNotional,
	}, nil
}

func mapCreateOrderRequest(order *domain.Order) (createOrderRequest, error) {
	marketSlug := strings.TrimSpace(order.Ticker)
	if marketSlug == "" {
		return createOrderRequest{}, errors.New("polymarket: order ticker (market slug) is required")
	}
	if order.Quantity <= 0 {
		return createOrderRequest{}, errors.New("polymarket: order quantity must be greater than zero")
	}

	intent, err := resolveOrderIntent(order)
	if err != nil {
		return createOrderRequest{}, err
	}

	request := createOrderRequest{
		MarketSlug: marketSlug,
		Intent:     intent,
	}

	switch order.OrderType {
	case domain.OrderTypeLimit:
		if order.LimitPrice == nil {
			return createOrderRequest{}, errors.New("polymarket: limit order requires limit price")
		}
		if *order.LimitPrice < 0 || *order.LimitPrice > 1 {
			return createOrderRequest{}, errors.New("polymarket: limit price must be between 0 and 1")
		}
		request.Type = "ORDER_TYPE_LIMIT"
		request.Price = &amount{Value: formatFloat(*order.LimitPrice), Currency: "USD"}
		request.Quantity = order.Quantity
		request.TIF = defaultTimeInForce
	case domain.OrderTypeMarket:
		request.Type = "ORDER_TYPE_MARKET"
		request.Quantity = order.Quantity
	default:
		return createOrderRequest{}, fmt.Errorf("polymarket: unsupported order type %q", order.OrderType)
	}

	return request, nil
}

func resolveOrderIntent(order *domain.Order) (string, error) {
	if order == nil {
		return "", errors.New("polymarket: order is required")
	}
	if strings.TrimSpace(order.PolymarketIntent) != "" {
		return strings.TrimSpace(order.PolymarketIntent), nil
	}

	side := strings.ToLower(strings.TrimSpace(order.Side.String()))
	predictionSide := strings.ToUpper(strings.TrimSpace(order.PredictionSide))
	if predictionSide == "" {
		return "", errors.New("polymarket: prediction side is required")
	}

	switch predictionSide {
	case "YES":
		switch side {
		case string(domain.OrderSideBuy):
			return "ORDER_INTENT_BUY_LONG", nil
		case string(domain.OrderSideSell):
			return "ORDER_INTENT_SELL_LONG", nil
		}
	case "NO":
		switch side {
		case string(domain.OrderSideBuy):
			return "ORDER_INTENT_BUY_SHORT", nil
		case string(domain.OrderSideSell):
			return "ORDER_INTENT_SELL_SHORT", nil
		}
	default:
		return "", fmt.Errorf("polymarket: unsupported prediction side %q", order.PredictionSide)
	}

	return "", fmt.Errorf("polymarket: unsupported order side %q", order.Side)
}

func mapOrderStatus(rawStatus string) (domain.OrderStatus, error) {
	status := strings.TrimSpace(rawStatus)
	switch status {
	case "":
		return "", errors.New("polymarket: order status is required")
	case "ORDER_STATE_PENDING_NEW", "ORDER_STATE_PENDING_REPLACE", "ORDER_STATE_PENDING_CANCEL", "ORDER_STATE_PENDING_RISK":
		return domain.OrderStatusSubmitted, nil
	case "ORDER_STATE_PARTIALLY_FILLED":
		return domain.OrderStatusPartial, nil
	case "ORDER_STATE_FILLED":
		return domain.OrderStatusFilled, nil
	case "ORDER_STATE_CANCELED", "ORDER_STATE_EXPIRED":
		return domain.OrderStatusCancelled, nil
	case "ORDER_STATE_REJECTED":
		return domain.OrderStatusRejected, nil
	default:
		return "", fmt.Errorf("polymarket: unsupported order status %q", rawStatus)
	}
}

func mapPosition(slug string, response userPosition) (domain.Position, error) {
	ticker := strings.TrimSpace(slug)
	if ticker == "" && response.MarketMetadata != nil {
		ticker = strings.TrimSpace(response.MarketMetadata.Slug)
	}
	if ticker == "" {
		return domain.Position{}, errors.New("polymarket: position market slug is required")
	}

	quantityString := strings.TrimSpace(response.QtyAvailable)
	if quantityString == "" {
		quantityString = strings.TrimSpace(response.NetPosition)
	}
	quantity, err := parseRequiredFloat("netPosition", quantityString)
	if err != nil {
		return domain.Position{}, err
	}
	if quantity < 0 {
		quantity = -quantity
	}

	avgPrice := 0.0
	if response.Cost != nil {
		avgPrice, err = parseRequiredFloat("cost", response.Cost.Value)
		if err != nil {
			return domain.Position{}, err
		}
	}
	if avgPrice <= 0 {
		avgPrice = 0.5
	}

	positionSide := domain.PositionSideLong
	if strings.TrimSpace(response.NetPosition) != "" {
		netPosition, err := parseRequiredFloat("netPosition", response.NetPosition)
		if err == nil && netPosition < 0 {
			positionSide = domain.PositionSideShort
		}
	}

	return domain.Position{
		Ticker:   ticker,
		Side:     positionSide,
		Quantity: quantity,
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
