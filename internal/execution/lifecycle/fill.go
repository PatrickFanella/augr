package lifecycle

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

const fillIDDomain = "execution-fill"

// Fill is one immutable, exact execution linked one-to-one to raw source
// evidence, an OVR-103 normalization, and its ledger transaction.
type Fill struct {
	ID                    uuid.UUID
	IntentID              uuid.UUID
	OrderID               uuid.UUID
	AccountID             uuid.UUID
	InstrumentID          uuid.UUID
	VenueContractID       uuid.UUID
	EconomicSourceEventID uuid.UUID
	NormalizationID       uuid.UUID
	LedgerTransactionID   uuid.UUID
	Side                  Side
	Quantity              decimal.Decimal
	Price                 decimal.Decimal
	Source                string
	SourceNamespace       string
	SourceEventID         string
	SourceRevision        string
	EffectiveAt           time.Time
	ReceivedAt            time.Time
	CreatedAt             time.Time
}

// FillInput supplies a complete normalization graph and the one lifecycle
// observation committed with it. ExternalOrderID is required only when this is
// the first venue/simulator observation after route.
type FillInput struct {
	Normalization   *ledger.EconomicNormalization
	ExternalOrderID string
	Event           EventInput
	CreatedAt       time.Time
}

// FillID derives the lifecycle fill identity before normalization so the
// normalization can reference `execution_fill/<UUID>` without a write first.
func FillID(orderID, economicSourceEventID uuid.UUID) uuid.UUID {
	return economicid.DeterministicUUID(fillIDDomain, orderID.String(), economicSourceEventID.String())
}

// RecordFill constructs the immutable fill, optional first binding, and one
// lifecycle event. Persistence applies the supplied normalization/ledger and
// these lifecycle facts in one transaction.
func RecordFill(aggregate *Aggregate, input FillInput) (*Transition, error) {
	if aggregate == nil || aggregate.Order == nil || aggregate.AllocatedQuantity == nil {
		return nil, fmt.Errorf("record execution fill: routed lifecycle is required")
	}
	if aggregate.State != StateRouted && aggregate.State != StateWorking && aggregate.State != StatePartiallyFilled {
		return nil, fmt.Errorf("record execution fill: lifecycle state %q cannot accept a fill", aggregate.State)
	}
	normalization := input.Normalization
	if normalization == nil {
		return nil, fmt.Errorf("record execution fill: normalization is required")
	}
	if err := normalization.Validate(); err != nil {
		return nil, fmt.Errorf("record execution fill: invalid normalization: %w", err)
	}
	if normalization.SourceEvent == nil || normalization.SourceEvent.ID == uuid.Nil {
		return nil, fmt.Errorf("record execution fill: raw economic source event is required")
	}
	fillID := FillID(aggregate.Order.ID, normalization.SourceEvent.ID)
	if err := validateFillNormalization(aggregate, fillID, normalization); err != nil {
		return nil, err
	}
	if input.Event.ObservationClass != "" && input.Event.ObservationClass != ObservationOrdinary {
		return nil, fmt.Errorf("record execution fill: fill event must use ordinary observation identity")
	}
	if normalization.SourceEvent.Source != strings.ToLower(strings.TrimSpace(input.Event.Source)) ||
		normalization.SourceEvent.SourceNamespace != strings.TrimSpace(input.Event.SourceNamespace) ||
		normalization.SourceEvent.SourceEventID != strings.TrimSpace(input.Event.SourceEventID) ||
		normalization.SourceEvent.SourceRevision != strings.TrimSpace(input.Event.SourceRevision) ||
		!normalization.SourceEvent.ObservedAt.Equal(normalizeTime(input.Event.ReceivedAt)) ||
		!bytes.Equal(normalization.SourceEvent.RawPayload, input.Event.Evidence) {
		return nil, fmt.Errorf("record execution fill: lifecycle evidence differs from raw economic source event")
	}
	if err := validateJSONObject(normalization.SourceEvent.RawPayload, "execution fill source payload"); err != nil {
		return nil, err
	}
	createdAt := normalizeTime(input.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	fill := Fill{
		ID:                    fillID,
		IntentID:              aggregate.Intent.ID,
		OrderID:               aggregate.Order.ID,
		AccountID:             aggregate.Intent.AccountID,
		InstrumentID:          aggregate.Intent.InstrumentID,
		VenueContractID:       aggregate.Order.VenueContractID,
		EconomicSourceEventID: normalization.SourceEvent.ID,
		NormalizationID:       normalization.ID,
		LedgerTransactionID:   normalization.Transaction.ID,
		Side:                  aggregate.Order.Side,
		Quantity:              *normalization.Quantity,
		Price:                 *normalization.Price,
		Source:                normalization.SourceEvent.Source,
		SourceNamespace:       normalization.SourceEvent.SourceNamespace,
		SourceEventID:         normalization.SourceEvent.SourceEventID,
		SourceRevision:        normalization.SourceEvent.SourceRevision,
		EffectiveAt:           normalization.EffectiveAt,
		ReceivedAt:            normalization.SourceEvent.ObservedAt,
		CreatedAt:             createdAt,
	}
	if err := fill.Validate(); err != nil {
		return nil, fmt.Errorf("record execution fill: %w", err)
	}
	cumulative := sumFillQuantity(aggregate.Fills).Add(fill.Quantity)
	if cumulative.GreaterThan(aggregate.Order.Quantity) {
		return nil, fmt.Errorf("record execution fill: cumulative quantity %s exceeds order quantity %s", cumulative, aggregate.Order.Quantity)
	}
	nextState := StatePartiallyFilled
	if cumulative.Equal(aggregate.Order.Quantity) {
		nextState = StateFilled
	}

	var binding *OrderBinding
	kind := EventFillRecorded
	if aggregate.State == StateRouted {
		if aggregate.Binding != nil {
			return nil, fmt.Errorf("record execution fill: routed lifecycle unexpectedly has a binding")
		}
		var err error
		binding, err = newOrderBinding(aggregate, input.ExternalOrderID, createdAt)
		if err != nil {
			return nil, fmt.Errorf("record execution fill: %w", err)
		}
		kind = EventFillAcknowledged
	} else {
		if aggregate.Binding == nil {
			return nil, fmt.Errorf("record execution fill: working lifecycle has no external binding")
		}
		if externalID := strings.TrimSpace(input.ExternalOrderID); externalID != "" && externalID != aggregate.Binding.ExternalOrderID {
			return nil, fmt.Errorf("record execution fill: external order ID does not match binding")
		}
	}
	event, err := newEvent(aggregate.Intent, kind, aggregate.State, nextState, input.Event, createdAt)
	if err != nil {
		return nil, fmt.Errorf("record execution fill: %w", err)
	}
	event.OrderID = cloneUUID(&aggregate.Order.ID)
	if binding != nil {
		event.BindingID = cloneUUID(&binding.ID)
	} else {
		event.BindingID = cloneUUID(&aggregate.Binding.ID)
	}
	event.FillID = cloneUUID(&fill.ID)
	event.PolicyKind = aggregate.Order.PolicyKind
	event.PolicyVersion = aggregate.Order.PolicyVersion
	event.QuantityDelta = cloneDecimal(aggregate.AllocatedQuantity)
	event.CumulativeFillQuantity = cloneDecimal(&cumulative)
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("record execution fill: %w", err)
	}
	return &Transition{Event: event, Binding: binding, Fill: &fill, Normalization: normalization}, nil
}

func validateFillNormalization(aggregate *Aggregate, fillID uuid.UUID, normalization *ledger.EconomicNormalization) error {
	if normalization.SourceEvent.AccountID != aggregate.Intent.AccountID || normalization.Account == nil || normalization.Account.ID != aggregate.Intent.AccountID ||
		normalization.Instrument == nil || normalization.Instrument.ID != aggregate.Intent.InstrumentID ||
		normalization.VenueContract == nil || normalization.VenueContract.ID != aggregate.Order.VenueContractID ||
		normalization.Venue != aggregate.Order.Venue || normalization.ExecutionOriginType != aggregate.Intent.OriginType ||
		normalization.ExecutionOriginID != aggregate.Intent.OriginID || normalization.ReferenceType != "execution_fill" ||
		normalization.ReferenceID != fillID.String() || normalization.Transaction == nil {
		return fmt.Errorf("record execution fill: normalization context does not match lifecycle")
	}
	wantEventType := ledger.EconomicEventFillBuy
	if aggregate.Order.Side == SideSell {
		wantEventType = ledger.EconomicEventFillSell
	}
	if normalization.EventType != wantEventType || normalization.Quantity == nil || normalization.Price == nil ||
		!normalization.Quantity.IsPositive() || normalization.Price.IsNegative() ||
		!validExactDecimal(*normalization.Quantity, false) || !validExactDecimal(*normalization.Price, true) {
		return fmt.Errorf("record execution fill: normalization fill mechanics do not match order")
	}
	return nil
}

// Validate checks fill identity, exact values, and normalized source evidence.
func (fill Fill) Validate() error {
	if fill.ID == uuid.Nil || fill.IntentID == uuid.Nil || fill.OrderID == uuid.Nil || fill.AccountID == uuid.Nil ||
		fill.InstrumentID == uuid.Nil || fill.VenueContractID == uuid.Nil || fill.EconomicSourceEventID == uuid.Nil ||
		fill.NormalizationID == uuid.Nil || fill.LedgerTransactionID == uuid.Nil {
		return fmt.Errorf("execution fill IDs are required")
	}
	if fill.Side != SideBuy && fill.Side != SideSell || !fill.Quantity.IsPositive() || fill.Price.IsNegative() ||
		!validExactDecimal(fill.Quantity, false) || !validExactDecimal(fill.Price, true) {
		return fmt.Errorf("execution fill side, quantity, or price is invalid")
	}
	if fill.Source == "" || fill.Source != strings.ToLower(strings.TrimSpace(fill.Source)) ||
		fill.SourceNamespace == "" || fill.SourceNamespace != strings.TrimSpace(fill.SourceNamespace) ||
		fill.SourceEventID == "" || fill.SourceEventID != strings.TrimSpace(fill.SourceEventID) ||
		fill.SourceRevision != strings.TrimSpace(fill.SourceRevision) {
		return fmt.Errorf("execution fill source identity is invalid")
	}
	for name, value := range map[string]time.Time{
		"effective": fill.EffectiveAt,
		"received":  fill.ReceivedAt,
		"created":   fill.CreatedAt,
	} {
		if value.IsZero() || value.Location() != time.UTC || !value.Equal(value.Truncate(time.Microsecond)) {
			return fmt.Errorf("execution fill %s time must use UTC microsecond precision", name)
		}
	}
	if fill.EffectiveAt.After(fill.ReceivedAt) {
		return fmt.Errorf("execution fill effective time cannot follow receive time")
	}
	if fill.ID != FillID(fill.OrderID, fill.EconomicSourceEventID) {
		return fmt.Errorf("execution fill ID does not match deterministic identity")
	}
	return nil
}

// SameFillPayload compares exact immutable fill retry facts excluding local
// creation time.
func SameFillPayload(left, right *Fill) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.IntentID == right.IntentID && left.OrderID == right.OrderID &&
		left.AccountID == right.AccountID && left.InstrumentID == right.InstrumentID && left.VenueContractID == right.VenueContractID &&
		left.EconomicSourceEventID == right.EconomicSourceEventID && left.NormalizationID == right.NormalizationID &&
		left.LedgerTransactionID == right.LedgerTransactionID && left.Side == right.Side &&
		left.Quantity.Equal(right.Quantity) && left.Price.Equal(right.Price) && left.Source == right.Source &&
		left.SourceNamespace == right.SourceNamespace && left.SourceEventID == right.SourceEventID &&
		left.SourceRevision == right.SourceRevision && left.EffectiveAt.Equal(right.EffectiveAt) && left.ReceivedAt.Equal(right.ReceivedAt)
}

func sumFillQuantity(fills []Fill) decimal.Decimal {
	total := decimal.Zero
	for _, fill := range fills {
		total = total.Add(fill.Quantity)
	}
	return total
}

func newOrderBinding(aggregate *Aggregate, externalOrderID string, createdAt time.Time) (*OrderBinding, error) {
	externalOrderID = strings.TrimSpace(externalOrderID)
	if externalOrderID == "" {
		return nil, fmt.Errorf("external order ID is required")
	}
	binding := &OrderBinding{
		OrderID:         aggregate.Order.ID,
		AccountID:       aggregate.Intent.AccountID,
		Venue:           aggregate.Order.Venue,
		ExternalOrderID: externalOrderID,
		CreatedAt:       createdAt,
	}
	binding.ID = economicid.DeterministicUUID(bindingIDDomain, binding.OrderID.String())
	if err := binding.Validate(); err != nil {
		return nil, err
	}
	return binding, nil
}
