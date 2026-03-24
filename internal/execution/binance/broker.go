package binance

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

const (
	defaultTimeInForce   = "GTC"
	orderExternalIDDelim = ":"
)

var preferredBalanceAssets = []string{"USDT", "USDC", "BUSD", "FDUSD", "TUSD", "USD"}

// Broker implements the execution.Broker interface for Binance spot trading.
type Broker struct {
	client *Client
}

type submitOrderResponse struct {
	Symbol  string `json:"symbol"`
	OrderID int64  `json:"orderId"`
}

type orderStatusResponse struct {
	Status string `json:"status"`
}

type accountResponse struct {
	Balances []accountBalance `json:"balances"`
}

type accountBalance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

// NewBroker constructs a Binance broker adapter.
func NewBroker(client *Client) *Broker {
	return &Broker{client: client}
}

// SubmitOrder sends a spot order to Binance and returns a composite external order ID.
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil || b.client == nil {
		return "", errors.New("binance: broker client is required")
	}
	if order == nil {
		return "", errors.New("binance: order is required")
	}

	params, symbol, err := mapSubmitOrderParams(order)
	if err != nil {
		return "", err
	}

	responseBody, err := b.client.SignedPost(ctx, "/api/v3/order", params)
	if err != nil {
		return "", fmt.Errorf("binance: submit order: %w", err)
	}

	var response submitOrderResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("binance: decode submit order response: %w", err)
	}
	if response.OrderID <= 0 {
		return "", errors.New("binance: submit order response missing order id")
	}
	if responseSymbol := normalizeSymbol(response.Symbol); responseSymbol != "" {
		symbol = responseSymbol
	}

	return formatExternalOrderID(symbol, response.OrderID), nil
}

// CancelOrder cancels an existing Binance order by composite external ID.
func (b *Broker) CancelOrder(ctx context.Context, externalID string) error {
	if b == nil || b.client == nil {
		return errors.New("binance: broker client is required")
	}

	symbol, orderID, err := parseExternalOrderID(externalID)
	if err != nil {
		return err
	}

	if _, err := b.client.SignedDelete(ctx, "/api/v3/order", url.Values{
		"symbol":  []string{symbol},
		"orderId": []string{strconv.FormatInt(orderID, 10)},
	}); err != nil {
		return fmt.Errorf("binance: cancel order: %w", err)
	}

	return nil
}

// GetOrderStatus fetches a Binance order by composite external ID and maps its status.
func (b *Broker) GetOrderStatus(ctx context.Context, externalID string) (domain.OrderStatus, error) {
	if b == nil || b.client == nil {
		return "", errors.New("binance: broker client is required")
	}

	symbol, orderID, err := parseExternalOrderID(externalID)
	if err != nil {
		return "", err
	}

	responseBody, err := b.client.SignedGet(ctx, "/api/v3/order", url.Values{
		"symbol":  []string{symbol},
		"orderId": []string{strconv.FormatInt(orderID, 10)},
	})
	if err != nil {
		return "", fmt.Errorf("binance: get order status: %w", err)
	}

	var response orderStatusResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", fmt.Errorf("binance: decode order status response: %w", err)
	}

	return mapOrderStatus(response.Status)
}

// GetPositions returns current Binance spot balances mapped to long positions.
func (b *Broker) GetPositions(ctx context.Context) ([]domain.Position, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("binance: broker client is required")
	}

	account, err := b.getAccount(ctx)
	if err != nil {
		return nil, err
	}

	positions := make([]domain.Position, 0, len(account.Balances))
	for _, balance := range account.Balances {
		position, ok, err := mapPosition(balance)
		if err != nil {
			return nil, err
		}
		if ok {
			positions = append(positions, position)
		}
	}

	return positions, nil
}

// GetAccountBalance returns the preferred cash-equivalent balance from a Binance spot account.
func (b *Broker) GetAccountBalance(ctx context.Context) (execution.Balance, error) {
	if b == nil || b.client == nil {
		return execution.Balance{}, errors.New("binance: broker client is required")
	}

	account, err := b.getAccount(ctx)
	if err != nil {
		return execution.Balance{}, err
	}

	selectedBalance, err := selectAccountBalance(account.Balances)
	if err != nil {
		return execution.Balance{}, err
	}

	free, locked, err := parseBalanceAmounts(selectedBalance)
	if err != nil {
		return execution.Balance{}, err
	}

	return execution.Balance{
		Currency:    normalizeAsset(selectedBalance.Asset),
		Cash:        free,
		BuyingPower: free,
		Equity:      free + locked,
	}, nil
}

func (b *Broker) getAccount(ctx context.Context) (accountResponse, error) {
	responseBody, err := b.client.SignedGet(ctx, "/api/v3/account", nil)
	if err != nil {
		return accountResponse{}, fmt.Errorf("binance: get account: %w", err)
	}

	var response accountResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return accountResponse{}, fmt.Errorf("binance: decode account response: %w", err)
	}

	return response, nil
}

func mapSubmitOrderParams(order *domain.Order) (url.Values, string, error) {
	symbol := normalizeSymbol(order.Ticker)
	if symbol == "" {
		return nil, "", errors.New("binance: order ticker is required")
	}

	rawSide := strings.TrimSpace(order.Side.String())
	if rawSide == "" {
		return nil, "", errors.New("binance: order side is required")
	}

	side := strings.ToUpper(rawSide)
	switch domain.OrderSide(strings.ToLower(side)) {
	case domain.OrderSideBuy, domain.OrderSideSell:
	default:
		return nil, "", fmt.Errorf("binance: unsupported order side %q", order.Side)
	}

	if order.Quantity <= 0 {
		return nil, "", errors.New("binance: order quantity must be greater than zero")
	}

	params := url.Values{
		"symbol":   []string{symbol},
		"side":     []string{side},
		"quantity": []string{formatFloat(order.Quantity)},
	}

	switch order.OrderType {
	case domain.OrderTypeMarket:
		params.Set("type", "MARKET")
	case domain.OrderTypeLimit:
		if order.LimitPrice == nil {
			return nil, "", errors.New("binance: limit order requires limit price")
		}
		if *order.LimitPrice <= 0 {
			return nil, "", errors.New("binance: limit price must be greater than zero")
		}

		params.Set("type", "LIMIT")
		params.Set("price", formatFloat(*order.LimitPrice))
		params.Set("timeInForce", defaultTimeInForce)
	default:
		return nil, "", fmt.Errorf("binance: unsupported order type %q", order.OrderType)
	}

	return params, symbol, nil
}

func formatExternalOrderID(symbol string, orderID int64) string {
	return symbol + orderExternalIDDelim + strconv.FormatInt(orderID, 10)
}

func parseExternalOrderID(externalID string) (string, int64, error) {
	trimmedID := strings.TrimSpace(externalID)
	if trimmedID == "" {
		return "", 0, errors.New("binance: external order id is required")
	}

	parts := strings.Split(trimmedID, orderExternalIDDelim)
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("binance: external order id %q must be in SYMBOL:ORDER_ID format", externalID)
	}

	symbol := normalizeSymbol(parts[0])
	if symbol == "" {
		return "", 0, fmt.Errorf("binance: external order id %q must include a symbol", externalID)
	}

	orderID, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil || orderID <= 0 {
		return "", 0, fmt.Errorf("binance: external order id %q must include a valid numeric order id", externalID)
	}

	return symbol, orderID, nil
}

func mapOrderStatus(rawStatus string) (domain.OrderStatus, error) {
	switch status := strings.ToUpper(strings.TrimSpace(rawStatus)); status {
	case "":
		return "", errors.New("binance: order status is required")
	case "PENDING_NEW", "PENDING_CANCEL":
		return domain.OrderStatusPending, nil
	case "NEW":
		return domain.OrderStatusSubmitted, nil
	case "PARTIALLY_FILLED":
		return domain.OrderStatusPartial, nil
	case "FILLED":
		return domain.OrderStatusFilled, nil
	case "CANCELED", "EXPIRED", "EXPIRED_IN_MATCH":
		return domain.OrderStatusCancelled, nil
	case "REJECTED":
		return domain.OrderStatusRejected, nil
	default:
		return "", fmt.Errorf("binance: unsupported order status %q", rawStatus)
	}
}

func mapPosition(balance accountBalance) (domain.Position, bool, error) {
	asset := normalizeAsset(balance.Asset)
	if asset == "" {
		return domain.Position{}, false, errors.New("binance: balance asset is required")
	}

	free, locked, err := parseBalanceAmounts(balance)
	if err != nil {
		return domain.Position{}, false, err
	}

	quantity := free + locked
	if quantity == 0 {
		return domain.Position{}, false, nil
	}

	return domain.Position{
		Ticker:   asset,
		Side:     domain.PositionSideLong,
		Quantity: quantity,
		AvgEntry: 0,
	}, true, nil
}

func selectAccountBalance(balances []accountBalance) (accountBalance, error) {
	if len(balances) == 0 {
		return accountBalance{}, errors.New("binance: account balances are required")
	}

	normalized := make([]accountBalance, 0, len(balances))
	for _, balance := range balances {
		asset := normalizeAsset(balance.Asset)
		if asset == "" {
			return accountBalance{}, errors.New("binance: balance asset is required")
		}
		normalized = append(normalized, balance)
	}

	for _, asset := range preferredBalanceAssets {
		for _, balance := range normalized {
			if normalizeAsset(balance.Asset) == asset {
				free, locked, err := parseBalanceAmounts(balance)
				if err != nil {
					return accountBalance{}, err
				}
				if free+locked > 0 {
					return balance, nil
				}
			}
		}
	}

	for _, balance := range normalized {
		free, locked, err := parseBalanceAmounts(balance)
		if err != nil {
			return accountBalance{}, err
		}
		if free+locked > 0 {
			return balance, nil
		}
	}

	return normalized[0], nil
}

func parseBalanceAmounts(balance accountBalance) (float64, float64, error) {
	free, err := parseRequiredFloat("free", balance.Free)
	if err != nil {
		return 0, 0, err
	}
	locked, err := parseRequiredFloat("locked", balance.Locked)
	if err != nil {
		return 0, 0, err
	}

	return free, locked, nil
}

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func parseRequiredFloat(fieldName, value string) (float64, error) {
	trimmedValue := strings.TrimSpace(value)
	if trimmedValue == "" {
		return 0, fmt.Errorf("binance: %s is required", fieldName)
	}

	parsedValue, err := strconv.ParseFloat(trimmedValue, 64)
	if err != nil {
		return 0, fmt.Errorf("binance: parse %s: %w", fieldName, err)
	}

	return parsedValue, nil
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}

func normalizeAsset(asset string) string {
	return strings.ToUpper(strings.TrimSpace(asset))
}
