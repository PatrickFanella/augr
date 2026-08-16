package alpaca

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

var (
	// ErrOrderNotFound classifies a provider 404 without discarding the
	// underlying Alpaca status/error details.
	ErrOrderNotFound = errors.New("alpaca: order not found")
	// ErrDuplicateOrder classifies a submit rejected because the stable client
	// order identity has already been used. Recovery must look it up by that ID.
	ErrDuplicateOrder = errors.New("alpaca: duplicate client order id")
)

// CommonOrderRequest is the exact reviewed Trading API v2 projection of one
// common-lifecycle order. It deliberately has no fields for implicit or
// unsupported mechanics such as notional, extended hours, trailing orders,
// brackets, or replacement.
type CommonOrderRequest struct {
	Symbol        string `json:"symbol"`
	Quantity      string `json:"qty"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	TimeInForce   string `json:"time_in_force"`
	ClientOrderID string `json:"client_order_id"`
	LimitPrice    string `json:"limit_price,omitempty"`
	StopPrice     string `json:"stop_price,omitempty"`
}

// MapCommonOrderRequest projects canonical reference and lifecycle facts into
// one deterministic Alpaca request without using binary floating point.
func MapCommonOrderRequest(
	policy *venue.Policy,
	primary *instrument.Instrument,
	contract *instrument.VenueContract,
	order *lifecycle.Order,
) (CommonOrderRequest, error) {
	if policy == nil || primary == nil || contract == nil || order == nil {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: policy, instrument, contract, and order are required")
	}
	if policy.Provider() != venue.ProviderAlpaca || policy.Venue() != "alpaca" {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: reviewed Alpaca policy is required")
	}
	if err := primary.Validate(); err != nil || primary.Status != instrument.StatusActive {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: instrument is not active and valid: %w", err)
	}
	if err := contract.Validate(); err != nil {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: invalid venue contract: %w", err)
	}
	if primary.ID != order.InstrumentID || contract.InstrumentID != primary.ID || contract.ID != order.VenueContractID ||
		contract.Venue != "alpaca" || order.Venue != "alpaca" || order.PolicyKind != lifecycle.PolicyVenue ||
		order.PolicyVersion != policy.Version() {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: canonical instrument, contract, venue, or policy mismatch")
	}
	if order.ID == uuid.Nil || order.ClientOrderID != order.ID.String() || len(order.ClientOrderID) > policy.MaxClientOrderIDLength() {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: client order identity is invalid")
	}
	if order.Side != lifecycle.SideBuy && order.Side != lifecycle.SideSell {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: unsupported side %q", order.Side)
	}
	if !policy.Supports(primary.AssetClass, order.OrderType, order.TimeInForce) {
		return CommonOrderRequest{}, fmt.Errorf(
			"alpaca common order: unsupported capability %s/%s/%s",
			primary.AssetClass, order.OrderType, order.TimeInForce,
		)
	}
	if !validCommonDecimal(order.Quantity, false) || !order.Quantity.IsPositive() ||
		!order.Quantity.Mod(contract.LotSize).IsZero() {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: quantity is invalid or off lot")
	}
	if contract.ContractID == "" {
		return CommonOrderRequest{}, fmt.Errorf("alpaca common order: canonical venue symbol is required")
	}
	if err := validateCommonOrderPrices(order, contract.TickSize); err != nil {
		return CommonOrderRequest{}, err
	}

	request := CommonOrderRequest{
		Symbol: contract.ContractID, Quantity: order.Quantity.String(), Side: string(order.Side),
		Type: string(order.OrderType), TimeInForce: string(order.TimeInForce), ClientOrderID: order.ClientOrderID,
	}
	if order.LimitPrice != nil {
		request.LimitPrice = order.LimitPrice.String()
	}
	if order.StopPrice != nil {
		request.StopPrice = order.StopPrice.String()
	}
	return request, nil
}

func validateCommonOrderPrices(order *lifecycle.Order, tick decimal.Decimal) error {
	validPrice := func(value *decimal.Decimal) bool {
		return value != nil && validCommonDecimal(*value, true) && !value.IsNegative() && value.Mod(tick).IsZero()
	}
	switch order.OrderType {
	case lifecycle.OrderMarket:
		if order.LimitPrice != nil || order.StopPrice != nil {
			return fmt.Errorf("alpaca common order: market order cannot carry limit or stop price")
		}
	case lifecycle.OrderLimit:
		if !validPrice(order.LimitPrice) || order.StopPrice != nil {
			return fmt.Errorf("alpaca common order: limit order requires only an exact on-tick limit price")
		}
	case lifecycle.OrderStop:
		if !validPrice(order.StopPrice) || order.LimitPrice != nil {
			return fmt.Errorf("alpaca common order: stop order requires only an exact on-tick stop price")
		}
	case lifecycle.OrderStopLimit:
		if !validPrice(order.LimitPrice) || !validPrice(order.StopPrice) {
			return fmt.Errorf("alpaca common order: stop-limit order requires exact on-tick limit and stop prices")
		}
	default:
		return fmt.Errorf("alpaca common order: unsupported order type %q", order.OrderType)
	}
	return nil
}

func validCommonDecimal(value decimal.Decimal, allowZero bool) bool {
	if value.Exponent() < -12 || value.NumDigits()+int(value.Exponent()) > 26 {
		return false
	}
	return allowZero || !value.IsZero()
}

// CommonOrder is the provider order object retained alongside exact wire
// evidence. Numeric values remain strings until canonical validation.
type CommonOrder struct {
	ID             string `json:"id"`
	ClientOrderID  string `json:"client_order_id"`
	Symbol         string `json:"symbol"`
	Side           string `json:"side"`
	Type           string `json:"type"`
	TimeInForce    string `json:"time_in_force"`
	Quantity       string `json:"qty"`
	FilledQuantity string `json:"filled_qty"`
	FilledAvgPrice string `json:"filled_avg_price"`
	Status         string `json:"status"`
	CreatedAt      string `json:"created_at"`
	SubmittedAt    string `json:"submitted_at"`
	UpdatedAt      string `json:"updated_at"`
	FilledAt       string `json:"filled_at"`
	CanceledAt     string `json:"canceled_at"`
	ExpiredAt      string `json:"expired_at"`
	FailedAt       string `json:"failed_at"`
	ReplacedBy     string `json:"replaced_by"`
	Replaces       string `json:"replaces"`
}

// CommonOrderFact keeps parsed routing fields and byte-for-byte provider
// evidence together.
type CommonOrderFact struct {
	Order      CommonOrder
	RawPayload json.RawMessage
}

// FillActivity contains only exact Alpaca account-activity fields needed by
// the reviewed normalization. Extra provider fields remain in RawPayload.
type FillActivity struct {
	ID                 string `json:"id"`
	ActivityType       string `json:"activity_type"`
	OrderID            string `json:"order_id"`
	Quantity           string `json:"qty"`
	Price              string `json:"price"`
	Side               string `json:"side"`
	Symbol             string `json:"symbol"`
	TransactionTime    string `json:"transaction_time"`
	CumulativeQuantity string `json:"cum_qty"`
	LeavesQuantity     string `json:"leaves_qty"`
	Commission         string `json:"commission"`
	OriginalActivityID string `json:"original_activity_id"`
}

// FillActivityFact retains one activity's exact object bytes independently of
// page boundaries.
type FillActivityFact struct {
	Activity   FillActivity
	RawPayload json.RawMessage
}

// CommonLifecycleClient is the narrow loopback-testable Trading API transport
// used by the common lifecycle. It performs no runtime activation.
type CommonLifecycleClient struct {
	client *Client
}

func NewCommonLifecycleClient(client *Client) (*CommonLifecycleClient, error) {
	if client == nil {
		return nil, fmt.Errorf("alpaca common lifecycle client is required")
	}
	return &CommonLifecycleClient{client: client}, nil
}

func (client *CommonLifecycleClient) Submit(ctx context.Context, request CommonOrderRequest) (*CommonOrderFact, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("alpaca common lifecycle client is required")
	}
	if err := validateMappedRequest(request); err != nil {
		return nil, err
	}
	body, err := client.client.Post(ctx, "/v2/orders", request)
	if err != nil {
		return nil, classifyCommonTransportError("submit order", err)
	}
	return decodeCommonOrder(body, "submit order")
}

func (client *CommonLifecycleClient) GetByClientOrderID(ctx context.Context, clientOrderID string) (*CommonOrderFact, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("alpaca common lifecycle client is required")
	}
	clientOrderID = strings.TrimSpace(clientOrderID)
	if clientOrderID == "" {
		return nil, fmt.Errorf("alpaca common lifecycle: client order id is required")
	}
	body, err := client.client.Get(ctx, "/v2/orders:by_client_order_id", url.Values{"client_order_id": {clientOrderID}})
	if err != nil {
		return nil, classifyCommonTransportError("get order by client id", err)
	}
	return decodeCommonOrder(body, "get order by client id")
}

func (client *CommonLifecycleClient) GetByExternalOrderID(ctx context.Context, externalOrderID string) (*CommonOrderFact, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("alpaca common lifecycle client is required")
	}
	externalOrderID = strings.TrimSpace(externalOrderID)
	if externalOrderID == "" {
		return nil, fmt.Errorf("alpaca common lifecycle: external order id is required")
	}
	body, err := client.client.Get(ctx, "/v2/orders/"+url.PathEscape(externalOrderID), nil)
	if err != nil {
		return nil, classifyCommonTransportError("get order by external id", err)
	}
	return decodeCommonOrder(body, "get order by external id")
}

func (client *CommonLifecycleClient) Cancel(ctx context.Context, externalOrderID string) error {
	if client == nil || client.client == nil {
		return fmt.Errorf("alpaca common lifecycle client is required")
	}
	externalOrderID = strings.TrimSpace(externalOrderID)
	if externalOrderID == "" {
		return fmt.Errorf("alpaca common lifecycle: external order id is required")
	}
	if _, err := client.client.Delete(ctx, "/v2/orders/"+url.PathEscape(externalOrderID), nil); err != nil {
		return classifyCommonTransportError("cancel order", err)
	}
	return nil
}

// ListFillActivities walks the Alpaca account-activity cursor in ascending
// order. Alpaca defines the next page token as the last activity ID returned;
// a full page with a repeated token therefore fails closed.
func (client *CommonLifecycleClient) ListFillActivities(
	ctx context.Context,
	externalOrderID string,
	pageSize int,
) ([]FillActivityFact, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("alpaca common lifecycle client is required")
	}
	externalOrderID = strings.TrimSpace(externalOrderID)
	if externalOrderID == "" || pageSize <= 0 || pageSize > 100 {
		return nil, fmt.Errorf("alpaca common lifecycle: external order id and page size 1..100 are required")
	}
	var result []FillActivityFact
	seenIDs := make(map[string]struct{})
	token := ""
	for {
		query := url.Values{
			"order_id": {externalOrderID}, "direction": {"asc"}, "page_size": {strconv.Itoa(pageSize)},
		}
		if token != "" {
			query.Set("page_token", token)
		}
		body, err := client.client.Get(ctx, "/v2/account/activities/FILL", query)
		if err != nil {
			return nil, classifyCommonTransportError("list fill activities", err)
		}
		page, err := decodeFillActivityPage(body)
		if err != nil {
			return nil, fmt.Errorf("alpaca common lifecycle: decode fill activities: %w", err)
		}
		for index := range page {
			id := strings.TrimSpace(page[index].Activity.ID)
			if id == "" {
				return nil, fmt.Errorf("alpaca common lifecycle: fill activity id is required")
			}
			if _, duplicate := seenIDs[id]; duplicate {
				return nil, fmt.Errorf("alpaca common lifecycle: duplicate or repeated fill activity id %q", id)
			}
			seenIDs[id] = struct{}{}
			result = append(result, page[index])
		}
		if len(page) < pageSize {
			return result, nil
		}
		next := strings.TrimSpace(page[len(page)-1].Activity.ID)
		if next == "" || next == token {
			return nil, fmt.Errorf("alpaca common lifecycle: repeated fill activity page token %q", next)
		}
		token = next
	}
}

func validateMappedRequest(request CommonOrderRequest) error {
	if strings.TrimSpace(request.Symbol) == "" || strings.TrimSpace(request.Quantity) == "" ||
		strings.TrimSpace(request.ClientOrderID) == "" || request.Symbol != strings.TrimSpace(request.Symbol) ||
		request.ClientOrderID != strings.TrimSpace(request.ClientOrderID) {
		return fmt.Errorf("alpaca common lifecycle: mapped request identity is invalid")
	}
	quantity, err := decimal.NewFromString(request.Quantity)
	if err != nil || !quantity.IsPositive() || quantity.String() != request.Quantity {
		return fmt.Errorf("alpaca common lifecycle: mapped request quantity is not an exact canonical decimal")
	}
	if request.Side != string(lifecycle.SideBuy) && request.Side != string(lifecycle.SideSell) {
		return fmt.Errorf("alpaca common lifecycle: mapped request side is invalid")
	}
	return nil
}

func decodeCommonOrder(body []byte, operation string) (*CommonOrderFact, error) {
	raw := append(json.RawMessage(nil), body...)
	var order CommonOrder
	if err := decodeOneJSON(raw, &order); err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: decode %s response: %w", operation, err)
	}
	if strings.TrimSpace(order.ID) == "" || strings.TrimSpace(order.ClientOrderID) == "" ||
		strings.TrimSpace(order.Symbol) == "" || strings.TrimSpace(order.Status) == "" {
		return nil, fmt.Errorf("alpaca common lifecycle: %s response identity or state is incomplete", operation)
	}
	return &CommonOrderFact{Order: order, RawPayload: raw}, nil
}

func decodeFillActivityPage(body []byte) ([]FillActivityFact, error) {
	var rawActivities []json.RawMessage
	if err := decodeOneJSON(body, &rawActivities); err != nil {
		return nil, err
	}
	result := make([]FillActivityFact, 0, len(rawActivities))
	for index := range rawActivities {
		var activity FillActivity
		if err := decodeOneJSON(rawActivities[index], &activity); err != nil {
			return nil, fmt.Errorf("activity %d: %w", index, err)
		}
		result = append(result, FillActivityFact{
			Activity: activity, RawPayload: append(json.RawMessage(nil), rawActivities[index]...),
		})
	}
	return result, nil
}

func decodeOneJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return err
	}
	return nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("response contains trailing JSON")
		}
		return err
	}
	return nil
}

func classifyCommonTransportError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var response *ErrorResponse
	if errors.As(err, &response) {
		switch response.StatusCode() {
		case http.StatusNotFound:
			return fmt.Errorf("alpaca common lifecycle: %s: %w: %w", operation, ErrOrderNotFound, err)
		case http.StatusConflict:
			return fmt.Errorf("alpaca common lifecycle: %s: %w: %w", operation, ErrDuplicateOrder, err)
		case http.StatusUnprocessableEntity:
			message := strings.ToLower(response.Message)
			if strings.Contains(message, "client_order_id") &&
				(strings.Contains(message, "exist") || strings.Contains(message, "duplicate")) {
				return fmt.Errorf("alpaca common lifecycle: %s: %w: %w", operation, ErrDuplicateOrder, err)
			}
		}
	}
	return fmt.Errorf("alpaca common lifecycle: %s: %w", operation, err)
}
