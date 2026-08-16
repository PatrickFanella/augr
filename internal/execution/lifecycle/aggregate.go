package lifecycle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

// Transition is one event plus any immutable child facts inserted atomically
// with it. Order, binding, and fill children are introduced by later slices.
type Transition struct {
	Event   Event
	Order   *Order
	Binding *OrderBinding
	Fill    *Fill

	Normalization *ledger.EconomicNormalization
}

// Allocate records the exact quantity admitted by the allocator. Allocation
// can reduce magnitude but cannot reverse or enlarge the proposed intent.
func Allocate(aggregate *Aggregate, quantity decimal.Decimal, input EventInput, createdAt time.Time) (*Transition, error) {
	if aggregate == nil || aggregate.State != StateProposed {
		return nil, fmt.Errorf("allocate execution intent: lifecycle must be proposed")
	}
	if !validExactDecimal(quantity, false) || quantity.Sign() != aggregate.Intent.DesiredQuantityDelta.Sign() ||
		quantity.Abs().GreaterThan(aggregate.Intent.DesiredQuantityDelta.Abs()) {
		return nil, fmt.Errorf("allocate execution intent: allocation must retain direction and not exceed desired quantity")
	}
	event, err := newEvent(aggregate.Intent, EventIntentAllocated, StateProposed, StateAllocated, input, createdAt)
	if err != nil {
		return nil, fmt.Errorf("allocate execution intent: %w", err)
	}
	event.QuantityDelta = cloneDecimal(&quantity)
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("allocate execution intent: %w", err)
	}
	return &Transition{Event: event}, nil
}

// ApproveRisk records deterministic risk admission for the exact allocated
// quantity. Risk evaluation cannot resize the allocation implicitly.
func ApproveRisk(aggregate *Aggregate, input EventInput, createdAt time.Time) (*Transition, error) {
	return riskTransition(aggregate, true, input, createdAt)
}

// RejectRisk records a terminal deterministic risk rejection.
func RejectRisk(aggregate *Aggregate, input EventInput, createdAt time.Time) (*Transition, error) {
	return riskTransition(aggregate, false, input, createdAt)
}

func riskTransition(aggregate *Aggregate, approved bool, input EventInput, createdAt time.Time) (*Transition, error) {
	if aggregate == nil || aggregate.State != StateAllocated || aggregate.AllocatedQuantity == nil {
		return nil, fmt.Errorf("risk execution intent: lifecycle must contain an allocation")
	}
	kind := EventRiskRejected
	next := StateRiskRejected
	if approved {
		kind = EventRiskApproved
		next = StateRiskApproved
	}
	event, err := newEvent(aggregate.Intent, kind, StateAllocated, next, input, createdAt)
	if err != nil {
		return nil, fmt.Errorf("risk execution intent: %w", err)
	}
	event.QuantityDelta = cloneDecimal(aggregate.AllocatedQuantity)
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("risk execution intent: %w", err)
	}
	return &Transition{Event: event}, nil
}

// ApplyTransition validates one transition against the current aggregate and
// returns a deep-cloned next aggregate. Persistence performs the same check
// after locking and reloading the intent row.
func ApplyTransition(aggregate *Aggregate, transition *Transition) (*Aggregate, error) {
	if aggregate == nil || transition == nil {
		return nil, fmt.Errorf("apply execution transition: aggregate and transition are required")
	}
	event := transition.Event
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("apply execution transition: %w", err)
	}
	if event.IntentID != aggregate.Intent.ID || event.AccountID != aggregate.Intent.AccountID ||
		event.Environment != aggregate.Intent.Environment || event.OriginType != aggregate.Intent.OriginType ||
		event.OriginID != aggregate.Intent.OriginID || event.StrategyVersionID != aggregate.Intent.StrategyVersionID {
		return nil, fmt.Errorf("apply execution transition: event context does not match intent")
	}
	if aggregate.Order == nil {
		if event.Kind != EventOrderRouted && (event.OrderID != nil || event.PolicyKind != "" || event.PolicyVersion != "") {
			return nil, fmt.Errorf("apply execution transition: pre-route event carries order policy context")
		}
	} else if event.Kind != EventOrderRouted {
		if event.OrderID == nil || *event.OrderID != aggregate.Order.ID || event.PolicyKind != aggregate.Order.PolicyKind ||
			event.PolicyVersion != aggregate.Order.PolicyVersion {
			return nil, fmt.Errorf("apply execution transition: event order policy context mismatch")
		}
	}
	if event.PriorState != aggregate.State {
		return nil, fmt.Errorf("apply execution transition: stale prior state %q, current %q", event.PriorState, aggregate.State)
	}
	if !validEventTransition(event.Kind, event.ObservationClass, event.PriorState, event.NextState) {
		return nil, fmt.Errorf("apply execution transition: illegal %s transition %s -> %s", event.Kind, event.PriorState, event.NextState)
	}

	next := cloneAggregate(aggregate)
	switch event.Kind {
	case EventIntentAllocated:
		if transition.Order != nil || transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil {
			return nil, fmt.Errorf("apply execution transition: allocation cannot carry child facts")
		}
		if event.QuantityDelta == nil || !validExactDecimal(*event.QuantityDelta, false) ||
			event.QuantityDelta.Sign() != next.Intent.DesiredQuantityDelta.Sign() ||
			event.QuantityDelta.Abs().GreaterThan(next.Intent.DesiredQuantityDelta.Abs()) {
			return nil, fmt.Errorf("apply execution transition: invalid allocated quantity")
		}
		next.AllocatedQuantity = cloneDecimal(event.QuantityDelta)
	case EventRiskApproved, EventRiskRejected:
		if transition.Order != nil || transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil {
			return nil, fmt.Errorf("apply execution transition: risk decision cannot carry child facts")
		}
		if next.AllocatedQuantity == nil || event.QuantityDelta == nil || !event.QuantityDelta.Equal(*next.AllocatedQuantity) {
			return nil, fmt.Errorf("apply execution transition: risk quantity does not match allocation")
		}
	case EventOrderRouted:
		if transition.Order == nil || aggregate.Order != nil || event.OrderID == nil || *event.OrderID != transition.Order.ID ||
			transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil ||
			transition.Order.IntentID != aggregate.Intent.ID || transition.Order.AccountID != aggregate.Intent.AccountID ||
			transition.Order.InstrumentID != aggregate.Intent.InstrumentID || next.AllocatedQuantity == nil ||
			!transition.Order.Quantity.Equal(next.AllocatedQuantity.Abs()) ||
			transition.Order.Side != sideForDelta(*next.AllocatedQuantity) ||
			event.PolicyKind != transition.Order.PolicyKind || event.PolicyVersion != transition.Order.PolicyVersion ||
			event.QuoteSnapshotID == nil || *event.QuoteSnapshotID != transition.Order.RouteQuoteSnapshotID {
			return nil, fmt.Errorf("apply execution transition: routed order does not match lifecycle")
		}
		if err := transition.Order.Validate(); err != nil {
			return nil, fmt.Errorf("apply execution transition: %w", err)
		}
		next.Order = cloneOrder(transition.Order)
	case EventOrderWorking:
		if aggregate.Order == nil || aggregate.Binding != nil || transition.Binding == nil || event.BindingID == nil ||
			transition.Order != nil || transition.Fill != nil || transition.Normalization != nil ||
			*event.BindingID != transition.Binding.ID || transition.Binding.OrderID != aggregate.Order.ID ||
			transition.Binding.AccountID != aggregate.Intent.AccountID || transition.Binding.Venue != aggregate.Order.Venue {
			return nil, fmt.Errorf("apply execution transition: order binding does not match lifecycle")
		}
		if err := transition.Binding.Validate(); err != nil {
			return nil, fmt.Errorf("apply execution transition: %w", err)
		}
		next.Binding = cloneBinding(transition.Binding)
	case EventCancelRequested:
		if aggregate.Binding == nil || event.BindingID == nil || *event.BindingID != aggregate.Binding.ID ||
			transition.Order != nil || transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil {
			return nil, fmt.Errorf("apply execution transition: cancel request binding is invalid")
		}
	case EventOrderCancelled, EventOrderExpired, EventOrderRejected:
		if transition.Order != nil || transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil {
			return nil, fmt.Errorf("apply execution transition: terminal observation cannot carry child facts")
		}
		if aggregate.State == StateWorking || aggregate.State == StatePartiallyFilled {
			if aggregate.Binding == nil || event.BindingID == nil || *event.BindingID != aggregate.Binding.ID {
				return nil, fmt.Errorf("apply execution transition: terminal observation binding is invalid")
			}
		} else if aggregate.State == StateRouted && event.BindingID != nil {
			return nil, fmt.Errorf("apply execution transition: pre-ack terminal observation cannot carry a binding")
		}
	case EventFillAcknowledged, EventFillRecorded:
		if aggregate.Order == nil || transition.Fill == nil || transition.Normalization == nil || event.FillID == nil ||
			transition.Order != nil ||
			*event.FillID != transition.Fill.ID || transition.Fill.IntentID != aggregate.Intent.ID ||
			transition.Fill.OrderID != aggregate.Order.ID || transition.Fill.AccountID != aggregate.Intent.AccountID ||
			transition.Fill.InstrumentID != aggregate.Intent.InstrumentID || transition.Fill.VenueContractID != aggregate.Order.VenueContractID {
			return nil, fmt.Errorf("apply execution transition: fill does not match lifecycle")
		}
		if err := transition.Fill.Validate(); err != nil {
			return nil, fmt.Errorf("apply execution transition: %w", err)
		}
		if err := validateFillNormalization(aggregate, transition.Fill.ID, transition.Normalization); err != nil {
			return nil, fmt.Errorf("apply execution transition: %w", err)
		}
		if transition.Fill.EconomicSourceEventID != transition.Normalization.SourceEvent.ID ||
			transition.Fill.NormalizationID != transition.Normalization.ID ||
			transition.Fill.LedgerTransactionID != transition.Normalization.Transaction.ID ||
			event.Source != transition.Fill.Source || event.SourceNamespace != transition.Fill.SourceNamespace ||
			event.SourceEventID != transition.Fill.SourceEventID || event.SourceRevision != transition.Fill.SourceRevision ||
			!event.ReceivedAt.Equal(transition.Fill.ReceivedAt) {
			return nil, fmt.Errorf("apply execution transition: fill source or economic links mismatch")
		}
		cumulative := sumFillQuantity(next.Fills).Add(transition.Fill.Quantity)
		if cumulative.GreaterThan(aggregate.Order.Quantity) || event.CumulativeFillQuantity == nil ||
			!event.CumulativeFillQuantity.Equal(cumulative) ||
			(event.NextState == StateFilled) != cumulative.Equal(aggregate.Order.Quantity) {
			return nil, fmt.Errorf("apply execution transition: fill cumulative quantity is invalid")
		}
		if event.Kind == EventFillAcknowledged {
			if aggregate.State != StateRouted || aggregate.Binding != nil || transition.Binding == nil || event.BindingID == nil ||
				*event.BindingID != transition.Binding.ID || transition.Binding.OrderID != aggregate.Order.ID {
				return nil, fmt.Errorf("apply execution transition: immediate fill binding is invalid")
			}
			if err := transition.Binding.Validate(); err != nil {
				return nil, fmt.Errorf("apply execution transition: %w", err)
			}
			next.Binding = cloneBinding(transition.Binding)
		} else if aggregate.Binding == nil || transition.Binding != nil || event.BindingID == nil || *event.BindingID != aggregate.Binding.ID {
			return nil, fmt.Errorf("apply execution transition: fill binding is invalid")
		}
		next.Fills = append(next.Fills, *cloneFill(transition.Fill))
	case EventFillCorrectionObserved, EventFillBustObserved:
		if transition.Order != nil || transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil ||
			event.OriginalFillID == nil || !aggregate.hasFill(*event.OriginalFillID, event.OriginalSourceEventID) {
			return nil, fmt.Errorf("apply execution transition: correction or bust does not identify an existing fill")
		}
	case EventUnknownVenueState, EventContradictoryVenueState:
		if transition.Order != nil || transition.Binding != nil || transition.Fill != nil || transition.Normalization != nil {
			return nil, fmt.Errorf("apply execution transition: reconciliation failure cannot carry child facts")
		}
	}
	next.State = event.NextState
	next.Events = append(next.Events, cloneEvent(event))
	return next, nil
}

func validEventTransition(kind EventKind, observationClass ObservationClass, prior, next State) bool {
	if observationClass == ObservationCorrection || observationClass == ObservationBust {
		return (kind == EventFillCorrectionObserved || kind == EventFillBustObserved) &&
			prior != StateNone && prior != StateFailedReconciliation && next == StateFailedReconciliation
	}
	if observationClass != ObservationOrdinary {
		return false
	}
	switch kind {
	case EventIntentProposed:
		return prior == StateNone && next == StateProposed
	case EventIntentAllocated:
		return prior == StateProposed && next == StateAllocated
	case EventRiskApproved:
		return prior == StateAllocated && next == StateRiskApproved
	case EventRiskRejected:
		return prior == StateAllocated && next == StateRiskRejected
	case EventOrderRouted:
		return prior == StateRiskApproved && next == StateRouted
	case EventOrderWorking:
		return prior == StateRouted && next == StateWorking
	case EventCancelRequested:
		return (prior == StateWorking || prior == StatePartiallyFilled) && next == prior
	case EventFillAcknowledged:
		return prior == StateRouted && (next == StatePartiallyFilled || next == StateFilled)
	case EventFillRecorded:
		return (prior == StateWorking || prior == StatePartiallyFilled) &&
			(next == StatePartiallyFilled || next == StateFilled)
	case EventOrderCancelled:
		return (prior == StateRouted || prior == StateWorking || prior == StatePartiallyFilled) && next == StateCancelled
	case EventOrderExpired:
		return (prior == StateRouted || prior == StateWorking || prior == StatePartiallyFilled) && next == StateExpired
	case EventOrderRejected:
		return (prior == StateRouted || prior == StateWorking || prior == StatePartiallyFilled) && next == StateRejected
	case EventUnknownVenueState, EventContradictoryVenueState:
		return prior != StateNone && prior != StateFailedReconciliation && next == StateFailedReconciliation
	default:
		return false
	}
}

// Validate verifies an event independently. Cross-event state and child-fact
// rules are enforced by aggregate replay and PostgreSQL.
func (event Event) Validate() error {
	if event.ID == uuid.Nil || event.IntentID == uuid.Nil || event.AccountID == uuid.Nil {
		return fmt.Errorf("lifecycle event identity is required")
	}
	if event.Kind == "" || event.NextState == StateNone || !event.Environment.IsValid() || !validOrigin(event.OriginType) {
		return fmt.Errorf("lifecycle event kind, state, environment, and origin are required")
	}
	if event.Source == "" || event.Source != strings.ToLower(strings.TrimSpace(event.Source)) ||
		event.SourceNamespace == "" || event.SourceNamespace != strings.TrimSpace(event.SourceNamespace) ||
		event.SourceEventID == "" || event.SourceEventID != strings.TrimSpace(event.SourceEventID) ||
		event.SourceRevision != strings.TrimSpace(event.SourceRevision) {
		return fmt.Errorf("lifecycle event source identity must be normalized")
	}
	if event.Actor == "" || event.Actor != strings.TrimSpace(event.Actor) ||
		event.ReasonCode == "" || event.ReasonCode != strings.ToLower(strings.TrimSpace(event.ReasonCode)) ||
		event.Reason != strings.TrimSpace(event.Reason) {
		return fmt.Errorf("lifecycle event actor and reason must be normalized")
	}
	if event.SourceAt.IsZero() || event.ReceivedAt.IsZero() || event.SourceAt.Location() != time.UTC || event.ReceivedAt.Location() != time.UTC ||
		!event.SourceAt.Equal(event.SourceAt.Truncate(time.Microsecond)) || !event.ReceivedAt.Equal(event.ReceivedAt.Truncate(time.Microsecond)) ||
		event.SourceAt.After(event.ReceivedAt) {
		return fmt.Errorf("lifecycle event timestamps must be ordered UTC microseconds")
	}
	if event.CreatedAt.IsZero() || event.CreatedAt.Location() != time.UTC || !event.CreatedAt.Equal(event.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("lifecycle event creation time must use UTC microsecond precision")
	}
	if err := validateJSONObject(event.Evidence, "lifecycle event evidence"); err != nil {
		return err
	}
	evidenceHash := sha256.Sum256(event.Evidence)
	if event.EvidenceSHA256 != hex.EncodeToString(evidenceHash[:]) {
		return fmt.Errorf("lifecycle event evidence hash mismatch")
	}
	switch event.ObservationClass {
	case ObservationOrdinary:
		if event.ObservationDiscriminator != "" || event.OriginalFillID != nil || event.OriginalSourceEventID != "" {
			return fmt.Errorf("ordinary lifecycle event carries correction identity")
		}
	case ObservationCorrection, ObservationBust:
		if event.ObservationDiscriminator == "" || event.ObservationDiscriminator != strings.TrimSpace(event.ObservationDiscriminator) ||
			event.OriginalFillID == nil || *event.OriginalFillID == uuid.Nil || event.OriginalSourceEventID == "" ||
			event.OriginalSourceEventID != strings.TrimSpace(event.OriginalSourceEventID) {
			return fmt.Errorf("correction or bust lifecycle event identity is incomplete")
		}
		if strings.HasPrefix(event.ObservationDiscriminator, "revision:") &&
			(event.SourceRevision == "" || event.ObservationDiscriminator != "revision:"+event.SourceRevision) {
			return fmt.Errorf("correction revision discriminator does not match source revision")
		}
		if (event.ObservationClass == ObservationCorrection) != (event.Kind == EventFillCorrectionObserved) ||
			(event.ObservationClass == ObservationBust) != (event.Kind == EventFillBustObserved) {
			return fmt.Errorf("correction or bust observation class does not match event kind")
		}
	default:
		return fmt.Errorf("lifecycle event observation class is invalid")
	}
	isFillEvent := event.Kind == EventFillAcknowledged || event.Kind == EventFillRecorded
	if isFillEvent != (event.CumulativeFillQuantity != nil) {
		return fmt.Errorf("lifecycle event cumulative fill quantity must exist exactly on fill events")
	}
	expectedID := economicid.DeterministicUUID(
		eventIDDomain,
		event.IntentID.String(),
		string(event.ObservationClass),
		event.Source,
		event.SourceNamespace,
		eventIdentitySourceEventID(event.ObservationClass, event.SourceEventID, event.OriginalSourceEventID),
		event.ObservationDiscriminator,
	)
	if event.ID != expectedID {
		return fmt.Errorf("lifecycle event ID does not match deterministic identity")
	}
	return nil
}

// Replay reconstructs an aggregate solely from its immutable intent and
// ordered transition graph. Repository loads provide positive ingest sequence
// values; pure in-memory transitions may leave them zero.
func Replay(accountID uuid.UUID, intent Intent, transitions []Transition) (*Aggregate, error) {
	if accountID == uuid.Nil || intent.AccountID != accountID {
		return nil, fmt.Errorf("replay execution lifecycle: account identity mismatch")
	}
	if err := intent.Validate(); err != nil {
		return nil, fmt.Errorf("replay execution lifecycle: %w", err)
	}
	if len(transitions) == 0 {
		return nil, fmt.Errorf("replay execution lifecycle: proposed event is required")
	}
	initial := transitions[0]
	if initial.Order != nil || initial.Binding != nil || initial.Fill != nil || initial.Normalization != nil ||
		initial.Event.Kind != EventIntentProposed || initial.Event.ObservationClass != ObservationOrdinary ||
		initial.Event.PriorState != StateNone || initial.Event.NextState != StateProposed ||
		initial.Event.IntentID != intent.ID || initial.Event.AccountID != intent.AccountID ||
		initial.Event.Environment != intent.Environment || initial.Event.OriginType != intent.OriginType ||
		initial.Event.OriginID != intent.OriginID || initial.Event.StrategyVersionID != intent.StrategyVersionID ||
		initial.Event.QuantityDelta == nil || !initial.Event.QuantityDelta.Equal(intent.DesiredQuantityDelta) ||
		initial.Event.QuoteSnapshotID == nil || *initial.Event.QuoteSnapshotID != intent.DecisionQuoteSnapshotID ||
		initial.Event.OrderID != nil || initial.Event.BindingID != nil || initial.Event.FillID != nil ||
		initial.Event.PolicyKind != "" || initial.Event.PolicyVersion != "" {
		return nil, fmt.Errorf("replay execution lifecycle: invalid proposed event")
	}
	if err := initial.Event.Validate(); err != nil {
		return nil, fmt.Errorf("replay execution lifecycle: %w", err)
	}
	aggregate := &Aggregate{
		Intent: intent,
		State:  StateProposed,
		Events: []Event{cloneEvent(initial.Event)},
	}
	aggregate.Intent.Metadata = append([]byte(nil), intent.Metadata...)
	seen := map[uuid.UUID]struct{}{initial.Event.ID: {}}
	lastSequence := initial.Event.IngestSequence
	for index := 1; index < len(transitions); index++ {
		transition := transitions[index]
		if _, exists := seen[transition.Event.ID]; exists {
			return nil, fmt.Errorf("replay execution lifecycle: duplicate event %s", transition.Event.ID)
		}
		seen[transition.Event.ID] = struct{}{}
		if transition.Event.IngestSequence > 0 || lastSequence > 0 {
			if transition.Event.IngestSequence <= lastSequence {
				return nil, fmt.Errorf("replay execution lifecycle: event ingest sequence is not increasing")
			}
			lastSequence = transition.Event.IngestSequence
		}
		next, err := ApplyTransition(aggregate, &transition)
		if err != nil {
			return nil, fmt.Errorf("replay execution lifecycle event %d: %w", index, err)
		}
		aggregate = next
	}
	return aggregate, nil
}

// SameEventPayload compares exact retry evidence while excluding database
// ingest sequence and local persistence time.
func SameEventPayload(left, right *Event) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.IntentID == right.IntentID &&
		equalUUID(left.OrderID, right.OrderID) && equalUUID(left.BindingID, right.BindingID) && equalUUID(left.FillID, right.FillID) &&
		left.Kind == right.Kind && left.ObservationClass == right.ObservationClass &&
		left.ObservationDiscriminator == right.ObservationDiscriminator && left.PriorState == right.PriorState && left.NextState == right.NextState &&
		left.AccountID == right.AccountID && left.Environment == right.Environment && left.OriginType == right.OriginType &&
		left.OriginID == right.OriginID && left.StrategyVersionID == right.StrategyVersionID &&
		left.PolicyKind == right.PolicyKind && left.PolicyVersion == right.PolicyVersion &&
		equalDecimal(left.QuantityDelta, right.QuantityDelta) && equalDecimal(left.CumulativeFillQuantity, right.CumulativeFillQuantity) &&
		equalUUID(left.QuoteSnapshotID, right.QuoteSnapshotID) && left.Source == right.Source &&
		left.SourceNamespace == right.SourceNamespace && left.SourceEventID == right.SourceEventID && left.SourceRevision == right.SourceRevision &&
		left.SourceAt.Equal(right.SourceAt) && left.ReceivedAt.Equal(right.ReceivedAt) && left.Actor == right.Actor &&
		left.ReasonCode == right.ReasonCode && left.Reason == right.Reason && left.EvidenceSHA256 == right.EvidenceSHA256 &&
		bytes.Equal(left.Evidence, right.Evidence) && equalUUID(left.OriginalFillID, right.OriginalFillID) &&
		left.OriginalSourceEventID == right.OriginalSourceEventID
}

func cloneAggregate(value *Aggregate) *Aggregate {
	cloned := &Aggregate{
		Intent:            value.Intent,
		State:             value.State,
		AllocatedQuantity: cloneDecimal(value.AllocatedQuantity),
		Order:             cloneOrder(value.Order),
		Binding:           cloneBinding(value.Binding),
		Fills:             make([]Fill, len(value.Fills)),
		Events:            make([]Event, len(value.Events)),
	}
	cloned.Intent.Metadata = append([]byte(nil), value.Intent.Metadata...)
	for index := range value.Events {
		cloned.Events[index] = cloneEvent(value.Events[index])
	}
	for index := range value.Fills {
		cloned.Fills[index] = *cloneFill(&value.Fills[index])
	}
	return cloned
}

func cloneFill(value *Fill) *Fill {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneOrder(value *Order) *Order {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.LimitPrice = cloneDecimal(value.LimitPrice)
	cloned.StopPrice = cloneDecimal(value.StopPrice)
	return &cloned
}

func cloneBinding(value *OrderBinding) *OrderBinding {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

// RecoveryEligible reports whether restart handling must reconcile or continue
// the already-created canonical order. It never authorizes a second order.
func (aggregate *Aggregate) RecoveryEligible() bool {
	if aggregate == nil || aggregate.Order == nil {
		return false
	}
	return aggregate.State == StateRouted || aggregate.State == StateWorking || aggregate.State == StatePartiallyFilled
}

// RequestCancel records a durable cancel command without pretending the venue
// has confirmed cancellation.
func RequestCancel(aggregate *Aggregate, input EventInput, createdAt time.Time) (*Transition, error) {
	if aggregate == nil || (aggregate.State != StateWorking && aggregate.State != StatePartiallyFilled) ||
		aggregate.Order == nil || aggregate.Binding == nil {
		return nil, fmt.Errorf("request execution cancel: lifecycle must be bound and working or partially filled")
	}
	event, err := newOrderContextEvent(aggregate, EventCancelRequested, aggregate.State, input, createdAt)
	if err != nil {
		return nil, fmt.Errorf("request execution cancel: %w", err)
	}
	event.BindingID = cloneUUID(&aggregate.Binding.ID)
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("request execution cancel: %w", err)
	}
	return &Transition{Event: event}, nil
}

// ObserveOrderTerminal records one sourced venue/simulator terminal status.
// Rejection may precede binding; cancellation and expiry after working require
// the immutable binding.
func ObserveOrderTerminal(aggregate *Aggregate, kind EventKind, input EventInput, createdAt time.Time) (*Transition, error) {
	if aggregate == nil || aggregate.Order == nil {
		return nil, fmt.Errorf("observe terminal execution order: routed lifecycle is required")
	}
	var next State
	switch kind {
	case EventOrderCancelled:
		next = StateCancelled
	case EventOrderExpired:
		next = StateExpired
	case EventOrderRejected:
		next = StateRejected
	default:
		return nil, fmt.Errorf("observe terminal execution order: invalid terminal event kind %q", kind)
	}
	if !validEventTransition(kind, ObservationOrdinary, aggregate.State, next) {
		return nil, fmt.Errorf("observe terminal execution order: illegal transition from %q", aggregate.State)
	}
	if (aggregate.State == StateWorking || aggregate.State == StatePartiallyFilled) && aggregate.Binding == nil {
		return nil, fmt.Errorf("observe terminal execution order: working lifecycle has no binding")
	}
	event, err := newOrderContextEvent(aggregate, kind, next, input, createdAt)
	if err != nil {
		return nil, fmt.Errorf("observe terminal execution order: %w", err)
	}
	if aggregate.Binding != nil {
		event.BindingID = cloneUUID(&aggregate.Binding.ID)
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("observe terminal execution order: %w", err)
	}
	return &Transition{Event: event}, nil
}

func newOrderContextEvent(aggregate *Aggregate, kind EventKind, next State, input EventInput, createdAt time.Time) (Event, error) {
	event, err := newEvent(aggregate.Intent, kind, aggregate.State, next, input, createdAt)
	if err != nil {
		return Event{}, err
	}
	event.OrderID = cloneUUID(&aggregate.Order.ID)
	event.PolicyKind = aggregate.Order.PolicyKind
	event.PolicyVersion = aggregate.Order.PolicyVersion
	event.QuantityDelta = cloneDecimal(aggregate.AllocatedQuantity)
	return event, nil
}

// FailReconciliation records an explicit unknown/contradictory/correction/bust
// observation without changing any existing economic child fact.
func FailReconciliation(aggregate *Aggregate, kind EventKind, input EventInput, createdAt time.Time) (*Transition, error) {
	if aggregate == nil || aggregate.State == StateNone || aggregate.State == StateFailedReconciliation {
		return nil, fmt.Errorf("fail execution reconciliation: lifecycle cannot accept failure evidence")
	}
	switch kind {
	case EventFillCorrectionObserved:
		if input.ObservationClass != ObservationCorrection {
			return nil, fmt.Errorf("fail execution reconciliation: correction event class is required")
		}
	case EventFillBustObserved:
		if input.ObservationClass != ObservationBust {
			return nil, fmt.Errorf("fail execution reconciliation: bust event class is required")
		}
	case EventUnknownVenueState, EventContradictoryVenueState:
		if input.ObservationClass != "" && input.ObservationClass != ObservationOrdinary {
			return nil, fmt.Errorf("fail execution reconciliation: venue-state failure must use ordinary identity")
		}
	default:
		return nil, fmt.Errorf("fail execution reconciliation: unsupported event kind %q", kind)
	}
	event, err := newEvent(aggregate.Intent, kind, aggregate.State, StateFailedReconciliation, input, createdAt)
	if err != nil {
		return nil, fmt.Errorf("fail execution reconciliation: %w", err)
	}
	event.QuantityDelta = cloneDecimal(aggregate.AllocatedQuantity)
	if aggregate.Order != nil {
		event.OrderID = cloneUUID(&aggregate.Order.ID)
		event.PolicyKind = aggregate.Order.PolicyKind
		event.PolicyVersion = aggregate.Order.PolicyVersion
	}
	if aggregate.Binding != nil {
		event.BindingID = cloneUUID(&aggregate.Binding.ID)
	}
	if (kind == EventFillCorrectionObserved || kind == EventFillBustObserved) &&
		(event.OriginalFillID == nil || !aggregate.hasFill(*event.OriginalFillID, event.OriginalSourceEventID)) {
		return nil, fmt.Errorf("fail execution reconciliation: correction or bust must reference an existing fill")
	}
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("fail execution reconciliation: %w", err)
	}
	return &Transition{Event: event}, nil
}

func (aggregate *Aggregate) hasFill(id uuid.UUID, sourceEventID string) bool {
	for _, fill := range aggregate.Fills {
		if fill.ID == id && fill.SourceEventID == sourceEventID {
			return true
		}
	}
	return false
}

func cloneEvent(value Event) Event {
	cloned := value
	cloned.OrderID = cloneUUID(value.OrderID)
	cloned.BindingID = cloneUUID(value.BindingID)
	cloned.FillID = cloneUUID(value.FillID)
	cloned.QuantityDelta = cloneDecimal(value.QuantityDelta)
	cloned.CumulativeFillQuantity = cloneDecimal(value.CumulativeFillQuantity)
	cloned.QuoteSnapshotID = cloneUUID(value.QuoteSnapshotID)
	cloned.OriginalFillID = cloneUUID(value.OriginalFillID)
	cloned.Evidence = append([]byte(nil), value.Evidence...)
	return cloned
}

func equalUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func equalDecimal(left, right *decimal.Decimal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
