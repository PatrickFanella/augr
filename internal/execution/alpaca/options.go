package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

// OptionsBroker extends the standard Alpaca broker with options support.
type OptionsBroker struct {
	client *Client
}

// NewOptionsBroker constructs an Alpaca options broker adapter.
func NewOptionsBroker(client *Client) *OptionsBroker {
	return &OptionsBroker{client: client}
}

// optionOrderRequest is the Alpaca single-leg options order payload.
type optionOrderRequest struct {
	Symbol         string `json:"symbol"`
	Qty            string `json:"qty"`
	Side           string `json:"side"`
	Type           string `json:"type"`
	TimeInForce    string `json:"time_in_force"`
	LimitPrice     string `json:"limit_price,omitempty"`
	OrderClass     string `json:"order_class"`
	PositionIntent string `json:"position_intent"`
}

// mlegOrderRequest is the Alpaca multi-leg options order payload.
type mlegOrderRequest struct {
	Qty         string       `json:"qty"`
	Type        string       `json:"type"`
	TimeInForce string       `json:"time_in_force"`
	OrderClass  string       `json:"order_class"`
	Legs        []mlegLeg    `json:"legs"`
	LimitPrice  string       `json:"limit_price,omitempty"`
}

type mlegLeg struct {
	Symbol         string `json:"symbol"`
	Side           string `json:"side"`
	PositionIntent string `json:"position_intent"`
	RatioQty       string `json:"ratio_qty"`
}

type optionOrderResponse struct {
	ID string `json:"id"`
}

type mlegOrderResponse struct {
	ID   string `json:"id"`
	Legs []struct {
		ID string `json:"id"`
	} `json:"legs"`
}

type optionContractsResponse struct {
	Contracts []alpacaOptionContract `json:"option_contracts"`
}

type alpacaOptionContract struct {
	ID               string `json:"id"`
	Symbol           string `json:"symbol"`
	UnderlyingSymbol string `json:"underlying_symbol"`
	Type             string `json:"type"`
	Style            string `json:"style"`
	StrikePrice      string `json:"strike_price"`
	ExpirationDate   string `json:"expiration_date"`
	Multiplier       string `json:"multiplier"`
	Status           string `json:"status"`
}

// SubmitOptionOrder submits a single-leg options order.
// Uses POST /v2/orders with the position_intent field.
func (b *OptionsBroker) SubmitOptionOrder(ctx context.Context, order *domain.Order) (string, error) {
	if b == nil || b.client == nil {
		return "", errors.New("alpaca: options broker client is required")
	}
	if order == nil {
		return "", errors.New("alpaca: order is required")
	}

	symbol := domain.AlpacaSymbol(strings.TrimSpace(order.Ticker))
	if symbol == "" {
		return "", errors.New("alpaca: order ticker is required")
	}
	if order.Quantity <= 0 {
		return "", errors.New("alpaca: order quantity must be greater than zero")
	}
	if order.PositionIntent == nil {
		return "", errors.New("alpaca: position intent is required for options orders")
	}

	side := strings.ToLower(order.Side.String())
	switch domain.OrderSide(side) {
	case domain.OrderSideBuy, domain.OrderSideSell:
	default:
		return "", fmt.Errorf("alpaca: unsupported order side %q", order.Side)
	}

	orderType := strings.ToLower(order.OrderType.String())
	if orderType == "" {
		orderType = "limit"
	}

	req := optionOrderRequest{
		Symbol:         symbol,
		Qty:            formatFloat(order.Quantity),
		Side:           side,
		Type:           orderType,
		TimeInForce:    defaultTimeInForce,
		OrderClass:     "simple",
		PositionIntent: string(*order.PositionIntent),
	}

	if order.LimitPrice != nil {
		req.LimitPrice = formatFloat(*order.LimitPrice)
	}

	responseBody, err := b.client.Post(ctx, "/v2/orders", req)
	if err != nil {
		return "", fmt.Errorf("alpaca: submit option order: %w", err)
	}

	var resp optionOrderResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return "", fmt.Errorf("alpaca: decode option order response: %w", err)
	}
	if strings.TrimSpace(resp.ID) == "" {
		return "", errors.New("alpaca: option order response missing id")
	}

	return resp.ID, nil
}

// SubmitSpreadOrder submits a multi-leg options order.
// Uses POST /v2/orders with order_class "mleg" and a legs array.
func (b *OptionsBroker) SubmitSpreadOrder(ctx context.Context, spread *domain.OptionSpread, quantity float64) ([]string, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("alpaca: options broker client is required")
	}
	if spread == nil {
		return nil, errors.New("alpaca: spread is required")
	}
	if len(spread.Legs) == 0 {
		return nil, errors.New("alpaca: spread must have at least one leg")
	}
	if quantity <= 0 {
		return nil, errors.New("alpaca: spread quantity must be greater than zero")
	}

	legs := make([]mlegLeg, 0, len(spread.Legs))
	for _, leg := range spread.Legs {
		symbol := domain.AlpacaSymbol(strings.TrimSpace(leg.Contract.OCCSymbol))
		if symbol == "" {
			return nil, errors.New("alpaca: spread leg symbol is required")
		}

		side := strings.ToLower(leg.Side.String())
		switch domain.OrderSide(side) {
		case domain.OrderSideBuy, domain.OrderSideSell:
		default:
			return nil, fmt.Errorf("alpaca: unsupported spread leg side %q", leg.Side)
		}

		ratio := leg.Ratio
		if ratio <= 0 {
			ratio = 1
		}

		legs = append(legs, mlegLeg{
			Symbol:         symbol,
			Side:           side,
			PositionIntent: string(leg.PositionIntent),
			RatioQty:       strconv.Itoa(ratio),
		})
	}

	req := mlegOrderRequest{
		Qty:         formatFloat(quantity),
		Type:        "market",
		TimeInForce: defaultTimeInForce,
		OrderClass:  "mleg",
		Legs:        legs,
	}

	responseBody, err := b.client.Post(ctx, "/v2/orders", req)
	if err != nil {
		return nil, fmt.Errorf("alpaca: submit spread order: %w", err)
	}

	var resp mlegOrderResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return nil, fmt.Errorf("alpaca: decode spread order response: %w", err)
	}

	ids := make([]string, 0, 1+len(resp.Legs))
	if parentID := strings.TrimSpace(resp.ID); parentID != "" {
		ids = append(ids, parentID)
	}
	for _, leg := range resp.Legs {
		if legID := strings.TrimSpace(leg.ID); legID != "" {
			ids = append(ids, legID)
		}
	}
	if len(ids) == 0 {
		return nil, errors.New("alpaca: spread order response missing ids")
	}

	return ids, nil
}

// GetOptionsContracts fetches available option contracts for an underlying symbol.
// Uses GET /v2/options/contracts.
func (b *OptionsBroker) GetOptionsContracts(ctx context.Context, underlying string) ([]domain.OptionContract, error) {
	if b == nil || b.client == nil {
		return nil, errors.New("alpaca: options broker client is required")
	}

	symbol := strings.TrimSpace(strings.ToUpper(underlying))
	if symbol == "" {
		return nil, errors.New("alpaca: underlying symbol is required")
	}

	params := url.Values{}
	params.Set("underlying_symbols", symbol)
	params.Set("status", "active")

	responseBody, err := b.client.Get(ctx, "/v2/options/contracts", params)
	if err != nil {
		return nil, fmt.Errorf("alpaca: get options contracts: %w", err)
	}

	var resp optionContractsResponse
	if err := json.Unmarshal(responseBody, &resp); err != nil {
		return nil, fmt.Errorf("alpaca: decode options contracts response: %w", err)
	}

	contracts := make([]domain.OptionContract, 0, len(resp.Contracts))
	for _, ac := range resp.Contracts {
		contract, err := mapAlpacaContract(ac)
		if err != nil {
			// Skip unparseable contracts rather than failing the whole batch.
			continue
		}
		contracts = append(contracts, *contract)
	}

	return contracts, nil
}

func mapAlpacaContract(ac alpacaOptionContract) (*domain.OptionContract, error) {
	symbol := strings.TrimSpace(ac.Symbol)
	if symbol == "" {
		return nil, errors.New("alpaca: contract symbol is required")
	}

	// Try ParseOCC first — it gives us all the structured fields.
	parsed, err := domain.ParseOCC(symbol)
	if err == nil {
		// Overlay API-provided fields where available.
		if ac.Style != "" {
			parsed.Style = strings.ToLower(ac.Style)
		}
		if ac.Multiplier != "" {
			if m, mErr := strconv.ParseFloat(ac.Multiplier, 64); mErr == nil && m > 0 {
				parsed.Multiplier = m
			}
		}
		return parsed, nil
	}

	// Fallback: build from individual response fields.
	var optType domain.OptionType
	switch strings.ToLower(ac.Type) {
	case "call":
		optType = domain.OptionTypeCall
	case "put":
		optType = domain.OptionTypePut
	default:
		return nil, fmt.Errorf("alpaca: unknown option type %q", ac.Type)
	}

	strike, sErr := strconv.ParseFloat(ac.StrikePrice, 64)
	if sErr != nil {
		return nil, fmt.Errorf("alpaca: invalid strike price %q: %w", ac.StrikePrice, sErr)
	}

	expiry, eErr := time.Parse("2006-01-02", ac.ExpirationDate)
	if eErr != nil {
		return nil, fmt.Errorf("alpaca: invalid expiration date %q: %w", ac.ExpirationDate, eErr)
	}

	multiplier := 100.0
	if ac.Multiplier != "" {
		if m, mErr := strconv.ParseFloat(ac.Multiplier, 64); mErr == nil && m > 0 {
			multiplier = m
		}
	}

	return &domain.OptionContract{
		OCCSymbol:  symbol,
		Underlying: strings.TrimSpace(ac.UnderlyingSymbol),
		OptionType: optType,
		Strike:     strike,
		Expiry:     expiry,
		Multiplier: multiplier,
		Style:      strings.ToLower(ac.Style),
	}, nil
}
