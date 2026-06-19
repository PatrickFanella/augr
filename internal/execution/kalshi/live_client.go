package kalshi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
)

// signedClient is the minimal authenticated Kalshi transport used by the live adapter.
type signedClient interface {
	Get(ctx context.Context, path string, query url.Values, authenticated bool) ([]byte, error)
	Post(ctx context.Context, path string, body any) ([]byte, error)
	Delete(ctx context.Context, path string, query url.Values) ([]byte, error)
}

// HTTPClient adapts the signed Kalshi transport to the execution LiveClient boundary.
type HTTPClient struct {
	client signedClient
}

// NewLiveHTTPClient constructs a live execution adapter backed by the signed Kalshi HTTP client.
func NewLiveHTTPClient(client signedClient) (*HTTPClient, error) {
	if client == nil {
		return nil, errors.New("kalshi: live client is required")
	}
	return &HTTPClient{client: client}, nil
}

func (c *HTTPClient) CreateOrder(ctx context.Context, req CreateOrderRequest) (CreateOrderResponse, error) {
	if c == nil || c.client == nil {
		return CreateOrderResponse{}, errors.New("kalshi: live client is required")
	}
	payload, err := buildCreateOrderPayload(req)
	if err != nil {
		return CreateOrderResponse{}, err
	}

	body, err := c.client.Post(ctx, "/portfolio/events/orders", payload)
	if err != nil {
		return CreateOrderResponse{}, fmt.Errorf("kalshi: create order: %w", err)
	}

	var resp createOrderEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		return CreateOrderResponse{}, fmt.Errorf("kalshi: decode create order response: %w", err)
	}
	orderID := firstNonEmpty(strings.TrimSpace(resp.Order.OrderID), strings.TrimSpace(resp.OrderID))
	if orderID == "" {
		return CreateOrderResponse{}, errors.New("kalshi: create order response missing order id")
	}
	return CreateOrderResponse{OrderID: orderID}, nil
}

func (c *HTTPClient) CancelOrder(ctx context.Context, orderID string) error {
	if c == nil || c.client == nil {
		return errors.New("kalshi: live client is required")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return errors.New("kalshi: order id is required")
	}
	if _, err := c.client.Delete(ctx, "/portfolio/events/orders/"+url.PathEscape(orderID), nil); err != nil {
		return fmt.Errorf("kalshi: cancel order: %w", err)
	}
	return nil
}

func (c *HTTPClient) GetOrder(ctx context.Context, orderID string) (OrderResponse, error) {
	if c == nil || c.client == nil {
		return OrderResponse{}, errors.New("kalshi: live client is required")
	}
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return OrderResponse{}, errors.New("kalshi: order id is required")
	}

	body, err := c.client.Get(ctx, "/portfolio/orders/"+url.PathEscape(orderID), nil, true)
	if err != nil {
		return OrderResponse{}, fmt.Errorf("kalshi: get order: %w", err)
	}

	var resp getOrderEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		return OrderResponse{}, fmt.Errorf("kalshi: decode get order response: %w", err)
	}
	order := resp.Order
	status := strings.TrimSpace(order.Status)
	if status == "" {
		remaining, parseErr := parseFixedFloat(order.RemainingCountFP)
		if parseErr == nil && remaining == 0 {
			status = "executed"
		} else {
			return OrderResponse{}, errors.New("kalshi: order status is required")
		}
	}
	if strings.TrimSpace(order.OrderID) == "" {
		order.OrderID = orderID
	}
	return OrderResponse{OrderID: strings.TrimSpace(order.OrderID), Status: status}, nil
}

func (c *HTTPClient) ListPositions(ctx context.Context) ([]PositionResponse, error) {
	if c == nil || c.client == nil {
		return nil, errors.New("kalshi: live client is required")
	}
	var positions []PositionResponse
	cursor := ""
	for {
		query := url.Values{}
		query.Set("limit", "1000")
		query.Set("count_filter", "position")
		if cursor != "" {
			query.Set("cursor", cursor)
		}

		body, err := c.client.Get(ctx, "/portfolio/positions", query, true)
		if err != nil {
			return nil, fmt.Errorf("kalshi: list positions: %w", err)
		}

		var resp listPositionsEnvelope
		if err := json.Unmarshal(body, &resp); err != nil {
			return nil, fmt.Errorf("kalshi: decode positions response: %w", err)
		}

		for _, marketPosition := range resp.MarketPositions {
			position, err := mapMarketPosition(marketPosition)
			if err != nil {
				return nil, err
			}
			positions = append(positions, position)
		}

		nextCursor := strings.TrimSpace(resp.NextCursor)
		if nextCursor == "" {
			nextCursor = strings.TrimSpace(resp.Cursor)
		}
		if nextCursor == "" || nextCursor == cursor {
			return positions, nil
		}
		cursor = nextCursor
	}
}

func (c *HTTPClient) GetBalance(ctx context.Context) (BalanceResponse, error) {
	if c == nil || c.client == nil {
		return BalanceResponse{}, errors.New("kalshi: live client is required")
	}
	body, err := c.client.Get(ctx, "/portfolio/balance", nil, true)
	if err != nil {
		return BalanceResponse{}, fmt.Errorf("kalshi: get balance: %w", err)
	}

	var resp balanceEnvelope
	if err := json.Unmarshal(body, &resp); err != nil {
		return BalanceResponse{}, fmt.Errorf("kalshi: decode balance response: %w", err)
	}

	return BalanceResponse{
		CashCents:        resp.Balance,
		BuyingPowerCents: resp.Balance,
		EquityCents:      resp.PortfolioValue,
	}, nil
}

type createOrderEnvelope struct {
	OrderID string `json:"order_id"`
	Order   struct {
		OrderID string `json:"order_id"`
	} `json:"order"`
}

type getOrderEnvelope struct {
	Order struct {
		OrderID          string `json:"order_id"`
		Status           string `json:"status"`
		RemainingCountFP string `json:"remaining_count_fp"`
	} `json:"order"`
}

type listPositionsEnvelope struct {
	MarketPositions []marketPosition `json:"market_positions"`
	Cursor          string           `json:"cursor"`
	NextCursor      string           `json:"next_cursor"`
}

type marketPosition struct {
	Ticker                string `json:"ticker"`
	PositionFP            string `json:"position_fp"`
	MarketExposureDollars string `json:"market_exposure_dollars"`
}

type balanceEnvelope struct {
	Balance        int64 `json:"balance"`
	PortfolioValue int64 `json:"portfolio_value"`
}

func buildCreateOrderPayload(req CreateOrderRequest) (map[string]any, error) {
	if strings.TrimSpace(req.Ticker) == "" {
		return nil, errors.New("kalshi: ticker is required")
	}
	if req.Count <= 0 {
		return nil, errors.New("kalshi: count must be positive")
	}

	apiSide, err := mapOrderSide(req.Side, req.Action)
	if err != nil {
		return nil, err
	}

	priceCents, err := mapOrderPriceCents(req)
	if err != nil {
		return nil, err
	}

	payload := map[string]any{
		"ticker":                     strings.TrimSpace(req.Ticker),
		"side":                       apiSide,
		"count":                      fmt.Sprintf("%d.00", req.Count),
		"price":                      formatCents(priceCents),
		"time_in_force":              timeInForceForOrder(req.Type),
		"self_trade_prevention_type": "taker_at_cross",
	}
	if strings.TrimSpace(req.ClientOrderID) != "" {
		payload["client_order_id"] = strings.TrimSpace(req.ClientOrderID)
	}
	return payload, nil
}

func mapOrderSide(contractSide, action string) (string, error) {
	side := strings.ToLower(strings.TrimSpace(contractSide))
	action = strings.ToLower(strings.TrimSpace(action))

	switch side {
	case "yes":
		switch action {
		case "buy":
			return "bid", nil
		case "sell":
			return "ask", nil
		default:
			return "", fmt.Errorf("kalshi: unsupported action %q", action)
		}
	case "no":
		switch action {
		case "buy":
			return "ask", nil
		case "sell":
			return "bid", nil
		default:
			return "", fmt.Errorf("kalshi: unsupported action %q", action)
		}
	default:
		return "", fmt.Errorf("kalshi: unsupported side %q", contractSide)
	}
}

func mapOrderPriceCents(req CreateOrderRequest) (int64, error) {
	side := strings.ToLower(strings.TrimSpace(req.Side))
	orderType := strings.ToLower(strings.TrimSpace(req.Type))

	if orderType == "" {
		return 0, errors.New("kalshi: order type is required")
	}

	switch orderType {
	case "limit":
		// Kalshi quotes the YES book. NO orders are expressed as the economically
		// equivalent YES side, so buy NO becomes ask at 1-no_price and sell NO
		// becomes bid at 1-no_price.
		switch side {
		case "yes":
			if req.YesPrice == nil {
				return 0, errors.New("kalshi: yes price is required")
			}
			return validateQuoteCents(*req.YesPrice)
		case "no":
			if req.NoPrice == nil {
				return 0, errors.New("kalshi: no price is required")
			}
			cents, err := validateQuoteCents(*req.NoPrice)
			if err != nil {
				return 0, err
			}
			return 100 - cents, nil
		default:
			return 0, fmt.Errorf("kalshi: unsupported side %q", req.Side)
		}
	case "market":
		return 0, errors.New("kalshi: market orders are disabled until sandbox smoke-tested")
	default:
		return 0, fmt.Errorf("kalshi: unsupported order type %q", req.Type)
	}
}

func validateQuoteCents(cents int64) (int64, error) {
	if cents <= 0 || cents >= 100 {
		return 0, fmt.Errorf("kalshi: quote cents %d out of range", cents)
	}
	return cents, nil
}

func timeInForceForOrder(orderType string) string {
	switch strings.ToLower(strings.TrimSpace(orderType)) {
	case "market":
		return "immediate_or_cancel"
	default:
		return "good_till_canceled"
	}
}

func formatCents(cents int64) string {
	return fmt.Sprintf("%.2f", float64(cents)/100)
}

func mapMarketPosition(resp marketPosition) (PositionResponse, error) {
	ticker := strings.TrimSpace(resp.Ticker)
	if ticker == "" {
		return PositionResponse{}, errors.New("kalshi: position ticker is required")
	}
	positionFP, err := parseFixedFloat(resp.PositionFP)
	if err != nil {
		return PositionResponse{}, fmt.Errorf("kalshi: parse position count: %w", err)
	}
	exposureDollars, err := parseFixedFloat(resp.MarketExposureDollars)
	if err != nil {
		return PositionResponse{}, fmt.Errorf("kalshi: parse market exposure: %w", err)
	}
	position := PositionResponse{Ticker: ticker}
	if positionFP < 0 {
		position.Side = "no"
		position.Count = int64(math.Round(math.Abs(positionFP)))
	} else {
		position.Side = "yes"
		position.Count = int64(math.Round(positionFP))
	}
	position.ValueCents = int64(math.Round(math.Abs(exposureDollars) * 100))
	return position, nil
}

func parseFixedFloat(raw string) (float64, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, errors.New("value is required")
	}
	return strconv.ParseFloat(trimmed, 64)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
