package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
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

type orderStatusResponse struct {
	Status string `json:"status"`
}

type positionResponse struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Qty           string `json:"qty"`
	AvgEntryPrice string `json:"avg_entry_price"`
	CurrentPrice  string `json:"current_price"`
	UnrealizedPL  string `json:"unrealized_pl"`
}

type accountResponse struct {
	Currency    string `json:"currency"`
	Cash        string `json:"cash"`
	BuyingPower string `json:"buying_power"`
	Equity      string `json:"equity"`
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

// GetOrderStatus fetches an Alpaca order by external ID and maps its status.
func (b *Broker) GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error) {
	if b == nil || b.client == nil {
		return "", errors.New("alpaca: broker client is required")
	}

	orderID := strings.TrimSpace(externalID)
	if orderID == "" {
		return "", errors.New("alpaca: external order id is required")
	}

	responseBody, err := b.client.Get(ctx, "/v2/orders/"+url.PathEscape(orderID), nil)
	if err != nil {
		return "", fmt.Errorf("alpaca: get order status: %w", err)
	}

	var response orderStatusResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("alpaca: decode order status response: %w", err)
	}

	status, err := mapOrderStatus(response.Status)
	if err != nil {
		return "", err
	}

	return status, nil
}

// GetPositions returns current Alpaca positions mapped to domain positions.
func (b *Broker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("alpaca: broker client is required")
	}

	responseBody, err := b.client.Get(ctx, "/v2/positions", nil)
	if err != nil {
		return nil, fmt.Errorf("alpaca: get positions: %w", err)
	}

	var response []positionResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("alpaca: decode positions response: %w", err)
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

// GetAccountBalance returns the Alpaca account balance mapped to the shared balance shape.
func (b *Broker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil || b.client == nil {
		return execution.Balance{}, errors.New("alpaca: broker client is required")
	}

	responseBody, err := b.client.Get(ctx, "/v2/account", nil)
	if err != nil {
		return execution.Balance{}, fmt.Errorf("alpaca: get account balance: %w", err)
	}

	var response accountResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return execution.Balance{}, fmt.Errorf("alpaca: decode account balance response: %w", err)
	}

	cash, err := parseRequiredFloat("cash", response.Cash)
	if err != nil {
		return execution.Balance{}, err
	}
	buyingPower, err := parseRequiredFloat("buying_power", response.BuyingPower)
	if err != nil {
		return execution.Balance{}, err
	}
	equity, err := parseRequiredFloat("equity", response.Equity)
	if err != nil {
		return execution.Balance{}, err
	}

	return execution.Balance{
		Currency:    strings.TrimSpace(response.Currency),
		Cash:        cash,
		BuyingPower: buyingPower,
		Equity:      equity,
	}, nil
}

func mapSubmitOrderRequest(order *domain.Order) (submitOrderRequest, error) {
	symbol := strings.TrimSpace(order.Ticker)
	if symbol == "" {
		return submitOrderRequest{}, errors.New("alpaca: order ticker is required")
	}
	rawSide := strings.TrimSpace(order.Side.String())
	if rawSide == "" {
		return submitOrderRequest{}, errors.New("alpaca: order side is required")
	}
	side := strings.ToLower(rawSide)
	switch domain.OrderSide(side) {
	case domain.OrderSideBuy, domain.OrderSideSell:
	default:
		return submitOrderRequest{}, fmt.Errorf("alpaca: unsupported order side %q", order.Side)
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
		// Until domain.Order exposes dedicated Alpaca trail fields, trailing stops
		// reuse StopPrice for trail_price and LimitPrice for trail_percent.
		useStopPriceForTrail := order.StopPrice != nil
		useLimitPriceForTrail := order.LimitPrice != nil
		if useStopPriceForTrail == useLimitPriceForTrail {
			return submitOrderRequest{}, errors.New("alpaca: trailing stop order requires either StopPrice (trail_price) or LimitPrice (trail_percent), but not both or neither")
		}
		if useStopPriceForTrail {
			request.TrailPrice = formatFloat(*order.StopPrice)
		} else {
			request.TrailPercent = formatFloat(*order.LimitPrice)
		}
		return request, nil
	default:
		return submitOrderRequest{}, fmt.Errorf("alpaca: unsupported order type %q", order.OrderType)
	}

	return request, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func mapOrderStatus(rawStatus string) (domain.OrderStatus, error) {
	status := strings.ToLower(strings.TrimSpace(rawStatus))
	switch {
	case status == "":
		return "", errors.New("alpaca: order status is required")
	case slices.Contains([]string{"accepted_for_bidding", "calculated", "held", "pending_cancel", "pending_new", "pending_replace"}, status):
		return domain.OrderStatusPending, nil
	case slices.Contains([]string{"accepted", "done_for_day", "new", "replaced", "stopped", "suspended"}, status):
		return domain.OrderStatusSubmitted, nil
	case status == "partially_filled":
		return domain.OrderStatusPartial, nil
	case status == "filled":
		return domain.OrderStatusFilled, nil
	case slices.Contains([]string{"canceled", "cancelled", "expired"}, status):
		return domain.OrderStatusCancelled, nil
	case status == "rejected":
		return domain.OrderStatusRejected, nil
	default:
		return "", fmt.Errorf("alpaca: unsupported order status %q", rawStatus)
	}
}

func mapPosition(response positionResponse) (domain.Position, error) {
	ticker := strings.TrimSpace(response.Symbol)
	if ticker == "" {
		return domain.Position{}, errors.New("alpaca: position symbol is required")
	}

	side, err := mapPositionSide(response.Side)
	if err != nil {
		return domain.Position{}, err
	}

	quantity, err := parseRequiredFloat("qty", response.Qty)
	if err != nil {
		return domain.Position{}, err
	}
	avgEntry, err := parseRequiredFloat("avg_entry_price", response.AvgEntryPrice)
	if err != nil {
		return domain.Position{}, err
	}
	currentPrice, err := parseOptionalFloat("current_price", response.CurrentPrice)
	if err != nil {
		return domain.Position{}, err
	}
	unrealizedPnL, err := parseOptionalFloat("unrealized_pl", response.UnrealizedPL)
	if err != nil {
		return domain.Position{}, err
	}

	return domain.Position{
		Ticker:        ticker,
		Side:          side,
		Quantity:      quantity,
		AvgEntry:      avgEntry,
		CurrentPrice:  currentPrice,
		UnrealizedPnL: unrealizedPnL,
	}, nil
}

func mapPositionSide(rawSide string) (domain.PositionSide, error) {
	switch side := strings.ToLower(strings.TrimSpace(rawSide)); side {
	case string(domain.PositionSideLong):
		return domain.PositionSideLong, nil
	case string(domain.PositionSideShort):
		return domain.PositionSideShort, nil
	default:
		return "", fmt.Errorf("alpaca: unsupported position side %q", rawSide)
	}
}

func parseRequiredFloat(fieldName, value string) (float64, error) {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return 0, fmt.Errorf("alpaca: %s is required", fieldName)
	}

	parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
	if err != nil {
		return 0, fmt.Errorf("alpaca: parse %s: %w", fieldName, err)
	}

	return parsedValue, nil
}

func parseOptionalFloat(fieldName, value string) (*float64, error) {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return nil, nil
	}

	parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
	if err != nil {
		return nil, fmt.Errorf("alpaca: parse %s: %w", fieldName, err)
	}

	return &parsedValue, nil
}
