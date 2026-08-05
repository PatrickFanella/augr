package polymarket

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	polymarketdata "github.com/PatrickFanella/get-rich-quick/internal/data/polymarket"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
)

const defaultTimeInForce = "TIME_IN_FORCE_GOOD_TILL_CANCEL"

var dataAPIBaseURL = "https://data-api.polymarket.com"

// Broker implements the execution.Broker interface for Polymarket retail APIs.
type Broker struct {
	client             *Client
	OfficialCLOBClient polymarketdata.CLOBClient
	Breaker            risk.Breaker
	DryRun             bool
}

// DryRunObservation captures a rejected dry-run order submission.
type DryRunObservation struct {
	Kind       string
	OrderID    string
	Slug       string
	Side       string
	Price      float64
	Size       float64
	RawError   string
	ObservedAt time.Time
}

// DryRunRejectedError wraps a rejected dry-run observation.
type DryRunRejectedError struct {
	Observation DryRunObservation
	Err         error
}

func (e *DryRunRejectedError) Error() string {
	if e == nil {
		return "polymarket: dry-run rejected"
	}
	return fmt.Sprintf("polymarket: dry-run rejected (%s): %s", e.Observation.Kind, e.Observation.RawError)
}

func (e *DryRunRejectedError) Unwrap() error { return e.Err }

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

type CreateOrderResponse = createOrderResponse

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

type accountBalancesResponse struct {
	Balances []struct {
		CurrentBalance float64 `json:"currentBalance"`
		Currency       string  `json:"currency"`
		BuyingPower    float64 `json:"buyingPower"`
		AssetNotional  float64 `json:"assetNotional"`
	} `json:"balances"`
}

type dataAPIPosition struct {
	Slug     string  `json:"slug"`
	Outcome  string  `json:"outcome"`
	Size     float64 `json:"size"`
	AvgPrice float64 `json:"avgPrice"`
	CurPrice float64 `json:"curPrice"`
	Title    string  `json:"title"`
}

// NewBroker constructs a Polymarket broker adapter.
func NewBroker(client *Client) *Broker {
	return &Broker{client: client}
}

// SetOfficialCLOBClient wires an optional read-only official Polymarket CLOB client.
func (b *Broker) SetOfficialCLOBClient(client polymarketdata.CLOBClient) {
	if b == nil {
		return
	}
	b.OfficialCLOBClient = client
}

// SubmitOrder sends an order to Polymarket retail APIs and returns the external order ID.
func (b *Broker) SubmitOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil || b.client == nil {
		return "", errors.New("polymarket: broker client is required")
	}
	if order == nil {
		return "", errors.New("polymarket: order is required")
	}
	if b.Breaker != nil {
		if err := b.Breaker.Allow(ctx, domain.RiskBreakerScopeGlobal); err != nil {
			return "", err
		}
		if order.StrategyID != nil && *order.StrategyID != [16]byte{} {
			if err := b.Breaker.Allow(ctx, domain.RiskBreakerScopeStrategy(order.StrategyID.String())); err != nil {
				return "", err
			}
		}
	}

	request, err := mapCreateOrderRequest(order)
	if err != nil {
		return "", err
	}

	requestPath := "/v1/orders"
	if b.DryRun {
		requestPath = withDryRunQuery(requestPath)
	}
	responseBody, err := b.client.Post(ctx, requestPath, request)
	if err != nil {
		if b.DryRun {
			if kind, ok := ClassifyDryRunError(err); ok {
				obs := DryRunObservation{Kind: kind, Slug: order.Ticker, Side: string(order.Side), RawError: err.Error(), ObservedAt: time.Now()}
				if order.LimitPrice != nil {
					obs.Price = *order.LimitPrice
				}
				obs.Size = order.Quantity
				return "", &DryRunRejectedError{Observation: obs, Err: err}
			}
		}
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

// PrepareTemplate builds a reusable order send template from an execution order.
func (b *Broker) PrepareTemplate(order *domain.Order) (*OrderTemplate, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("polymarket: broker client is required")
	}
	if order == nil {
		return nil, errors.New("polymarket: order is required")
	}
	request, err := mapCreateOrderRequest(order)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("polymarket: marshal order template: %w", err)
	}
	secret, err := base64.StdEncoding.DecodeString(b.client.secretKey)
	if err != nil {
		return nil, fmt.Errorf("polymarket: decode secret key: %w", err)
	}
	if len(secret) == 64 {
		secret = secret[:32]
	}
	if len(secret) != 32 {
		return nil, fmt.Errorf("polymarket: secret key must decode to %d or 64 bytes, got %d", 32, len(secret))
	}
	requestPath := "/v1/orders"
	if b.DryRun {
		requestPath = withDryRunQuery(requestPath)
	}
	fullURL, _, err := b.client.buildURL(requestPath, nil, true)
	if err != nil {
		return nil, err
	}
	tmpl, err := NewOrderTemplate(secret, http.MethodPost, fullURL, body)
	if err != nil {
		return nil, err
	}
	tmpl.SetL2Auth(b.client.address, b.client.keyID, b.client.passphrase)
	if order.StrategyID != nil && *order.StrategyID != [16]byte{} {
		tmpl.StrategyID = order.StrategyID.String()
	}
	return tmpl, nil
}

// SendTemplate sends a pre-built order template.
func (b *Broker) SendTemplate(ctx context.Context, tmpl *OrderTemplate) (*CreateOrderResponse, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("polymarket: broker client is required")
	}
	if tmpl == nil {
		return nil, errors.New("polymarket: template is required")
	}
	if b.Breaker != nil {
		if err := b.Breaker.Allow(ctx, domain.RiskBreakerScopeGlobal); err != nil {
			return nil, err
		}
		if strings.TrimSpace(tmpl.StrategyID) != "" {
			if err := b.Breaker.Allow(ctx, domain.RiskBreakerScopeStrategy(tmpl.StrategyID)); err != nil {
				return nil, err
			}
		}
	}
	if b.DryRun && !hasDryRunQuery(tmpl.URL()) {
		return nil, errors.New("polymarket: dry-run mode active but template URL missing dry=1")
	}
	now := time.Now().Unix()
	sig := tmpl.SignAt(now)
	req, err := tmpl.newRequest(strconv.FormatInt(now, 10), sig)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	resp, err := b.client.getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("polymarket: do templated request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("polymarket: read templated response body: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		if b.DryRun {
			if kind, ok := ClassifyDryRunError(parseErrorResponse(resp.StatusCode, body)); ok {
				parsedErr := parseErrorResponse(resp.StatusCode, body)
				return nil, &DryRunRejectedError{Observation: DryRunObservation{Kind: kind, RawError: parsedErr.Error(), ObservedAt: time.Now()}, Err: parsedErr}
			}
		}
		return nil, parseErrorResponse(resp.StatusCode, body)
	}
	var out createOrderResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("polymarket: decode templated order response: %w", err)
	}
	return &out, nil
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

	if strings.TrimSpace(b.client.address) == "" {
		return nil, errors.New("polymarket: address is required for positions")
	}
	requestURL, err := url.Parse(strings.TrimRight(dataAPIBaseURL, "/") + "/positions")
	if err != nil {
		return nil, fmt.Errorf("polymarket: parse positions url: %w", err)
	}
	query := requestURL.Query()
	query.Set("user", strings.TrimSpace(b.client.address))
	query.Set("limit", "500")
	query.Set("sizeThreshold", "0")
	requestURL.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("polymarket: create positions request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := b.client.getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("polymarket: get positions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("polymarket: read positions response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("polymarket: get positions: %w", parseErrorResponse(resp.StatusCode, responseBody))
	}

	var response []dataAPIPosition
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return nil, fmt.Errorf("polymarket: decode positions response: %w", err)
	}

	positions := make([]domain.Position, 0, len(response))
	for _, apiPosition := range response {
		position := mapDataAPIPosition(apiPosition)
		if strings.TrimSpace(position.Ticker) != "" && position.Quantity > 0 {
			positions = append(positions, position)
		}
	}

	return positions, nil
}

func mapDataAPIPosition(position dataAPIPosition) domain.Position {
	slug := strings.TrimSpace(position.Slug)
	outcome := strings.ToUpper(strings.TrimSpace(position.Outcome))
	ticker := slug
	if outcome == "YES" || outcome == "NO" {
		ticker = slug + ":" + outcome
	}
	currentPrice := position.CurPrice
	unrealizedPnL := (position.CurPrice - position.AvgPrice) * position.Size
	return domain.Position{
		Ticker:        ticker,
		MarketType:    domain.MarketTypePolymarket,
		Side:          domain.PositionSideLong,
		Quantity:      position.Size,
		AvgEntry:      position.AvgPrice,
		CurrentPrice:  &currentPrice,
		UnrealizedPnL: &unrealizedPnL,
	}
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

// GetOrderBook fetches a read-only official CLOB snapshot when configured.
func (b *Broker) GetOrderBook(ctx context.Context, tokenID string) (domain.PolymarketBookSnapshot, error) {
	if b == nil {
		return domain.PolymarketBookSnapshot{}, errors.New("polymarket: broker is nil")
	}
	if b.OfficialCLOBClient == nil {
		return domain.PolymarketBookSnapshot{}, errors.New("polymarket: official clob client is not configured")
	}
	return b.OfficialCLOBClient.GetOrderBook(ctx, tokenID)
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

func formatFloat(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func hasDryRunQuery(rawURL string) bool {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	return parsed.Query().Get(dryRunParamName) == "1"
}
