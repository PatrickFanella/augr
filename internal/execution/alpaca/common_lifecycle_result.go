package alpaca

import (
	"context"
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
	alpacaOrderNamespace       = "alpaca/trading-api-v2/orders"
	alpacaSubmitNamespace      = "alpaca/trading-api-v2/orders/submit"
	alpacaTradeUpdateNamespace = "alpaca/trade_updates"
	alpacaAdapterActor         = "alpaca-adapter"
	alpacaFillNormalizer       = "alpaca-account-activity-fill-v1"
)

// CommonLifecycleContext pins the exact canonical state and reference facts
// against which one provider response is interpreted.
type CommonLifecycleContext struct {
	Policy        *venue.Policy
	Aggregate     *lifecycle.Aggregate
	Account       *domain.Account
	Instrument    *instrument.Instrument
	VenueContract *instrument.VenueContract
	ReceivedAt    time.Time
}

// TradeUpdate is one raw stream delivery. The stream can provide lifecycle
// notices, but never the authoritative economic fill boundary.
type TradeUpdate struct {
	Event      string
	Timestamp  string
	Order      CommonOrder
	RawPayload json.RawMessage
}

type tradeUpdateWire struct {
	Event     string      `json:"event"`
	Timestamp string      `json:"timestamp"`
	Order     CommonOrder `json:"order"`
}

// PersistCancellationThenDelete records the fixed local command before any
// provider mutation. A transport error returns the committed aggregate so the
// caller can safely retry without inventing provider confirmation.
func PersistCancellationThenDelete(
	ctx context.Context,
	store venue.CancellationPersistence,
	accountID uuid.UUID,
	aggregate *lifecycle.Aggregate,
	requestedAt time.Time,
	client *CommonLifecycleClient,
) (*lifecycle.Aggregate, error) {
	if client == nil || client.client == nil {
		return nil, fmt.Errorf("alpaca common lifecycle: cancellation client is required")
	}
	command, err := venue.NewCancellationCommand(aggregate, requestedAt)
	if err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: construct cancellation command: %w", err)
	}
	persisted, err := venue.PersistCancellationCommand(ctx, store, accountID, aggregate, command)
	if err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: persist cancellation command: %w", err)
	}
	if persisted.Binding == nil {
		return persisted, fmt.Errorf("alpaca common lifecycle: persisted cancellation has no binding")
	}
	if err := client.Cancel(ctx, persisted.Binding.ExternalOrderID); err != nil {
		return persisted, fmt.Errorf("alpaca common lifecycle: send persisted cancellation: %w", err)
	}
	return persisted, nil
}

// ParseTradeUpdate keeps the complete delivery bytes and parses only the
// exact fields needed for policy mapping and canonical identity checks.
func ParseTradeUpdate(raw json.RawMessage) (*TradeUpdate, error) {
	retained := append(json.RawMessage(nil), raw...)
	var wire tradeUpdateWire
	if err := decodeOneJSON(retained, &wire); err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: decode trade update: %w", err)
	}
	return &TradeUpdate{
		Event: wire.Event, Timestamp: wire.Timestamp, Order: wire.Order, RawPayload: retained,
	}, nil
}

// PlanOrderResult maps one submit or REST order response into an ordered,
// raw-first venue result. Invalid provider facts become explicit failure
// results when their JSON evidence remains journalable.
func PlanOrderResult(
	context CommonLifecycleContext,
	kind venue.ObservationKind,
	fact *CommonOrderFact,
) (*venue.Result, error) {
	if kind != venue.ObservationSubmitResponse && kind != venue.ObservationOrderSnapshot &&
		kind != venue.ObservationCancelResponse {
		return nil, fmt.Errorf("alpaca common lifecycle: unsupported order observation kind %q", kind)
	}
	if err := validateCommonLifecycleContext(context); err != nil {
		return nil, err
	}
	if fact == nil || len(fact.RawPayload) == 0 {
		return nil, fmt.Errorf("alpaca common lifecycle: order fact and raw payload are required")
	}
	var wire CommonOrder
	if err := decodeOneJSON(fact.RawPayload, &wire); err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: order response is not journalable JSON: %w", err)
	}
	if !reflect.DeepEqual(wire, fact.Order) {
		return nil, fmt.Errorf("alpaca common lifecycle: parsed order differs from retained evidence")
	}

	providerState := wire.Status
	mapped, known := context.Policy.Mapping(venue.MappingOrderStatus, providerState)
	malformed, contradiction := classifyCommonOrder(context, wire)
	if malformed {
		return planMalformedProviderResult(context, fact.RawPayload, kind, "malformed_order_response")
	}
	if contradiction {
		mapped = venue.OutcomeContradiction
	} else if !known {
		mapped = venue.OutcomeUnknownState
	}
	namespace := alpacaOrderNamespace
	identityLabel := "order"
	if kind == venue.ObservationSubmitResponse {
		namespace = alpacaSubmitNamespace
		identityLabel = "submit"
	}
	sourceAt, err := parseProviderTime(wire.UpdatedAt)
	if err != nil {
		return planMalformedProviderResult(context, fact.RawPayload, kind, "malformed_order_timestamp")
	}
	if sourceAt.After(context.ReceivedAt) {
		mapped = venue.OutcomeContradiction
		sourceAt = context.ReceivedAt
	}
	sourceEventID := localOrderSourceID(identityLabel, context.Aggregate.Order.ClientOrderID, wire.UpdatedAt)
	return planOrderLikeResult(
		context, kind, venue.MappingOrderStatus, providerState, mapped, wire,
		fact.RawPayload, namespace, sourceEventID, wire.UpdatedAt, sourceAt,
	)
}

// PlanTradeUpdateResult journals one stream delivery. Fill and partial-fill
// events intentionally stop at fill_notice; account activities are the sole
// source from which economic fill graphs can be built.
func PlanTradeUpdateResult(
	context CommonLifecycleContext,
	update *TradeUpdate,
) (*venue.Result, error) {
	if err := validateCommonLifecycleContext(context); err != nil {
		return nil, err
	}
	if update == nil || len(update.RawPayload) == 0 {
		return nil, fmt.Errorf("alpaca common lifecycle: trade update and raw payload are required")
	}
	var wire tradeUpdateWire
	if err := decodeOneJSON(update.RawPayload, &wire); err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: trade update is not journalable JSON: %w", err)
	}
	if wire.Event != update.Event || wire.Timestamp != update.Timestamp || !reflect.DeepEqual(wire.Order, update.Order) {
		return nil, fmt.Errorf("alpaca common lifecycle: parsed trade update differs from retained evidence")
	}
	malformed, contradiction := classifyCommonOrder(context, wire.Order)
	if malformed || wire.Event == "" || wire.Event != strings.TrimSpace(wire.Event) {
		return planMalformedProviderResult(context, update.RawPayload, venue.ObservationTradeUpdate, "malformed_trade_update")
	}
	mapped, known := context.Policy.Mapping(venue.MappingTradeUpdate, wire.Event)
	if contradiction {
		mapped = venue.OutcomeContradiction
	} else if !known {
		mapped = venue.OutcomeUnknownState
	}
	sourceAt, err := parseProviderTime(wire.Timestamp)
	if err != nil {
		return planMalformedProviderResult(context, update.RawPayload, venue.ObservationTradeUpdate, "malformed_trade_timestamp")
	}
	if sourceAt.After(context.ReceivedAt) {
		mapped = venue.OutcomeContradiction
		sourceAt = context.ReceivedAt
	}
	digest := sha256.Sum256([]byte(strings.Join([]string{
		context.Aggregate.Order.ClientOrderID, wire.Order.ID, wire.Event, wire.Timestamp,
	}, "\x1f")))
	sourceEventID := "local-response/alpaca/trade-update/" + hex.EncodeToString(digest[:])
	return planOrderLikeResult(
		context, venue.ObservationTradeUpdate, venue.MappingTradeUpdate, wire.Event, mapped,
		wire.Order, update.RawPayload, alpacaTradeUpdateNamespace, sourceEventID,
		wire.Timestamp, sourceAt,
	)
}

func planOrderLikeResult(
	context CommonLifecycleContext,
	kind venue.ObservationKind,
	_ venue.MappingNamespace,
	providerState string,
	mapped venue.MappedOutcome,
	providerOrder CommonOrder,
	raw json.RawMessage,
	namespace, sourceEventID, revision string,
	sourceAt time.Time,
) (*venue.Result, error) {
	current := context.Aggregate
	if current.Binding != nil && providerOrder.ID != current.Binding.ExternalOrderID {
		mapped = venue.OutcomeContradiction
	}
	if mapped == venue.OutcomeAcknowledge && current.Binding != nil &&
		(current.State == lifecycle.StateWorking || current.State == lifecycle.StatePartiallyFilled) {
		mapped = venue.OutcomeNoChange
	}
	if mapped == venue.OutcomeAcknowledge && current.State != lifecycle.StateRouted {
		mapped = venue.OutcomeContradiction
	}
	if mapped == venue.OutcomeFillNotice &&
		current.State != lifecycle.StateRouted && current.State != lifecycle.StateWorking &&
		current.State != lifecycle.StatePartiallyFilled {
		mapped = venue.OutcomeContradiction
	}

	bindingID := currentBindingID(current)
	action := providerOrder.Side
	if action != string(lifecycle.SideBuy) && action != string(lifecycle.SideSell) {
		action = ""
	}
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: context.Account.ID, IntentID: current.Intent.ID, OrderID: current.Order.ID,
		BindingID: bindingID, VenueContractID: context.VenueContract.ID,
		Provider: venue.ProviderAlpaca, Venue: "alpaca", PolicyVersion: context.Policy.Version(),
		Kind: kind, ProviderState: providerState, MappedOutcome: mapped,
		ExternalOrderID: providerOrder.ID, ClientOrderID: providerOrder.ClientOrderID,
		ProviderContractID: providerOrder.Symbol, ProviderAction: action,
		IdentityKind: venue.SourceIdentityLocalResponse, SourceNamespace: namespace,
		SourceEventID: sourceEventID, SourceRevision: revision,
		SourceAt: sourceAt, ReceivedAt: context.ReceivedAt, RawPayload: raw, CreatedAt: context.ReceivedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: construct order observation: %w", err)
	}

	step := venue.ResultStep{Observation: observation}
	transition, err := orderTransition(current, observation)
	if err != nil {
		return nil, err
	}
	step.Transition = transition
	final := current
	if transition != nil {
		final, err = lifecycle.ApplyTransition(current, transition)
		if err != nil {
			return nil, fmt.Errorf("alpaca common lifecycle: apply planned order transition: %w", err)
		}
	}
	return &venue.Result{Initial: current, Aggregate: final, Steps: []venue.ResultStep{step}}, nil
}

func orderTransition(
	aggregate *lifecycle.Aggregate,
	observation *venue.Observation,
) (*lifecycle.Transition, error) {
	input := lifecycle.EventInput{
		Source: "alpaca", SourceNamespace: observation.SourceNamespace,
		SourceEventID: observation.SourceEventID, SourceRevision: observation.SourceRevision,
		SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
		Actor: alpacaAdapterActor, Evidence: observation.RawPayload,
	}
	switch observation.MappedOutcome {
	case venue.OutcomeNoChange, venue.OutcomeFillNotice:
		return nil, nil
	case venue.OutcomeAcknowledge:
		input.ReasonCode = "provider_acknowledged"
		return lifecycle.Acknowledge(aggregate, observation.ExternalOrderID, input, observation.CreatedAt)
	case venue.OutcomeCancelled:
		input.ReasonCode = "provider_cancelled"
		return lifecycle.ObserveOrderTerminal(aggregate, lifecycle.EventOrderCancelled, input, observation.CreatedAt)
	case venue.OutcomeExpired:
		input.ReasonCode = "provider_expired"
		return lifecycle.ObserveOrderTerminal(aggregate, lifecycle.EventOrderExpired, input, observation.CreatedAt)
	case venue.OutcomeRejected:
		input.ReasonCode = "provider_rejected"
		return lifecycle.ObserveOrderTerminal(aggregate, lifecycle.EventOrderRejected, input, observation.CreatedAt)
	case venue.OutcomeUnknownState:
		input.ReasonCode = "unknown_provider_state"
		return lifecycle.FailReconciliation(aggregate, lifecycle.EventUnknownVenueState, input, observation.CreatedAt)
	case venue.OutcomeContradiction, venue.OutcomeMalformedObservation:
		input.ReasonCode = "contradictory_provider_state"
		return lifecycle.FailReconciliation(aggregate, lifecycle.EventContradictoryVenueState, input, observation.CreatedAt)
	default:
		return nil, fmt.Errorf("alpaca common lifecycle: unsupported mapped order outcome %q", observation.MappedOutcome)
	}
}

func planMalformedProviderResult(
	context CommonLifecycleContext,
	raw json.RawMessage,
	originalKind venue.ObservationKind,
	reason string,
) (*venue.Result, error) {
	if !json.Valid(raw) {
		return nil, fmt.Errorf("alpaca common lifecycle: malformed bytes cannot satisfy the JSON journal boundary")
	}
	digest := sha256.Sum256(raw)
	providerState := "malformed"
	sourceEventID := "local-response/alpaca/malformed/" + hex.EncodeToString(digest[:])
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: context.Account.ID, IntentID: context.Aggregate.Intent.ID, OrderID: context.Aggregate.Order.ID,
		BindingID: currentBindingID(context.Aggregate), VenueContractID: context.VenueContract.ID,
		Provider: venue.ProviderAlpaca, Venue: "alpaca", PolicyVersion: context.Policy.Version(),
		Kind: venue.ObservationMalformedResponse, ProviderState: providerState,
		MappedOutcome:   venue.OutcomeMalformedObservation,
		ExternalOrderID: boundExternalID(context.Aggregate), ClientOrderID: context.Aggregate.Order.ClientOrderID,
		IdentityKind:    venue.SourceIdentityLocalResponse,
		SourceNamespace: "alpaca/malformed/" + string(originalKind), SourceEventID: sourceEventID,
		SourceAt: context.ReceivedAt, ReceivedAt: context.ReceivedAt, RawPayload: raw, CreatedAt: context.ReceivedAt,
	})
	if err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: construct malformed observation: %w", err)
	}
	input := lifecycle.EventInput{
		Source: "alpaca", SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
		SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt, Actor: alpacaAdapterActor,
		ReasonCode: reason, Evidence: observation.RawPayload,
	}
	transition, err := lifecycle.FailReconciliation(
		context.Aggregate, lifecycle.EventContradictoryVenueState, input, observation.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: plan malformed failure: %w", err)
	}
	final, err := lifecycle.ApplyTransition(context.Aggregate, transition)
	if err != nil {
		return nil, fmt.Errorf("alpaca common lifecycle: apply malformed failure: %w", err)
	}
	return &venue.Result{
		Initial: context.Aggregate, Aggregate: final,
		Steps: []venue.ResultStep{{Observation: observation, Transition: transition}},
	}, nil
}

// PlanFillActivityResult maps ordered account activities. FILL is the sole
// economic source. Corrections, busts, unknowns, and contradictions append
// failure evidence without rewriting an existing fill graph.
func PlanFillActivityResult(
	context CommonLifecycleContext,
	facts []FillActivityFact,
) (*venue.Result, error) {
	if err := validateCommonLifecycleContext(context); err != nil {
		return nil, err
	}
	if len(facts) == 0 {
		return nil, fmt.Errorf("alpaca common lifecycle: at least one fill activity is required")
	}
	current := context.Aggregate
	result := &venue.Result{Initial: context.Aggregate, Aggregate: context.Aggregate}
	for index := range facts {
		step, next, err := planFillActivity(context, current, facts[index])
		if err != nil {
			return nil, fmt.Errorf("alpaca common lifecycle: plan activity %d: %w", index, err)
		}
		result.Steps = append(result.Steps, step)
		current = next
		result.Aggregate = next
		if current.State == lifecycle.StateFailedReconciliation {
			break
		}
	}
	return result, nil
}

func planFillActivity(
	context CommonLifecycleContext,
	current *lifecycle.Aggregate,
	fact FillActivityFact,
) (venue.ResultStep, *lifecycle.Aggregate, error) {
	var wire FillActivity
	if err := decodeOneJSON(fact.RawPayload, &wire); err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("activity is not journalable JSON: %w", err)
	}
	if !reflect.DeepEqual(wire, fact.Activity) {
		return venue.ResultStep{}, nil, fmt.Errorf("parsed activity differs from retained evidence")
	}
	activity := wire
	mapped, known := context.Policy.Mapping(venue.MappingAccountActivity, activity.ActivityType)
	malformed := activity.ID == "" || activity.ID != strings.TrimSpace(activity.ID) ||
		activity.ActivityType == "" || activity.ActivityType != strings.TrimSpace(activity.ActivityType) ||
		activity.OrderID == "" || activity.Symbol == "" || activity.ClientOrderID == "" ||
		activity.TransactionTime == ""
	if malformed {
		return planMalformedActivity(context, current, fact.RawPayload)
	}
	sourceAt, timeErr := parseProviderTime(activity.TransactionTime)
	contradiction := timeErr != nil || sourceAt.After(context.ReceivedAt) ||
		current.Binding != nil && activity.OrderID != current.Binding.ExternalOrderID ||
		activity.ClientOrderID != current.Order.ClientOrderID || activity.Symbol != context.VenueContract.ContractID ||
		activity.Side != string(current.Order.Side)
	if !known {
		mapped = venue.OutcomeUnknownState
	}
	switch mapped {
	case venue.OutcomeCorrection, venue.OutcomeBust:
		if _, found := findFillBySourceEventID(current, activity.OriginalActivityID); !found {
			mapped = venue.OutcomeContradiction
		}
	case venue.OutcomeFill:
		if err := validateFillActivityMechanics(context, current, activity); err != nil {
			contradiction = true
		}
	}
	if contradiction {
		mapped = venue.OutcomeContradiction
	}
	if timeErr != nil || sourceAt.After(context.ReceivedAt) {
		sourceAt = context.ReceivedAt
	}
	providerPrice := parsedOptionalNonnegativeDecimal(activity.Price)
	action := activity.Side
	if action != string(lifecycle.SideBuy) && action != string(lifecycle.SideSell) {
		action = ""
	}
	var kind venue.ObservationKind
	switch activity.ActivityType {
	case "trade_correct":
		kind = venue.ObservationCorrection
	case "trade_bust":
		kind = venue.ObservationBust
	default:
		kind = venue.ObservationFill
	}
	observation, err := venue.NewObservation(venue.ObservationInput{
		AccountID: context.Account.ID, IntentID: current.Intent.ID, OrderID: current.Order.ID,
		BindingID: currentBindingID(current), VenueContractID: context.VenueContract.ID,
		Provider: venue.ProviderAlpaca, Venue: "alpaca", PolicyVersion: context.Policy.Version(),
		Kind: kind, ProviderState: activity.ActivityType, MappedOutcome: mapped,
		ExternalOrderID: activity.OrderID, ClientOrderID: activity.ClientOrderID,
		ProviderContractID: activity.Symbol, ProviderAction: action, ProviderPrice: providerPrice,
		IdentityKind: venue.SourceIdentityProvider, SourceNamespace: context.Policy.AuthoritativeFillNamespace(),
		SourceEventID: activity.ID, SourceAt: sourceAt, ReceivedAt: context.ReceivedAt,
		RawPayload: fact.RawPayload, CreatedAt: context.ReceivedAt,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct activity observation: %w", err)
	}
	if mapped == venue.OutcomeFill {
		return planEconomicFill(context, current, activity, observation)
	}
	return planNonEconomicActivity(current, activity, observation)
}

func planEconomicFill(
	context CommonLifecycleContext,
	current *lifecycle.Aggregate,
	activity FillActivity,
	observation *venue.Observation,
) (venue.ResultStep, *lifecycle.Aggregate, error) {
	quantity, _ := decimal.NewFromString(activity.Quantity)
	price, _ := decimal.NewFromString(activity.Price)
	sourceEvent, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: context.Account.ID, Source: "alpaca",
		SourceNamespace: observation.SourceNamespace, SourceEventID: observation.SourceEventID,
		SourceRevision: observation.SourceRevision, ObservedAt: observation.ReceivedAt,
		RawPayload: observation.RawPayload, CreatedAt: observation.CreatedAt,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct economic source event: %w", err)
	}
	fillID := lifecycle.FillID(current.Order.ID, sourceEvent.ID)
	side := ledger.FillSideBuy
	if current.Order.Side == lifecycle.SideSell {
		side = ledger.FillSideSell
	}
	var cost *ledger.CostComponent
	if activity.Commission != "" {
		commission, parseErr := decimal.NewFromString(activity.Commission)
		if parseErr != nil || commission.IsNegative() {
			return venue.ResultStep{}, nil, fmt.Errorf("commission is invalid")
		}
		if commission.IsPositive() {
			cost = &ledger.CostComponent{Kind: ledger.CostKindFee, Currency: context.Account.BaseCurrency, Amount: commission}
		}
	}
	normalization, err := ledger.NewFillEconomicNormalization(ledger.FillEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent: sourceEvent, Account: context.Account,
			NormalizerVersion:   alpacaFillNormalizer + "/" + context.Policy.Version(),
			ExecutionOriginType: current.Intent.OriginType, ExecutionOriginID: current.Intent.OriginID,
			ReferenceType: "execution_fill", ReferenceID: fillID.String(), EffectiveAt: observation.SourceAt,
		},
		Instrument: *context.Instrument, VenueContract: *context.VenueContract,
		Side: side, Quantity: quantity, Price: price, Cost: cost,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct fill normalization: %w", err)
	}
	input := lifecycle.EventInput{
		Source: "alpaca", SourceNamespace: observation.SourceNamespace,
		SourceEventID: observation.SourceEventID, SourceRevision: observation.SourceRevision,
		SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
		Actor: alpacaAdapterActor, ReasonCode: "authoritative_fill_reported", Evidence: observation.RawPayload,
	}
	transition, err := lifecycle.RecordFill(current, lifecycle.FillInput{
		Normalization: normalization, ExternalOrderID: observation.ExternalOrderID,
		Event: input, CreatedAt: observation.CreatedAt,
	})
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct lifecycle fill: %w", err)
	}
	next, err := lifecycle.ApplyTransition(current, transition)
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("apply lifecycle fill: %w", err)
	}
	return venue.ResultStep{
		Observation: observation, EconomicSourceEvent: sourceEvent, Transition: transition,
	}, next, nil
}

func planNonEconomicActivity(
	current *lifecycle.Aggregate,
	activity FillActivity,
	observation *venue.Observation,
) (venue.ResultStep, *lifecycle.Aggregate, error) {
	input := lifecycle.EventInput{
		Source: "alpaca", SourceNamespace: observation.SourceNamespace,
		SourceEventID: observation.SourceEventID, SourceRevision: observation.SourceRevision,
		SourceAt: observation.SourceAt, ReceivedAt: observation.ReceivedAt,
		Actor: alpacaAdapterActor, Evidence: observation.RawPayload,
	}
	var (
		transition *lifecycle.Transition
		err        error
	)
	switch observation.MappedOutcome {
	case venue.OutcomeCorrection, venue.OutcomeBust:
		fill, found := findFillBySourceEventID(current, activity.OriginalActivityID)
		if !found {
			return venue.ResultStep{}, nil, fmt.Errorf("original fill is missing")
		}
		input.OriginalFillID = &fill.ID
		input.OriginalSourceEventID = fill.SourceEventID
		input.ObservationDiscriminator = "activity:" + activity.ID
		kind := lifecycle.EventFillCorrectionObserved
		input.ObservationClass = lifecycle.ObservationCorrection
		input.ReasonCode = "fill_correction_reported"
		if observation.MappedOutcome == venue.OutcomeBust {
			kind = lifecycle.EventFillBustObserved
			input.ObservationClass = lifecycle.ObservationBust
			input.ReasonCode = "fill_bust_reported"
		}
		transition, err = lifecycle.FailReconciliation(current, kind, input, observation.CreatedAt)
	case venue.OutcomeUnknownState:
		input.ReasonCode = "unknown_account_activity"
		transition, err = lifecycle.FailReconciliation(current, lifecycle.EventUnknownVenueState, input, observation.CreatedAt)
	case venue.OutcomeContradiction, venue.OutcomeMalformedObservation:
		input.ReasonCode = "contradictory_account_activity"
		transition, err = lifecycle.FailReconciliation(current, lifecycle.EventContradictoryVenueState, input, observation.CreatedAt)
	default:
		return venue.ResultStep{}, nil, fmt.Errorf("unsupported non-economic activity outcome %q", observation.MappedOutcome)
	}
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("construct non-economic activity failure: %w", err)
	}
	next, err := lifecycle.ApplyTransition(current, transition)
	if err != nil {
		return venue.ResultStep{}, nil, fmt.Errorf("apply non-economic activity failure: %w", err)
	}
	return venue.ResultStep{Observation: observation, Transition: transition}, next, nil
}

func planMalformedActivity(
	context CommonLifecycleContext,
	current *lifecycle.Aggregate,
	raw json.RawMessage,
) (venue.ResultStep, *lifecycle.Aggregate, error) {
	malformedContext := context
	malformedContext.Aggregate = current
	result, err := planMalformedProviderResult(
		malformedContext, raw, venue.ObservationFill, "malformed_account_activity",
	)
	if err != nil {
		return venue.ResultStep{}, nil, err
	}
	return result.Steps[0], result.Aggregate, nil
}

func validateFillActivityMechanics(
	context CommonLifecycleContext,
	current *lifecycle.Aggregate,
	activity FillActivity,
) error {
	quantity, err := decimal.NewFromString(activity.Quantity)
	if err != nil || !validCommonDecimal(quantity, false) || !quantity.IsPositive() ||
		!quantity.Mod(context.VenueContract.LotSize).IsZero() {
		return fmt.Errorf("fill quantity is invalid or off lot")
	}
	price, err := decimal.NewFromString(activity.Price)
	if err != nil || !validCommonDecimal(price, true) || price.IsNegative() ||
		!price.Mod(context.VenueContract.TickSize).IsZero() {
		return fmt.Errorf("fill price is invalid or off tick")
	}
	cumulative, err := decimal.NewFromString(activity.CumulativeQuantity)
	if err != nil || cumulative.IsNegative() {
		return fmt.Errorf("fill cumulative quantity is invalid")
	}
	leaves, err := decimal.NewFromString(activity.LeavesQuantity)
	if err != nil || leaves.IsNegative() {
		return fmt.Errorf("fill leaves quantity is invalid")
	}
	expectedCumulative := sumAggregateFills(current).Add(quantity)
	if !cumulative.Equal(expectedCumulative) || !leaves.Equal(current.Order.Quantity.Sub(expectedCumulative)) ||
		!cumulative.Add(leaves).Equal(current.Order.Quantity) {
		return fmt.Errorf("fill cumulative or leaves facts contradict canonical quantity")
	}
	if activity.Commission != "" {
		commission, parseErr := decimal.NewFromString(activity.Commission)
		if parseErr != nil || commission.IsNegative() || !validCommonDecimal(commission, true) {
			return fmt.Errorf("fill commission is invalid")
		}
	}
	return nil
}

func classifyCommonOrder(context CommonLifecycleContext, order CommonOrder) (malformed, contradiction bool) {
	if order.ID == "" || order.ClientOrderID == "" || order.Symbol == "" || order.Side == "" ||
		order.Type == "" || order.TimeInForce == "" || order.Quantity == "" || order.FilledQuantity == "" ||
		order.Status == "" || order.UpdatedAt == "" {
		return true, false
	}
	for _, value := range []string{
		order.ID, order.ClientOrderID, order.Symbol, order.Side, order.Type,
		order.TimeInForce, order.Quantity, order.FilledQuantity, order.Status, order.UpdatedAt,
	} {
		if value != strings.TrimSpace(value) {
			return true, false
		}
	}
	quantity, quantityErr := decimal.NewFromString(order.Quantity)
	filled, filledErr := decimal.NewFromString(order.FilledQuantity)
	if quantityErr != nil || filledErr != nil || quantity.IsNegative() || filled.IsNegative() || filled.GreaterThan(quantity) {
		return true, false
	}
	if order.FilledAvgPrice != "" {
		average, err := decimal.NewFromString(order.FilledAvgPrice)
		if err != nil || average.IsNegative() {
			return true, false
		}
	}
	if order.ClientOrderID != context.Aggregate.Order.ClientOrderID || order.Symbol != context.VenueContract.ContractID ||
		order.Side != string(context.Aggregate.Order.Side) || order.Type != string(context.Aggregate.Order.OrderType) ||
		order.TimeInForce != string(context.Aggregate.Order.TimeInForce) ||
		!quantity.Equal(context.Aggregate.Order.Quantity) || order.ReplacedBy != "" || order.Replaces != "" {
		return false, true
	}
	if context.Aggregate.Binding != nil && order.ID != context.Aggregate.Binding.ExternalOrderID {
		return false, true
	}
	return false, false
}

func validateCommonLifecycleContext(context CommonLifecycleContext) error {
	if context.Policy == nil || context.Aggregate == nil || context.Aggregate.Order == nil ||
		context.Account == nil || context.Instrument == nil || context.VenueContract == nil {
		return fmt.Errorf("alpaca common lifecycle: policy, aggregate, account, instrument, and contract are required")
	}
	if context.Policy.Provider() != venue.ProviderAlpaca || context.Policy.Venue() != "alpaca" ||
		context.Aggregate.Order.PolicyKind != lifecycle.PolicyVenue ||
		context.Aggregate.Order.PolicyVersion != context.Policy.Version() {
		return fmt.Errorf("alpaca common lifecycle: exact reviewed Alpaca policy is required")
	}
	if err := context.Account.Validate(); err != nil || context.Account.Status != domain.AccountStatusActive {
		return fmt.Errorf("alpaca common lifecycle: account is not active and valid: %w", err)
	}
	if err := context.Instrument.Validate(); err != nil || context.Instrument.Status != instrument.StatusActive {
		return fmt.Errorf("alpaca common lifecycle: instrument is not active and valid: %w", err)
	}
	if err := context.VenueContract.Validate(); err != nil {
		return fmt.Errorf("alpaca common lifecycle: venue contract is invalid: %w", err)
	}
	order := context.Aggregate.Order
	if context.Account.ID != context.Aggregate.Intent.AccountID || context.Account.ID != order.AccountID ||
		context.Instrument.ID != context.Aggregate.Intent.InstrumentID || context.Instrument.ID != order.InstrumentID ||
		context.VenueContract.InstrumentID != context.Instrument.ID || context.VenueContract.ID != order.VenueContractID ||
		context.Account.Venue != "alpaca" || context.VenueContract.Venue != "alpaca" || order.Venue != "alpaca" ||
		context.Account.BaseCurrency != context.VenueContract.Currency || order.ClientOrderID != order.ID.String() {
		return fmt.Errorf("alpaca common lifecycle: canonical account, order, instrument, or contract context differs")
	}
	receivedAt := context.ReceivedAt.UTC().Truncate(time.Microsecond)
	if receivedAt.IsZero() || !receivedAt.Equal(context.ReceivedAt) {
		return fmt.Errorf("alpaca common lifecycle: receive time must use UTC microsecond precision")
	}
	return nil
}

func parseProviderTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	parsed = parsed.UTC().Truncate(time.Microsecond)
	if parsed.IsZero() {
		return time.Time{}, fmt.Errorf("provider time is required")
	}
	return parsed, nil
}

func localOrderSourceID(label, clientOrderID, revision string) string {
	identity := clientOrderID
	if label != "submit" {
		identity += "\x1f" + revision
	}
	digest := sha256.Sum256([]byte(identity))
	return "local-response/alpaca/" + label + "/" + hex.EncodeToString(digest[:])
}

func currentBindingID(aggregate *lifecycle.Aggregate) *uuid.UUID {
	if aggregate == nil || aggregate.Binding == nil {
		return nil
	}
	value := aggregate.Binding.ID
	return &value
}

func boundExternalID(aggregate *lifecycle.Aggregate) string {
	if aggregate == nil || aggregate.Binding == nil {
		return ""
	}
	return aggregate.Binding.ExternalOrderID
}

func parsedOptionalNonnegativeDecimal(value string) *decimal.Decimal {
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.IsNegative() {
		return nil
	}
	return &parsed
}

func sumAggregateFills(aggregate *lifecycle.Aggregate) decimal.Decimal {
	total := decimal.Zero
	for index := range aggregate.Fills {
		total = total.Add(aggregate.Fills[index].Quantity)
	}
	return total
}

func findFillBySourceEventID(aggregate *lifecycle.Aggregate, sourceEventID string) (lifecycle.Fill, bool) {
	for index := range aggregate.Fills {
		if aggregate.Fills[index].SourceEventID == sourceEventID {
			return aggregate.Fills[index], true
		}
	}
	return lifecycle.Fill{}, false
}
