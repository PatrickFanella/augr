package lifecycle

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

const (
	orderIDDomain   = "execution-order"
	bindingIDDomain = "execution-order-binding"
)

// Side is the account-perspective direction of one routed order.
type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

// OrderType identifies supported common order mechanics.
type OrderType string

const (
	OrderMarket    OrderType = "market"
	OrderLimit     OrderType = "limit"
	OrderStop      OrderType = "stop"
	OrderStopLimit OrderType = "stop_limit"
)

// TimeInForce identifies supported common venue duration instructions.
type TimeInForce string

const (
	TimeInForceDay TimeInForce = "day"
	TimeInForceGTC TimeInForce = "gtc"
	TimeInForceIOC TimeInForce = "ioc"
	TimeInForceFOK TimeInForce = "fok"
	TimeInForceGTD TimeInForce = "gtd"
)

// Order is the immutable route command. Current status is replayed from events.
type Order struct {
	ID                   uuid.UUID
	IntentID             uuid.UUID
	AccountID            uuid.UUID
	InstrumentID         uuid.UUID
	IdempotencyKey       string
	ClientOrderID        string
	Side                 Side
	OrderType            OrderType
	TimeInForce          TimeInForce
	Quantity             decimal.Decimal
	LimitPrice           *decimal.Decimal
	StopPrice            *decimal.Decimal
	Venue                string
	VenueContractID      uuid.UUID
	RouteQuoteSnapshotID uuid.UUID
	RoutedAt             time.Time
	PolicyKind           PolicyKind
	PolicyVersion        string
	CreatedAt            time.Time
}

// OrderBinding immutably associates the canonical order with one external
// venue/simulator order identity.
type OrderBinding struct {
	ID              uuid.UUID
	OrderID         uuid.UUID
	AccountID       uuid.UUID
	Venue           string
	ExternalOrderID string
	CreatedAt       time.Time
}

// RouteInput supplies exact dated venue mechanics and point-in-time evidence.
type RouteInput struct {
	OrderIdempotencyKey string
	Instrument          instrument.Instrument
	VenueContract       instrument.VenueContract
	RouteSnapshot       marketdata.QuoteSnapshot
	QuoteRequirements   marketdata.QuoteRequirements
	OrderType           OrderType
	TimeInForce         TimeInForce
	LimitPrice          *decimal.Decimal
	StopPrice           *decimal.Decimal
	PolicyKind          PolicyKind
	PolicyVersion       string
	Event               EventInput
	RoutedAt            time.Time
	CreatedAt           time.Time
}

// Route materializes one deterministic order command and routed event.
func Route(aggregate *Aggregate, input RouteInput) (*Transition, error) {
	if aggregate == nil || aggregate.State != StateRiskApproved || aggregate.AllocatedQuantity == nil || aggregate.Order != nil {
		return nil, fmt.Errorf("route execution intent: lifecycle must contain one approved allocation and no order")
	}
	if input.Instrument.ID != aggregate.Intent.InstrumentID {
		return nil, fmt.Errorf("route execution intent: instrument mismatch")
	}
	if err := input.Instrument.Validate(); err != nil || input.Instrument.Status != instrument.StatusActive {
		return nil, fmt.Errorf("route execution intent: instrument is not executable: %w", err)
	}
	if err := input.VenueContract.Validate(); err != nil {
		return nil, fmt.Errorf("route execution intent: invalid venue contract: %w", err)
	}
	routedAt := normalizeTime(input.RoutedAt)
	if routedAt.IsZero() {
		return nil, fmt.Errorf("route execution intent: route time is required")
	}
	assessment, err := input.RouteSnapshot.AssessForExecution(routedAt, input.QuoteRequirements, input.Instrument, input.VenueContract)
	if err != nil {
		return nil, fmt.Errorf("route execution intent: quote is not executable: %w", err)
	}
	if assessment.SnapshotID != input.RouteSnapshot.ID || assessment.VenueContractID == nil || *assessment.VenueContractID != input.VenueContract.ID ||
		!assessment.EvaluatedAt.Equal(routedAt) {
		return nil, fmt.Errorf("route execution intent: quote assessment identity mismatch")
	}
	quantity := aggregate.AllocatedQuantity.Abs()
	if !isExactMultiple(quantity, input.VenueContract.LotSize) {
		return nil, fmt.Errorf("route execution intent: quantity is not an exact venue lot")
	}
	limitPrice := cloneDecimal(input.LimitPrice)
	stopPrice := cloneDecimal(input.StopPrice)
	if err := validateOrderPrices(input.OrderType, limitPrice, stopPrice, input.VenueContract.TickSize); err != nil {
		return nil, fmt.Errorf("route execution intent: %w", err)
	}
	if !validTimeInForce(input.TimeInForce) {
		return nil, fmt.Errorf("route execution intent: invalid time in force %q", input.TimeInForce)
	}
	if input.PolicyKind != PolicySimulation && input.PolicyKind != PolicyVenue {
		return nil, fmt.Errorf("route execution intent: invalid policy kind %q", input.PolicyKind)
	}
	policyVersion := strings.TrimSpace(input.PolicyVersion)
	orderKey := strings.TrimSpace(input.OrderIdempotencyKey)
	if policyVersion == "" || orderKey == "" {
		return nil, fmt.Errorf("route execution intent: order idempotency key and policy version are required")
	}
	createdAt := normalizeTime(input.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	order := Order{
		IntentID:             aggregate.Intent.ID,
		AccountID:            aggregate.Intent.AccountID,
		InstrumentID:         aggregate.Intent.InstrumentID,
		IdempotencyKey:       orderKey,
		Side:                 sideForDelta(*aggregate.AllocatedQuantity),
		OrderType:            input.OrderType,
		TimeInForce:          input.TimeInForce,
		Quantity:             quantity,
		LimitPrice:           limitPrice,
		StopPrice:            stopPrice,
		Venue:                input.VenueContract.Venue,
		VenueContractID:      input.VenueContract.ID,
		RouteQuoteSnapshotID: input.RouteSnapshot.ID,
		RoutedAt:             routedAt,
		PolicyKind:           input.PolicyKind,
		PolicyVersion:        policyVersion,
		CreatedAt:            createdAt,
	}
	order.ID = economicid.DeterministicUUID(orderIDDomain, order.IntentID.String(), order.IdempotencyKey)
	order.ClientOrderID = order.ID.String()
	if err := order.Validate(); err != nil {
		return nil, fmt.Errorf("route execution intent: %w", err)
	}
	event, err := newEvent(aggregate.Intent, EventOrderRouted, StateRiskApproved, StateRouted, input.Event, createdAt)
	if err != nil {
		return nil, fmt.Errorf("route execution intent: %w", err)
	}
	event.OrderID = cloneUUID(&order.ID)
	event.PolicyKind = order.PolicyKind
	event.PolicyVersion = order.PolicyVersion
	event.QuantityDelta = cloneDecimal(aggregate.AllocatedQuantity)
	event.QuoteSnapshotID = cloneUUID(&order.RouteQuoteSnapshotID)
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("route execution intent: %w", err)
	}
	return &Transition{Event: event, Order: &order}, nil
}

// Acknowledge creates the one immutable external binding and working event.
func Acknowledge(aggregate *Aggregate, externalOrderID string, input EventInput, createdAt time.Time) (*Transition, error) {
	if aggregate == nil || aggregate.State != StateRouted || aggregate.Order == nil || aggregate.Binding != nil {
		return nil, fmt.Errorf("acknowledge execution order: lifecycle must be routed and unbound")
	}
	createdAt = normalizeTime(createdAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	binding, err := newOrderBinding(aggregate, externalOrderID, createdAt)
	if err != nil {
		return nil, fmt.Errorf("acknowledge execution order: %w", err)
	}
	event, err := newEvent(aggregate.Intent, EventOrderWorking, StateRouted, StateWorking, input, createdAt)
	if err != nil {
		return nil, fmt.Errorf("acknowledge execution order: %w", err)
	}
	event.OrderID = cloneUUID(&aggregate.Order.ID)
	event.BindingID = cloneUUID(&binding.ID)
	event.PolicyKind = aggregate.Order.PolicyKind
	event.PolicyVersion = aggregate.Order.PolicyVersion
	event.QuantityDelta = cloneDecimal(aggregate.AllocatedQuantity)
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("acknowledge execution order: %w", err)
	}
	return &Transition{Event: event, Binding: binding}, nil
}

// Validate checks immutable order shape independently of reference lookups.
func (order Order) Validate() error {
	if order.ID == uuid.Nil || order.IntentID == uuid.Nil || order.AccountID == uuid.Nil || order.InstrumentID == uuid.Nil ||
		order.VenueContractID == uuid.Nil || order.RouteQuoteSnapshotID == uuid.Nil {
		return fmt.Errorf("execution order IDs are required")
	}
	if order.IdempotencyKey == "" || order.IdempotencyKey != strings.TrimSpace(order.IdempotencyKey) || order.ClientOrderID != order.ID.String() {
		return fmt.Errorf("execution order identity is invalid")
	}
	if order.Side != SideBuy && order.Side != SideSell {
		return fmt.Errorf("execution order side is invalid")
	}
	if !validExactDecimal(order.Quantity, false) || !order.Quantity.IsPositive() || !validTimeInForce(order.TimeInForce) {
		return fmt.Errorf("execution order quantity or time in force is invalid")
	}
	if !validOrderType(order.OrderType) || order.Venue == "" || order.Venue != strings.ToLower(strings.TrimSpace(order.Venue)) {
		return fmt.Errorf("execution order type or venue is invalid")
	}
	if order.PolicyKind != PolicySimulation && order.PolicyKind != PolicyVenue || order.PolicyVersion == "" || order.PolicyVersion != strings.TrimSpace(order.PolicyVersion) {
		return fmt.Errorf("execution order policy is invalid")
	}
	if order.RoutedAt.IsZero() || order.RoutedAt.Location() != time.UTC || !order.RoutedAt.Equal(order.RoutedAt.Truncate(time.Microsecond)) ||
		order.CreatedAt.IsZero() || order.CreatedAt.Location() != time.UTC || !order.CreatedAt.Equal(order.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("execution order timestamps must use UTC microsecond precision")
	}
	for _, price := range []*decimal.Decimal{order.LimitPrice, order.StopPrice} {
		if price != nil && (!validExactDecimal(*price, true) || price.IsNegative()) {
			return fmt.Errorf("execution order price is invalid")
		}
	}
	expectedID := economicid.DeterministicUUID(orderIDDomain, order.IntentID.String(), order.IdempotencyKey)
	if order.ID != expectedID {
		return fmt.Errorf("execution order ID does not match deterministic identity")
	}
	return nil
}

// Validate checks immutable external binding identity.
func (binding OrderBinding) Validate() error {
	if binding.ID == uuid.Nil || binding.OrderID == uuid.Nil || binding.AccountID == uuid.Nil ||
		binding.Venue == "" || binding.Venue != strings.ToLower(strings.TrimSpace(binding.Venue)) ||
		binding.ExternalOrderID == "" || binding.ExternalOrderID != strings.TrimSpace(binding.ExternalOrderID) {
		return fmt.Errorf("execution order binding identity is invalid")
	}
	if binding.CreatedAt.IsZero() || binding.CreatedAt.Location() != time.UTC || !binding.CreatedAt.Equal(binding.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("execution order binding creation time must use UTC microsecond precision")
	}
	if binding.ID != economicid.DeterministicUUID(bindingIDDomain, binding.OrderID.String()) {
		return fmt.Errorf("execution order binding ID does not match deterministic identity")
	}
	return nil
}

// SameOrderPayload compares semantic route retries excluding local creation time.
func SameOrderPayload(left, right *Order) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.IntentID == right.IntentID && left.AccountID == right.AccountID &&
		left.InstrumentID == right.InstrumentID && left.IdempotencyKey == right.IdempotencyKey &&
		left.ClientOrderID == right.ClientOrderID && left.Side == right.Side && left.OrderType == right.OrderType &&
		left.TimeInForce == right.TimeInForce && left.Quantity.Equal(right.Quantity) &&
		equalDecimal(left.LimitPrice, right.LimitPrice) && equalDecimal(left.StopPrice, right.StopPrice) &&
		left.Venue == right.Venue && left.VenueContractID == right.VenueContractID &&
		left.RouteQuoteSnapshotID == right.RouteQuoteSnapshotID && left.RoutedAt.Equal(right.RoutedAt) &&
		left.PolicyKind == right.PolicyKind && left.PolicyVersion == right.PolicyVersion
}

func validOrderType(value OrderType) bool {
	return value == OrderMarket || value == OrderLimit || value == OrderStop || value == OrderStopLimit
}

func validTimeInForce(value TimeInForce) bool {
	switch value {
	case TimeInForceDay, TimeInForceGTC, TimeInForceIOC, TimeInForceFOK, TimeInForceGTD:
		return true
	default:
		return false
	}
}

func validateOrderPrices(orderType OrderType, limitPrice, stopPrice *decimal.Decimal, tick decimal.Decimal) error {
	if !validOrderType(orderType) {
		return fmt.Errorf("invalid order type %q", orderType)
	}
	for name, price := range map[string]*decimal.Decimal{"limit": limitPrice, "stop": stopPrice} {
		if price != nil && (!validExactDecimal(*price, true) || price.IsNegative() || !isExactMultiple(*price, tick)) {
			return fmt.Errorf("%s price is invalid or off tick", name)
		}
	}
	switch orderType {
	case OrderMarket:
		if limitPrice != nil || stopPrice != nil {
			return fmt.Errorf("market order cannot carry limit or stop price")
		}
	case OrderLimit:
		if limitPrice == nil || stopPrice != nil {
			return fmt.Errorf("limit order requires only limit price")
		}
	case OrderStop:
		if stopPrice == nil || limitPrice != nil {
			return fmt.Errorf("stop order requires only stop price")
		}
	case OrderStopLimit:
		if limitPrice == nil || stopPrice == nil {
			return fmt.Errorf("stop-limit order requires limit and stop prices")
		}
	}
	return nil
}

func isExactMultiple(value, step decimal.Decimal) bool {
	return step.IsPositive() && value.Mod(step).IsZero()
}

func sideForDelta(value decimal.Decimal) Side {
	if value.IsNegative() {
		return SideSell
	}
	return SideBuy
}
