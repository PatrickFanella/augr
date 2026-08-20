package kalshi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

// CommonRouteFacts are immutable Kalshi routing coordinates that are not
// represented by the venue contract's deliberately exact outcome-only
// metadata object. The reviewed V2 API currently supports exchange index 0.
type CommonRouteFacts struct {
	Subaccount    int
	ExchangeIndex int
}

// CommonOrderRequest is the reviewed Kalshi V2 single-book wire request.
// Outcome and Action are canonical validation facts and never appear on wire.
type CommonOrderRequest struct {
	Ticker        string `json:"ticker"`
	ClientOrderID string `json:"client_order_id"`
	Side          string `json:"side"`
	Count         string `json:"count"`
	Price         string `json:"price"`
	TimeInForce   string `json:"time_in_force"`
	Subaccount    int    `json:"subaccount"`
	ExchangeIndex int    `json:"exchange_index"`
	Outcome       string `json:"-"`
	Action        string `json:"-"`
}

func MapCommonOrderRequest(policy *venue.Policy, primary *instrument.Instrument, contract *instrument.VenueContract, order *lifecycle.Order, route CommonRouteFacts) (CommonOrderRequest, error) {
	if policy == nil || primary == nil || contract == nil || order == nil {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: policy, instrument, contract, and order are required")
	}
	if policy.Provider() != venue.ProviderKalshi || policy.Venue() != "kalshi" {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: reviewed Kalshi policy is required")
	}
	if err := primary.Validate(); err != nil || primary.Status != instrument.StatusActive {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: instrument is not active and valid: %w", err)
	}
	if err := contract.Validate(); err != nil {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: invalid venue contract: %w", err)
	}
	if primary.ID != order.InstrumentID || contract.InstrumentID != primary.ID || contract.ID != order.VenueContractID ||
		contract.Venue != "kalshi" || order.Venue != "kalshi" || order.PolicyKind != lifecycle.PolicyVenue || order.PolicyVersion != policy.Version() {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: canonical instrument, contract, venue, or policy mismatch")
	}
	if order.ClientOrderID != order.ID.String() || len(order.ClientOrderID) > policy.MaxClientOrderIDLength() {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: client order identity is invalid")
	}
	if !policy.Supports(primary.AssetClass, order.OrderType, order.TimeInForce) {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: unsupported capability %s/%s/%s", primary.AssetClass, order.OrderType, order.TimeInForce)
	}
	if order.OrderType != lifecycle.OrderLimit || order.StopPrice != nil || order.LimitPrice == nil {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: exact limit price and no stop price are required")
	}
	if order.Side != lifecycle.SideBuy && order.Side != lifecycle.SideSell {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: unsupported action %q", order.Side)
	}
	if !validKalshiDecimal(order.Quantity) || !order.Quantity.IsPositive() || !order.Quantity.Mod(contract.LotSize).IsZero() {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: quantity is invalid or off lot")
	}
	if !validKalshiDecimal(*order.LimitPrice) || !order.LimitPrice.IsPositive() || order.LimitPrice.GreaterThanOrEqual(decimal.NewFromInt(1)) || !order.LimitPrice.Mod(contract.TickSize).IsZero() {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: price is invalid or off tick")
	}
	if route.Subaccount < 0 || route.Subaccount > 32 || route.ExchangeIndex != 0 {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: unsupported subaccount or exchange index")
	}
	outcome, err := exactKalshiOutcome(contract.Metadata)
	if err != nil {
		return CommonOrderRequest{}, err
	}
	bookSide := ""
	switch outcome + "/" + string(order.Side) {
	case "yes/buy", "no/sell":
		bookSide = "bid"
	case "yes/sell", "no/buy":
		bookSide = "ask"
	default:
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: unsupported outcome/action")
	}
	providerPrice := *order.LimitPrice
	if outcome == "no" {
		providerPrice = decimal.NewFromInt(1).Sub(providerPrice)
	}
	tif := map[lifecycle.TimeInForce]string{
		lifecycle.TimeInForceGTC: "good_till_canceled",
		lifecycle.TimeInForceIOC: "immediate_or_cancel",
		lifecycle.TimeInForceFOK: "fill_or_kill",
	}[order.TimeInForce]
	if tif == "" {
		return CommonOrderRequest{}, fmt.Errorf("kalshi common order: unsupported time in force %q", order.TimeInForce)
	}
	return CommonOrderRequest{
		Ticker: contract.ContractID, ClientOrderID: order.ClientOrderID, Side: bookSide,
		Count: order.Quantity.StringFixedBank(int32(-contract.LotSize.Exponent())),
		Price: providerPrice.String(), TimeInForce: tif, Subaccount: route.Subaccount,
		ExchangeIndex: route.ExchangeIndex, Outcome: outcome, Action: string(order.Side),
	}, nil
}

func validKalshiDecimal(value decimal.Decimal) bool {
	return value.Exponent() >= -12 && value.NumDigits()+int(value.Exponent()) <= 26
}

func exactKalshiOutcome(raw json.RawMessage) (string, error) {
	type nested struct {
		Outcome string `json:"outcome"`
	}
	type metadata struct {
		KalshiV2 nested `json:"kalshi_v2"`
	}
	var value metadata
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("kalshi common order: metadata must be exactly kalshi_v2.outcome: %w", err)
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return "", fmt.Errorf("kalshi common order: metadata must be one exact object: %w", err)
	}
	if value.KalshiV2.Outcome != "yes" && value.KalshiV2.Outcome != "no" {
		return "", fmt.Errorf("kalshi common order: metadata outcome must be exactly yes or no")
	}
	return value.KalshiV2.Outcome, nil
}

// CommonOrder retains all exact fields needed to reconcile a V2 order.
type CommonOrder struct {
	ID               string `json:"order_id"`
	ClientOrderID    string `json:"client_order_id"`
	Ticker           string `json:"ticker"`
	Side             string `json:"side"`
	Action           string `json:"action"`
	OutcomeSide      string `json:"outcome_side"`
	BookSide         string `json:"book_side"`
	Type             string `json:"type"`
	Status           string `json:"status"`
	YesPriceDollars  string `json:"yes_price_dollars"`
	NoPriceDollars   string `json:"no_price_dollars"`
	FillCountFP      string `json:"fill_count_fp"`
	RemainingCountFP string `json:"remaining_count_fp"`
	InitialCountFP   string `json:"initial_count_fp"`
	CreatedTime      string `json:"created_time"`
	LastUpdateTime   string `json:"last_update_time"`
	SubaccountNumber int    `json:"subaccount_number"`
	ExchangeIndex    int    `json:"exchange_index"`
}

type CommonOrderFact struct {
	Order      CommonOrder
	RawPayload json.RawMessage
}

type CommonFill struct {
	ID               string `json:"fill_id"`
	TradeID          string `json:"trade_id"`
	OrderID          string `json:"order_id"`
	Ticker           string `json:"ticker"`
	Side             string `json:"side"`
	Action           string `json:"action"`
	OutcomeSide      string `json:"outcome_side"`
	BookSide         string `json:"book_side"`
	CountFP          string `json:"count_fp"`
	YesPriceDollars  string `json:"yes_price_dollars"`
	NoPriceDollars   string `json:"no_price_dollars"`
	FeeCost          string `json:"fee_cost"`
	CreatedTime      string `json:"created_time"`
	SubaccountNumber int    `json:"subaccount_number"`
	ExchangeIndex    int    `json:"exchange_index"`
}

type CommonFillFact struct {
	Fill       CommonFill
	RawPayload json.RawMessage
}

type CommonLifecycleClient struct{ client signedClient }

func NewCommonLifecycleClient(client signedClient) (*CommonLifecycleClient, error) {
	if client == nil {
		return nil, fmt.Errorf("kalshi common lifecycle client is required")
	}
	return &CommonLifecycleClient{client: client}, nil
}

func (c *CommonLifecycleClient) Submit(ctx context.Context, request CommonOrderRequest) (*CommonOrderFact, error) {
	if err := validateCommonRequest(request); err != nil {
		return nil, err
	}
	body, err := c.client.Post(ctx, "/portfolio/events/orders", request)
	if err != nil {
		return nil, fmt.Errorf("kalshi common lifecycle: submit: %w", err)
	}
	var envelope struct {
		Order CommonOrder `json:"order"`
	}
	if err := decodeOneJSON(body, &envelope); err != nil {
		return nil, fmt.Errorf("kalshi common lifecycle: decode submit: %w", err)
	}
	if strings.TrimSpace(envelope.Order.ID) == "" {
		return nil, fmt.Errorf("kalshi common lifecycle: submit response missing order id")
	}
	return &CommonOrderFact{Order: envelope.Order, RawPayload: append(json.RawMessage(nil), body...)}, nil
}

func (c *CommonLifecycleClient) FindByClientOrderID(ctx context.Context, clientOrderID string, subaccount int) (*CommonOrderFact, error) {
	clientOrderID = strings.TrimSpace(clientOrderID)
	if clientOrderID == "" || subaccount < 0 || subaccount > 32 {
		return nil, fmt.Errorf("kalshi common lifecycle: valid client id and subaccount are required")
	}
	var matches []*CommonOrderFact
	for _, path := range []string{"/portfolio/orders", "/portfolio/orders/historical"} {
		facts, err := c.scanOrders(ctx, path, subaccount)
		if err != nil {
			return nil, err
		}
		for _, fact := range facts {
			if fact.Order.ClientOrderID == clientOrderID {
				matches = append(matches, fact)
			}
		}
		if len(matches) > 0 {
			break
		}
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("kalshi common lifecycle: client order id not found")
	}
	if len(matches) != 1 {
		return nil, fmt.Errorf("kalshi common lifecycle: multiple orders share client order id")
	}
	return matches[0], nil
}

func (c *CommonLifecycleClient) scanOrders(ctx context.Context, path string, subaccount int) ([]*CommonOrderFact, error) {
	query := url.Values{"limit": {"1000"}, "subaccount": {fmt.Sprint(subaccount)}}
	seen := map[string]bool{}
	var facts []*CommonOrderFact
	for {
		body, err := c.client.Get(ctx, path, query, true)
		if err != nil {
			return nil, fmt.Errorf("kalshi common lifecycle: scan orders: %w", err)
		}
		var envelope struct {
			Orders []json.RawMessage `json:"orders"`
			Cursor string            `json:"cursor"`
		}
		if err := decodeOneJSON(body, &envelope); err != nil {
			return nil, fmt.Errorf("kalshi common lifecycle: decode orders: %w", err)
		}
		for _, raw := range envelope.Orders {
			var order CommonOrder
			if err := decodeOneJSON(raw, &order); err != nil {
				return nil, fmt.Errorf("kalshi common lifecycle: decode order: %w", err)
			}
			facts = append(facts, &CommonOrderFact{Order: order, RawPayload: append(json.RawMessage(nil), raw...)})
		}
		cursor := strings.TrimSpace(envelope.Cursor)
		if cursor == "" {
			return facts, nil
		}
		if seen[cursor] {
			return nil, fmt.Errorf("kalshi common lifecycle: repeated order cursor")
		}
		seen[cursor] = true
		query.Set("cursor", cursor)
	}
}

func (c *CommonLifecycleClient) ListFills(ctx context.Context, orderID, ticker string, subaccount int) ([]*CommonFillFact, error) {
	orderID, ticker = strings.TrimSpace(orderID), strings.TrimSpace(ticker)
	if orderID == "" || ticker == "" || subaccount < 0 || subaccount > 32 {
		return nil, fmt.Errorf("kalshi common lifecycle: valid order, ticker, and subaccount are required")
	}
	var facts []*CommonFillFact
	for _, path := range []string{"/portfolio/fills", "/portfolio/fills/historical"} {
		query := url.Values{"cursor": {""}, "limit": {"1000"}, "order_id": {orderID}, "ticker": {ticker}, "subaccount": {fmt.Sprint(subaccount)}}
		seen := map[string]bool{}
		for {
			body, err := c.client.Get(ctx, path, query, true)
			if err != nil {
				return nil, fmt.Errorf("kalshi common lifecycle: list fills: %w", err)
			}
			var envelope struct {
				Fills  []json.RawMessage `json:"fills"`
				Cursor string            `json:"cursor"`
			}
			if err := decodeOneJSON(body, &envelope); err != nil {
				return nil, fmt.Errorf("kalshi common lifecycle: decode fills: %w", err)
			}
			for _, raw := range envelope.Fills {
				var fill CommonFill
				if err := decodeOneJSON(raw, &fill); err != nil {
					return nil, fmt.Errorf("kalshi common lifecycle: decode fill: %w", err)
				}
				facts = append(facts, &CommonFillFact{Fill: fill, RawPayload: append(json.RawMessage(nil), raw...)})
			}
			cursor := strings.TrimSpace(envelope.Cursor)
			if cursor == "" {
				break
			}
			if seen[cursor] {
				return nil, fmt.Errorf("kalshi common lifecycle: repeated fill cursor")
			}
			seen[cursor] = true
			query.Set("cursor", cursor)
		}
	}
	return facts, nil
}

func (c *CommonLifecycleClient) Cancel(ctx context.Context, orderID string, subaccount, exchangeIndex int) error {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" || subaccount < 0 || subaccount > 32 || exchangeIndex != 0 {
		return fmt.Errorf("kalshi common lifecycle: valid cancel identity is required")
	}
	query := url.Values{"subaccount": {fmt.Sprint(subaccount)}, "exchange_index": {fmt.Sprint(exchangeIndex)}}
	if _, err := c.client.Delete(ctx, "/portfolio/events/orders/"+url.PathEscape(orderID), query); err != nil {
		return fmt.Errorf("kalshi common lifecycle: cancel: %w", err)
	}
	return nil
}

func validateCommonRequest(request CommonOrderRequest) error {
	if strings.TrimSpace(request.Ticker) == "" || strings.TrimSpace(request.ClientOrderID) == "" ||
		(request.Side != "bid" && request.Side != "ask") || (request.Outcome != "yes" && request.Outcome != "no") ||
		(request.Action != "buy" && request.Action != "sell") || request.Subaccount < 0 || request.Subaccount > 32 || request.ExchangeIndex != 0 {
		return fmt.Errorf("kalshi common lifecycle: invalid mapped request identity")
	}
	if _, err := decimal.NewFromString(request.Count); err != nil {
		return fmt.Errorf("kalshi common lifecycle: invalid exact count")
	}
	if _, err := decimal.NewFromString(request.Price); err != nil {
		return fmt.Errorf("kalshi common lifecycle: invalid exact price")
	}
	if request.TimeInForce != "good_till_canceled" && request.TimeInForce != "immediate_or_cancel" && request.TimeInForce != "fill_or_kill" {
		return fmt.Errorf("kalshi common lifecycle: invalid time in force")
	}
	return nil
}

func decodeOneJSON(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return ensureJSONEOF(decoder)
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("trailing JSON value")
		}
		return err
	}
	return nil
}
