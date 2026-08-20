package kalshi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

const (
	kalshiAdapterActor   = "kalshi-adapter"
	kalshiFillNormalizer = "kalshi-v2-fill-v1"
)

// CommonLifecycleContext pins every canonical fact needed to interpret
// Kalshi evidence. Route coordinates remain immutable throughout recovery.
type CommonLifecycleContext struct {
	Policy        *venue.Policy
	Aggregate     *lifecycle.Aggregate
	Account       *domain.Account
	Instrument    *instrument.Instrument
	VenueContract *instrument.VenueContract
	Route         CommonRouteFacts
	ReceivedAt    time.Time
}

// PlanOrderResult journals one exact Kalshi order response. Executed is only
// accepted after authoritative fills have already brought the local aggregate
// to the full initial quantity; the order snapshot itself never invents a
// pseudo-fill.
func PlanOrderResult(context CommonLifecycleContext, kind venue.ObservationKind, fact *CommonOrderFact) (*venue.Result, error) {
	if err := validateCommonLifecycleContext(context); err != nil {
		return nil, err
	}
	if kind != venue.ObservationSubmitResponse && kind != venue.ObservationOrderSnapshot && kind != venue.ObservationCancelResponse {
		return nil, fmt.Errorf("kalshi common lifecycle: unsupported order observation kind %q", kind)
	}
	if fact == nil || len(fact.RawPayload) == 0 {
		return nil, fmt.Errorf("kalshi common lifecycle: order fact and raw payload are required")
	}
	var wire CommonOrder
	if err := decodeOneJSON(fact.RawPayload, &wire); err != nil {
		return nil, fmt.Errorf("kalshi common lifecycle: order is not journalable JSON: %w", err)
	}
	current := context.Aggregate
	outcome, _ := exactKalshiOutcome(context.VenueContract.Metadata)
	expectedBook := expectedKalshiBookSide(outcome, current.Order.Side)
	initial, initialErr := decimal.NewFromString(wire.InitialCountFP)
	filled, filledErr := decimal.NewFromString(wire.FillCountFP)
	remaining, remainingErr := decimal.NewFromString(wire.RemainingCountFP)
	yesPrice, yesErr := decimal.NewFromString(wire.YesPriceDollars)
	noPrice, noErr := decimal.NewFromString(wire.NoPriceDollars)
	sourceAt, timeErr := parseKalshiTime(wire.LastUpdateTime)
	malformed := wire.ID == "" || wire.ClientOrderID == "" || wire.Ticker == "" || wire.Status == "" ||
		wire.Type == "" || wire.LastUpdateTime == "" || initialErr != nil || filledErr != nil || remainingErr != nil ||
		yesErr != nil || noErr != nil || !initial.IsPositive() || filled.IsNegative() || remaining.IsNegative() ||
		!filled.Add(remaining).Equal(initial) || !yesPrice.Add(noPrice).Equal(decimal.NewFromInt(1))
	contradiction := !reflect.DeepEqual(wire, fact.Order) || timeErr != nil || sourceAt.After(context.ReceivedAt) ||
		wire.ClientOrderID != current.Order.ClientOrderID || wire.Ticker != context.VenueContract.ContractID ||
		wire.Side != outcome || wire.OutcomeSide != outcome || wire.BookSide != expectedBook ||
		wire.Action != string(current.Order.Side) || wire.Type != "limit" || !initial.Equal(current.Order.Quantity) ||
		wire.SubaccountNumber == nil || *wire.SubaccountNumber != context.Route.Subaccount ||
		wire.ExchangeIndex == nil || *wire.ExchangeIndex != context.Route.ExchangeIndex ||
		current.Binding != nil && wire.ID != current.Binding.ExternalOrderID
	expectedProviderPrice := *current.Order.LimitPrice
	if outcome == "no" {
		expectedProviderPrice = decimal.NewFromInt(1).Sub(expectedProviderPrice)
	}
	if !yesPrice.Equal(expectedProviderPrice) {
		contradiction = true
	}
	localFilled := sumKalshiFills(current)
	if !filled.Equal(localFilled) {
		contradiction = true
	}
	mapped, known := context.Policy.Mapping(venue.MappingOrderStatus, wire.Status)
	if !known {
		mapped = venue.OutcomeUnknownState
	}
	if wire.Status == "executed" && (!filled.Equal(initial) || !remaining.IsZero() || !localFilled.Equal(current.Order.Quantity) || current.State != lifecycle.StateFilled) {
		contradiction = true
	}
	if wire.Status == "resting" && (remaining.IsZero() || current.State == lifecycle.StateFilled || current.State == lifecycle.StateCancelled) {
		contradiction = true
	}
	if wire.Status == "canceled" && current.State == lifecycle.StateFilled {
		contradiction = true
	}
	if timeErr != nil || sourceAt.After(context.ReceivedAt) {
		sourceAt = context.ReceivedAt
	}
	observationKind := kind
	providerState := wire.Status
	if malformed {
		mapped = venue.OutcomeMalformedObservation
		observationKind = venue.ObservationMalformedResponse
		providerState = "malformed_order"
	} else if contradiction {
		mapped = venue.OutcomeContradiction
	}
	if mapped == venue.OutcomeAcknowledge && (current.State == lifecycle.StateWorking || current.State == lifecycle.StatePartiallyFilled) {
		mapped = venue.OutcomeNoChange
	}
	namespace := "kalshi/portfolio/orders"
	label := "order"
	if kind == venue.ObservationSubmitResponse {
		namespace = "kalshi/portfolio/events/orders/submit"
		label = "submit"
	}
	providerContractID := wire.Ticker
	if observationKind == venue.ObservationMalformedResponse {
		providerContractID = ""
	}
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: context.Account.ID, IntentID: current.Intent.ID, OrderID: current.Order.ID,
		BindingID: currentBindingID(current), VenueContractID: context.VenueContract.ID,
		Provider: venue.ProviderKalshi, Venue: "kalshi", PolicyVersion: context.Policy.Version(),
		Kind: observationKind, ProviderState: providerState, MappedOutcome: mapped,
		ExternalOrderID: wire.ID, ClientOrderID: current.Order.ClientOrderID,
		ProviderContractID: providerContractID, CanonicalOutcome: validOutcome(wire.OutcomeSide),
		ProviderBookSide: validBookSide(wire.BookSide), ProviderAction: validAction(wire.Action),
		ProviderPrice: optionalValidPrice(yesPrice), IdentityKind: venue.SourceIdentityLocalResponse,
		SourceNamespace: namespace, SourceEventID: localKalshiOrderSourceID(label, current.Order.ClientOrderID, wire.LastUpdateTime),
		SourceRevision: wire.LastUpdateTime, SourceAt: sourceAt, ReceivedAt: context.ReceivedAt,
		RawPayload: fact.RawPayload, CreatedAt: context.ReceivedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("kalshi common lifecycle: construct order observation: %w", err)
	}
	step := venue.ResultStep{Observation: observation}
	next := current
	switch mapped {
	case venue.OutcomeNoChange, venue.OutcomeFillNotice:
		// Raw evidence is the complete result. Economic fills are authoritative.
	case venue.OutcomeAcknowledge:
		transition, transitionErr := lifecycle.Acknowledge(current, wire.ID, kalshiEventInput(observation, "order_acknowledged"), observation.CreatedAt)
		if transitionErr != nil {
			return nil, transitionErr
		}
		step.Transition = transition
		next, err = lifecycle.ApplyTransition(current, transition)
	case venue.OutcomeCancelled:
		transition, transitionErr := lifecycle.ObserveOrderTerminal(current, lifecycle.EventOrderCancelled, kalshiEventInput(observation, "order_canceled"), observation.CreatedAt)
		if transitionErr != nil {
			return nil, transitionErr
		}
		step.Transition = transition
		next, err = lifecycle.ApplyTransition(current, transition)
	case venue.OutcomeUnknownState, venue.OutcomeContradiction, venue.OutcomeMalformedObservation:
		reason := "contradictory_order"
		kind := lifecycle.EventContradictoryVenueState
		if mapped == venue.OutcomeUnknownState {
			reason, kind = "unknown_order_status", lifecycle.EventUnknownVenueState
		}
		transition, transitionErr := lifecycle.FailReconciliation(current, kind, kalshiEventInput(observation, reason), observation.CreatedAt)
		if transitionErr != nil {
			return nil, transitionErr
		}
		step.Transition = transition
		next, err = lifecycle.ApplyTransition(current, transition)
	default:
		return nil, fmt.Errorf("kalshi common lifecycle: unsupported mapped order outcome %q", mapped)
	}
	if err != nil {
		return nil, fmt.Errorf("kalshi common lifecycle: apply order result: %w", err)
	}
	return &venue.Result{Initial: current, Aggregate: next, Steps: []venue.ResultStep{step}}, nil
}

// PlanFillResults validates authoritative Kalshi fill records in provider
// order. Every accepted fill becomes one raw observation, one raw economic
// event, one normalization/ledger graph, and one canonical lifecycle fill.
// The first contradiction is journaled and terminates reconciliation without
// creating economic facts.
func PlanFillResults(context CommonLifecycleContext, facts []CommonFillFact) (*venue.Result, error) {
	if err := validateCommonLifecycleContext(context); err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, fmt.Errorf("kalshi common lifecycle: at least one fill is required")
	}
	result := &venue.Result{Initial: context.Aggregate, Aggregate: context.Aggregate}
	current := context.Aggregate
	for index := range facts {
		step, next, err := planFill(context, current, facts[index])
		if err != nil {
			return nil, fmt.Errorf("kalshi common lifecycle: plan fill %d: %w", index, err)
		}
		result.Steps = append(result.Steps, step)
		result.Aggregate = next
		current = next
		if current.State == lifecycle.StateFailedReconciliation {
			break
		}
	}
	return result, nil
}

func planFill(context CommonLifecycleContext, current *lifecycle.Aggregate, fact CommonFillFact) (venue.ResultStep, *lifecycle.Aggregate, error) {
	if len(fact.RawPayload) == 0 {
		return venue.ResultStep{}, nil, fmt.Errorf("fill raw payload is required")
	}
	var wire CommonFill
	if err := decodeOneJSON(fact.RawPayload, &wire); err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("fill is not journalable JSON: %w", err)
	}
	outcome, _ := exactKalshiOutcome(context.VenueContract.Metadata)
	expectedBook := expectedKalshiBookSide(outcome, current.Order.Side)
	contradiction := !reflect.DeepEqual(wire, fact.Fill)
	quantity, quantityErr := decimal.NewFromString(wire.CountFP)
	yesPrice, yesErr := decimal.NewFromString(wire.YesPriceDollars)
	noPrice, noErr := decimal.NewFromString(wire.NoPriceDollars)
	fee := decimal.Zero
	var feeErr error
	if wire.FeeCost != "" {
		fee, feeErr = decimal.NewFromString(wire.FeeCost)
	}
	sourceAt, timeErr := parseKalshiTime(wire.CreatedTime)
	malformed := wire.ID == "" || wire.ID != strings.TrimSpace(wire.ID) || wire.TradeID == "" ||
		wire.OrderID == "" || wire.Ticker == "" || wire.CountFP == "" || wire.CreatedTime == "" ||
		quantityErr != nil || !quantity.IsPositive() || yesErr != nil || noErr != nil ||
		yesPrice.IsNegative() || noPrice.IsNegative() || !yesPrice.Add(noPrice).Equal(decimal.NewFromInt(1)) ||
		feeErr != nil || fee.IsNegative()
	if timeErr != nil || sourceAt.After(context.ReceivedAt) {
		contradiction = true
		sourceAt = context.ReceivedAt
	}
	if wire.OrderID == "" || wire.Ticker != context.VenueContract.ContractID || wire.Side != outcome ||
		wire.OutcomeSide != outcome || wire.BookSide != expectedBook || wire.Action != string(current.Order.Side) ||
		wire.SubaccountNumber == nil || *wire.SubaccountNumber != context.Route.Subaccount ||
		wire.ExchangeIndex == nil || *wire.ExchangeIndex != context.Route.ExchangeIndex ||
		current.Binding != nil && wire.OrderID != current.Binding.ExternalOrderID {
		contradiction = true
	}
	mapped := venue.OutcomeFill
	kind := venue.ObservationFill
	providerState := "fill"
	if malformed {
		mapped = venue.OutcomeMalformedObservation
		kind = venue.ObservationMalformedResponse
		providerState = "malformed_fill"
	} else if contradiction {
		mapped = venue.OutcomeContradiction
	}
	providerPrice := yesPrice
	if outcome == "no" {
		providerPrice = noPrice
	}
	bookSide := validBookSide(wire.BookSide)
	action := validAction(wire.Action)
	providerOutcome := validOutcome(wire.OutcomeSide)
	providerContractID := wire.Ticker
	if kind == venue.ObservationMalformedResponse {
		providerContractID = ""
	}
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: context.Account.ID, IntentID: current.Intent.ID, OrderID: current.Order.ID,
		BindingID: currentBindingID(current), VenueContractID: context.VenueContract.ID,
		Provider: venue.ProviderKalshi, Venue: "kalshi", PolicyVersion: context.Policy.Version(),
		Kind: kind, ProviderState: providerState, MappedOutcome: mapped,
		ExternalOrderID: wire.OrderID, ClientOrderID: current.Order.ClientOrderID,
		ProviderContractID: providerContractID, CanonicalOutcome: providerOutcome,
		ProviderBookSide: bookSide, ProviderAction: action, ProviderPrice: optionalValidPrice(providerPrice),
		IdentityKind: venue.SourceIdentityProvider, SourceNamespace: context.Policy.AuthoritativeFillNamespace(),
		SourceEventID: sourceIdentity(wire.ID, fact.RawPayload), SourceRevision: wire.CreatedTime,
		SourceAt: sourceAt, ReceivedAt: context.ReceivedAt, RawPayload: fact.RawPayload, CreatedAt: context.ReceivedAt,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct fill observation: %w", err)
	}
	if mapped != venue.OutcomeFill {
		return planFillFailure(current, observation)
	}
	return planEconomicFill(context, current, wire, quantity, providerPrice, fee, observation)
}

func planEconomicFill(context CommonLifecycleContext, current *lifecycle.Aggregate, fill CommonFill, quantity, price, fee decimal.Decimal, observation *venue.Observation) (venue.ResultStep, *lifecycle.Aggregate, error) {
	sourceEvent, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: context.Account.ID, Source: "kalshi", SourceNamespace: observation.SourceNamespace,
		SourceEventID: observation.SourceEventID, SourceRevision: observation.SourceRevision,
		ObservedAt: observation.ReceivedAt, RawPayload: observation.RawPayload, CreatedAt: observation.CreatedAt,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct economic source event: %w", err)
	}
	fillID := lifecycle.FillID(current.Order.ID, sourceEvent.ID)
	ledgerSide := ledger.FillSideBuy
	if current.Order.Side == lifecycle.SideSell {
		ledgerSide = ledger.FillSideSell
	}
	var cost *ledger.CostComponent
	if fee.IsPositive() {
		cost = &ledger.CostComponent{Kind: ledger.CostKindFee, Currency: context.Account.BaseCurrency, Amount: fee}
	}
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent: sourceEvent, Account: context.Account,
			NormalizerVersion:   kalshiFillNormalizer + "/" + context.Policy.Version(),
			ExecutionOriginType: current.Intent.OriginType, ExecutionOriginID: current.Intent.OriginID,
			ReferenceType: "execution_fill", ReferenceID: fillID.String(), EffectiveAt: observation.SourceAt,
		},
		Instrument: *context.Instrument, VenueContract: *context.VenueContract,
		Side: ledgerSide, Quantity: quantity, Price: price, Cost: cost,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct fill normalization: %w", err)
	}
	transition, err := lifecycle.RecordFill(current, lifecycle.FillInput{
		Normalization: normalization, ExternalOrderID: fill.OrderID,
		Event: lifecycle.EventInput{
			Source: "kalshi", SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
			SourceRevision: observation.SourceRevision, SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
			Actor: kalshiAdapterActor, ReasonCode: "authoritative_fill_reported", Evidence: observation.RawPayload,
		},
		CreatedAt: observation.CreatedAt,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct lifecycle fill: %w", err)
	}
	next, err := lifecycle.ApplyTransition(current, transition)
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("apply lifecycle fill: %w", err)
	}
	return venue.ResultStep{Observation: observation, EconomicSourceEvent: sourceEvent, Transition: transition}, next, nil
}

func planFillFailure(current *lifecycle.Aggregate, observation *venue.Observation) (venue.ResultStep, *lifecycle.Aggregate, error) {
	reason := "contradictory_fill"
	if observation.MappedOutcome == venue.OutcomeMalformedObservation {
		reason = "malformed_fill"
	}
	transition, err := lifecycle.FailReconciliation(current, lifecycle.EventContradictoryVenueState, lifecycle.EventInput{
		Source: "kalshi", SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
		SourceRevision: observation.SourceRevision, SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
		Actor: kalshiAdapterActor, ReasonCode: reason, Evidence: observation.RawPayload,
	}, observation.CreatedAt)
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct fill reconciliation failure: %w", err)
	}
	next, err := lifecycle.ApplyTransition(current, transition)
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("apply fill reconciliation failure: %w", err)
	}
	return venue.ResultStep{Observation: observation, Transition: transition}, next, nil
}

func validateCommonLifecycleContext(context CommonLifecycleContext) error {
	if context.Policy == nil || context.Aggregate == nil || context.Aggregate.Order == nil || context.Account == nil || context.Instrument == nil || context.VenueContract == nil {
		return fmt.Errorf("kalshi common lifecycle: policy, aggregate, account, instrument, and contract are required")
	}
	if context.Policy.Provider() != venue.ProviderKalshi || context.Policy.Venue() != "kalshi" ||
		context.Aggregate.Order.PolicyKind != lifecycle.PolicyVenue || context.Aggregate.Order.PolicyVersion != context.Policy.Version() {
		return fmt.Errorf("kalshi common lifecycle: exact reviewed Kalshi policy is required")
	}
	if err := context.Account.Validate(); err != nil || context.Account.Status != domain.AccountStatusActive {
		return fmt.Errorf("kalshi common lifecycle: account is not active and valid: %w", err)
	}
	if err := context.Instrument.Validate(); err != nil || context.Instrument.Status != instrument.StatusActive {
		return fmt.Errorf("kalshi common lifecycle: instrument is not active and valid: %w", err)
	}
	if err := context.VenueContract.Validate(); err != nil {
		return fmt.Errorf("kalshi common lifecycle: venue contract is invalid: %w", err)
	}
	if _, err := exactKalshiOutcome(context.VenueContract.Metadata); err != nil {
		return err
	}
	order := context.Aggregate.Order
	if context.Account.ID != context.Aggregate.Intent.AccountID || context.Account.ID != order.AccountID ||
		context.Instrument.ID != context.Aggregate.Intent.InstrumentID || context.Instrument.ID != order.InstrumentID ||
		context.VenueContract.InstrumentID != context.Instrument.ID || context.VenueContract.ID != order.VenueContractID ||
		context.Account.Venue != "kalshi" || context.VenueContract.Venue != "kalshi" || order.Venue != "kalshi" ||
		context.Account.BaseCurrency != context.VenueContract.Currency || order.ClientOrderID != order.ID.String() ||
		context.Route.Subaccount < 0 || context.Route.Subaccount > 32 || context.Route.ExchangeIndex != 0 {
		return fmt.Errorf("kalshi common lifecycle: canonical account, route, order, instrument, or contract context differs")
	}
	receivedAt := context.ReceivedAt.UTC().Truncate(time.Microsecond)
	if receivedAt.IsZero() || !receivedAt.Equal(context.ReceivedAt) {
		return fmt.Errorf("kalshi common lifecycle: receive time must use UTC microsecond precision")
	}
	return nil
}

func expectedKalshiBookSide(outcome string, side lifecycle.Side) string {
	if outcome == "yes" && side == lifecycle.SideBuy || outcome == "no" && side == lifecycle.SideSell {
		return "bid"
	}
	return "ask"
}

func parseKalshiTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC().Truncate(time.Microsecond), nil
}

func currentBindingID(aggregate *lifecycle.Aggregate) *uuid.UUID {
	if aggregate == nil || aggregate.Binding == nil {
		return nil
	}
	value := aggregate.Binding.ID
	return &value
}

func validOutcome(value string) string {
	if value == "yes" || value == "no" {
		return value
	}
	return ""
}

func validBookSide(value string) string {
	if value == "bid" || value == "ask" {
		return value
	}
	return ""
}

func validAction(value string) string {
	if value == "buy" || value == "sell" {
		return value
	}
	return ""
}

func optionalValidPrice(value decimal.Decimal) *decimal.Decimal {
	if value.IsNegative() || value.GreaterThan(decimal.NewFromInt(1)) {
		return nil
	}
	return &value
}

func sourceIdentity(value string, raw json.RawMessage) string {
	if strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	digest := sha256.Sum256(raw)
	return "malformed-fill-" + hex.EncodeToString(digest[:])
}

func localKalshiOrderSourceID(label, clientOrderID, revision string) string {
	digest := sha256.Sum256([]byte(clientOrderID + "\x1f" + label + "\x1f" + revision))
	return "local-response/kalshi/" + label + "/" + hex.EncodeToString(digest[:])
}

func sumKalshiFills(aggregate *lifecycle.Aggregate) decimal.Decimal {
	total := decimal.Zero
	for index := range aggregate.Fills {
		total = total.Add(aggregate.Fills[index].Quantity)
	}
	return total
}

func kalshiEventInput(observation *venue.Observation, reason string) lifecycle.EventInput {
	return lifecycle.EventInput{
		Source: "kalshi", SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
		SourceRevision: observation.SourceRevision, SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
		Actor: kalshiAdapterActor, ReasonCode: reason, Evidence: observation.RawPayload,
	}
}
