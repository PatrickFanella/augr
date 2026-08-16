package simulation

import (
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

// VenueErrorCode is a stable fail-closed classification shared by canonical
// backtest and internal-paper adapters.
type VenueErrorCode string

const (
	VenueErrorInvalidRequest         VenueErrorCode = "invalid_request"
	VenueErrorPolicyMismatch         VenueErrorCode = "policy_mismatch"
	VenueErrorReferenceMismatch      VenueErrorCode = "reference_mismatch"
	VenueErrorUnsupportedInstruction VenueErrorCode = "unsupported_instruction"
	VenueErrorQuoteNotExecutable     VenueErrorCode = "quote_not_executable"
)

// VenueError retains a stable code while preserving a lower-level assessment
// error for diagnostics and errors.As/errors.Is callers.
type VenueError struct {
	Code   VenueErrorCode
	Detail string
	Cause  error
}

func (err *VenueError) Error() string {
	if err == nil {
		return ""
	}
	if err.Cause != nil {
		return string(err.Code) + ": " + err.Detail + ": " + err.Cause.Error()
	}
	return string(err.Code) + ": " + err.Detail
}

func (err *VenueError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

// Decision describes the deterministic result of one observation.
type Decision string

const (
	DecisionNoop            Decision = "noop"
	DecisionWaitingLatency  Decision = "waiting_latency"
	DecisionResting         Decision = "resting"
	DecisionPartiallyFilled Decision = "partially_filled"
	DecisionFilled          Decision = "filled"
	DecisionCancelled       Decision = "cancelled"
	DecisionExpired         Decision = "expired"
	DecisionRejected        Decision = "rejected"
)

// EvaluationRequest joins a routed lifecycle to exact account, reference, and
// point-in-time quote evidence.
type EvaluationRequest struct {
	Account       domain.Account
	Instrument    instrument.Instrument
	VenueContract instrument.VenueContract
	Aggregate     *lifecycle.Aggregate
	Snapshot      marketdata.QuoteSnapshot
	EvaluatedAt   time.Time
}

// Result contains only transitions already accepted by lifecycle.ApplyTransition.
type Result struct {
	Decision    Decision
	Aggregate   *lifecycle.Aggregate
	Transitions []*lifecycle.Transition
	Fills       []FillEffect
}

// Venue is one immutable content-addressed deterministic simulation venue.
type Venue struct{ policy *Policy }

// NewVenue validates policy identity before it can govern an order.
func NewVenue(policy *Policy) (*Venue, error) {
	if policy == nil {
		return nil, fmt.Errorf("simulation venue policy is required")
	}
	artifact, err := policy.NewArtifact(time.Unix(0, 0).UTC())
	if err != nil {
		return nil, fmt.Errorf("construct simulation venue: %w", err)
	}
	if _, err := PolicyFromArtifact(*artifact); err != nil {
		return nil, fmt.Errorf("construct simulation venue: %w", err)
	}
	return &Venue{policy: policy}, nil
}

// PolicyVersion returns the exact policy version required on routed orders.
func (venue *Venue) PolicyVersion() string {
	if venue == nil || venue.policy == nil {
		return ""
	}
	return venue.policy.Version()
}

// PolicyDigest returns the exact content digest for adapter parity checks.
func (venue *Venue) PolicyDigest() string {
	if venue == nil || venue.policy == nil {
		return ""
	}
	return venue.policy.Digest()
}

// Evaluate consumes one point-in-time observation without mutating request
// values. Every returned transition has been folded through the lifecycle.
func (venue *Venue) Evaluate(request EvaluationRequest) (*Result, error) {
	asset, routeSession, err := venue.validateRequest(request)
	if err != nil {
		return nil, err
	}
	current := request.Aggregate
	result := &Result{Decision: DecisionNoop, Aggregate: current}
	if terminalSimulationState(current.State) {
		return result, nil
	}

	if routeSession == nil && asset.Calendar.Kind == CalendarExplicitSessions {
		return venue.applyTerminal(result, request, lifecycle.EventOrderRejected, DecisionRejected,
			"route_outside_session", request.Aggregate.Order.RoutedAt, nil, nil)
	}
	if routeSession != nil && current.Order.TimeInForce == lifecycle.TimeInForceDay &&
		!request.EvaluatedAt.Before(routeSession.CloseAt) {
		return venue.applyTerminal(result, request, lifecycle.EventOrderExpired, DecisionExpired,
			"day_session_closed", routeSession.CloseAt, &request.Snapshot, routeSession)
	}

	if asset.Calendar.Kind == CalendarExplicitSessions && !simulationTimeInSession(asset.Calendar, request.EvaluatedAt) {
		if current.Order.TimeInForce == lifecycle.TimeInForceGTC {
			return venue.ensureWorking(result, request, asset, routeSession, DecisionResting, "outside_current_session")
		}
	}

	assessment, err := request.Snapshot.AssessForExecution(
		request.EvaluatedAt,
		asset.QuoteRequirements,
		request.Instrument,
		request.VenueContract,
	)
	if err != nil {
		return nil, venueFailure(VenueErrorQuoteNotExecutable, "quote assessment failed", err)
	}
	if assessment.SnapshotID != request.Snapshot.ID || !assessment.EvaluatedAt.Equal(request.EvaluatedAt) {
		return nil, venueFailure(VenueErrorQuoteNotExecutable, "quote assessment identity mismatch", nil)
	}
	latencyEligibleAt := current.Order.RoutedAt.Add(asset.FixedLatency)
	if request.Snapshot.AvailableAt == nil || request.Snapshot.AvailableAt.Before(latencyEligibleAt) {
		if current.Order.TimeInForce == lifecycle.TimeInForceIOC || current.Order.TimeInForce == lifecycle.TimeInForceFOK {
			result.Decision = DecisionWaitingLatency
			return result, nil
		}
		return venue.ensureWorking(result, request, asset, routeSession, DecisionWaitingLatency, "fixed_latency_pending")
	}

	levels := venue.eligibleLevels(current, request.Snapshot, asset, request.VenueContract.LotSize)
	remaining := current.Order.Quantity.Sub(simulationFilledQuantity(current))
	if current.Order.TimeInForce == lifecycle.TimeInForceFOK {
		capacity := decimal.Zero
		for _, level := range levels {
			capacity = capacity.Add(level.capacity)
		}
		if capacity.LessThan(remaining) {
			return venue.applyTerminal(result, request, lifecycle.EventOrderRejected, DecisionRejected,
				"fok_insufficient_depth", simulationSnapshotSourceAt(request.Snapshot), &request.Snapshot, routeSession)
		}
	}

	for _, candidate := range levels {
		if remaining.IsZero() {
			break
		}
		quantity := candidate.capacity
		if quantity.GreaterThan(remaining) {
			quantity = remaining
		}
		transition, effect, err := buildSimulationFill(simulationFillInput{
			Policy: venue.policy, Asset: asset, Account: request.Account,
			Instrument: request.Instrument, VenueContract: request.VenueContract,
			Aggregate: current, Snapshot: request.Snapshot, Assessment: *assessment,
			EvaluatedAt: request.EvaluatedAt, RouteSession: routeSession,
			Level: candidate.level, Capacity: candidate.capacity, Quantity: quantity,
			FirstFill: len(current.Fills) == 0,
		})
		if err != nil {
			return nil, err
		}
		current, err = lifecycle.ApplyTransition(current, transition)
		if err != nil {
			return nil, fmt.Errorf("apply simulation fill transition: %w", err)
		}
		result.Transitions = append(result.Transitions, transition)
		result.Fills = append(result.Fills, effect)
		result.Aggregate = current
		remaining = remaining.Sub(quantity)
	}

	if current.State == lifecycle.StateFilled {
		result.Decision = DecisionFilled
		return result, nil
	}
	if current.Order.TimeInForce == lifecycle.TimeInForceIOC {
		return venue.applyTerminal(result, EvaluationRequest{
			Account: request.Account, Instrument: request.Instrument, VenueContract: request.VenueContract,
			Aggregate: current, Snapshot: request.Snapshot, EvaluatedAt: request.EvaluatedAt,
		}, lifecycle.EventOrderCancelled, DecisionCancelled, "ioc_remainder_cancelled",
			simulationSnapshotSourceAt(request.Snapshot), &request.Snapshot, routeSession)
	}
	if len(result.Fills) > 0 {
		result.Decision = DecisionPartiallyFilled
		return result, nil
	}
	return venue.ensureWorking(result, request, asset, routeSession, DecisionResting, "no_marketable_depth")
}

func (venue *Venue) validateRequest(request EvaluationRequest) (AssetPolicy, *SessionWindow, error) {
	if venue == nil || venue.policy == nil || request.Aggregate == nil || request.Aggregate.Order == nil {
		return AssetPolicy{}, nil, venueFailure(VenueErrorInvalidRequest, "routed lifecycle and venue are required", nil)
	}
	evaluatedAt := request.EvaluatedAt
	if evaluatedAt.IsZero() || evaluatedAt.Location() != time.UTC || !evaluatedAt.Equal(evaluatedAt.Truncate(time.Microsecond)) ||
		evaluatedAt.Before(request.Aggregate.Order.RoutedAt) {
		return AssetPolicy{}, nil, venueFailure(VenueErrorInvalidRequest, "evaluation time is invalid", nil)
	}
	if err := request.Account.Validate(); err != nil {
		return AssetPolicy{}, nil, venueFailure(VenueErrorReferenceMismatch, "account is invalid", err)
	}
	if request.Account.Status != domain.AccountStatusActive {
		return AssetPolicy{}, nil, venueFailure(VenueErrorReferenceMismatch, "account is not active", nil)
	}
	if err := request.Instrument.Validate(); err != nil {
		return AssetPolicy{}, nil, venueFailure(VenueErrorReferenceMismatch, "instrument is invalid", err)
	}
	if err := request.VenueContract.Validate(); err != nil {
		return AssetPolicy{}, nil, venueFailure(VenueErrorReferenceMismatch, "venue contract is invalid", err)
	}
	aggregate := request.Aggregate
	order := aggregate.Order
	if err := aggregate.Intent.Validate(); err != nil {
		return AssetPolicy{}, nil, venueFailure(VenueErrorInvalidRequest, "execution intent is invalid", err)
	}
	if err := order.Validate(); err != nil {
		return AssetPolicy{}, nil, venueFailure(VenueErrorInvalidRequest, "execution order is invalid", err)
	}
	if aggregate.Intent.AccountID != request.Account.ID || aggregate.Intent.Environment != request.Account.Environment ||
		aggregate.Intent.InstrumentID != request.Instrument.ID || order.AccountID != request.Account.ID ||
		order.InstrumentID != request.Instrument.ID || order.VenueContractID != request.VenueContract.ID ||
		order.Venue != request.VenueContract.Venue || request.VenueContract.InstrumentID != request.Instrument.ID ||
		request.Instrument.PrimaryVenue != request.VenueContract.Venue ||
		request.Account.BaseCurrency != request.Instrument.Currency || request.Account.BaseCurrency != request.VenueContract.Currency ||
		!request.Instrument.TickSize.Equal(request.VenueContract.TickSize) ||
		!request.Instrument.LotSize.Equal(request.VenueContract.LotSize) ||
		!request.Instrument.Multiplier.Equal(request.VenueContract.Multiplier) {
		return AssetPolicy{}, nil, venueFailure(VenueErrorReferenceMismatch, "account, instrument, contract, venue, currency, or mechanics mismatch", nil)
	}
	if order.PolicyKind != lifecycle.PolicySimulation || order.PolicyVersion != venue.policy.Version() {
		return AssetPolicy{}, nil, venueFailure(VenueErrorPolicyMismatch, "order policy does not match simulation venue", nil)
	}
	asset, ok := venue.policy.AssetPolicy(request.Instrument.AssetClass)
	if !ok || !containsSimulationOrderType(asset.OrderTypes, order.OrderType) ||
		!containsSimulationTimeInForce(asset.TimeInForce, order.TimeInForce) {
		return AssetPolicy{}, nil, venueFailure(VenueErrorUnsupportedInstruction, "asset, order type, or time in force is unsupported", nil)
	}
	session, sessionErr := venue.policy.RouteSession(request.Instrument.AssetClass, order.RoutedAt)
	if sessionErr != nil {
		if asset.Calendar.Kind == CalendarExplicitSessions && strings.Contains(sessionErr.Error(), "outside explicit sessions") {
			return asset, nil, nil
		}
		return AssetPolicy{}, nil, venueFailure(VenueErrorUnsupportedInstruction, "route session cannot be resolved", sessionErr)
	}
	return asset, session, nil
}

type eligibleSimulationLevel struct {
	level    marketdata.DepthLevel
	capacity decimal.Decimal
}

func (venue *Venue) eligibleLevels(
	aggregate *lifecycle.Aggregate,
	snapshot marketdata.QuoteSnapshot,
	asset AssetPolicy,
	lotSize decimal.Decimal,
) []eligibleSimulationLevel {
	side := marketdata.DepthSideAsk
	if aggregate.Order.Side == lifecycle.SideSell {
		side = marketdata.DepthSideBid
	}
	levels := make([]eligibleSimulationLevel, 0, len(snapshot.Depth))
	for _, level := range snapshot.Depth {
		if level.Side != side || !simulationLevelCrosses(aggregate.Order, level.Price) {
			continue
		}
		sourceID := simulationFillSourceEventID(aggregate.Order.ID.String(), snapshot.ID.String(), level.Side, level.Level)
		if simulationAggregateHasSourceEvent(aggregate, sourceID) {
			continue
		}
		capacity := floorSimulationLot(level.Size.Mul(asset.MaxDepthParticipation), lotSize)
		if !capacity.IsPositive() {
			continue
		}
		levels = append(levels, eligibleSimulationLevel{level: level, capacity: capacity})
	}
	return levels
}

func (venue *Venue) ensureWorking(
	result *Result,
	request EvaluationRequest,
	_ AssetPolicy,
	session *SessionWindow,
	decision Decision,
	reason string,
) (*Result, error) {
	if result.Aggregate.State != lifecycle.StateRouted {
		result.Decision = decision
		return result, nil
	}
	sourceAt := simulationSnapshotSourceAt(request.Snapshot)
	evidence, err := marshalSimulationObservationEvidence(
		venue.policy, request.Account, result.Aggregate, &request.Snapshot,
		"working", reason, request.EvaluatedAt, sourceAt, request.Snapshot.ReceivedAt, session,
	)
	if err != nil {
		return nil, err
	}
	namespace, err := simulationSourceNamespace(request.Account, "order")
	if err != nil {
		return nil, err
	}
	transition, err := lifecycle.Acknowledge(
		result.Aggregate,
		simulationExternalOrderID(result.Aggregate.Order.ID.String()),
		lifecycle.EventInput{
			Source: "simulation", SourceNamespace: namespace,
			SourceEventID:  simulationObservationSourceEventID(result.Aggregate.Order.ID.String(), request.Snapshot.ID.String(), "working", reason),
			SourceRevision: request.Snapshot.SourceRevision, SourceAt: sourceAt,
			ReceivedAt: request.Snapshot.ReceivedAt, Actor: "simulation-venue",
			ReasonCode: reason, Evidence: evidence,
		},
		request.EvaluatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("build simulation acknowledgement: %w", err)
	}
	next, err := lifecycle.ApplyTransition(result.Aggregate, transition)
	if err != nil {
		return nil, fmt.Errorf("apply simulation acknowledgement: %w", err)
	}
	result.Aggregate = next
	result.Transitions = append(result.Transitions, transition)
	result.Decision = decision
	return result, nil
}

func (venue *Venue) applyTerminal(
	result *Result,
	request EvaluationRequest,
	kind lifecycle.EventKind,
	decision Decision,
	reason string,
	sourceAt time.Time,
	snapshot *marketdata.QuoteSnapshot,
	session *SessionWindow,
) (*Result, error) {
	current := result.Aggregate
	receivedAt := terminalReceivedAt(request, snapshot)
	if receivedAt.Before(sourceAt) {
		receivedAt = request.EvaluatedAt
	}
	evidence, err := marshalSimulationObservationEvidence(
		venue.policy, request.Account, current, snapshot, string(kind), reason,
		request.EvaluatedAt, sourceAt, receivedAt, session,
	)
	if err != nil {
		return nil, err
	}
	namespace, err := simulationSourceNamespace(request.Account, "order")
	if err != nil {
		return nil, err
	}
	snapshotID := ""
	revision := ""
	if snapshot != nil {
		snapshotID = snapshot.ID.String()
		revision = snapshot.SourceRevision
	}
	transition, err := lifecycle.ObserveOrderTerminal(current, kind, lifecycle.EventInput{
		Source: "simulation", SourceNamespace: namespace,
		SourceEventID:  simulationObservationSourceEventID(current.Order.ID.String(), snapshotID, string(kind), reason),
		SourceRevision: revision, SourceAt: sourceAt, ReceivedAt: receivedAt,
		Actor: "simulation-venue", ReasonCode: reason, Evidence: evidence,
	}, request.EvaluatedAt)
	if err != nil {
		return nil, fmt.Errorf("build simulation terminal transition: %w", err)
	}
	next, err := lifecycle.ApplyTransition(current, transition)
	if err != nil {
		return nil, fmt.Errorf("apply simulation terminal transition: %w", err)
	}
	result.Aggregate = next
	result.Transitions = append(result.Transitions, transition)
	result.Decision = decision
	return result, nil
}

func venueFailure(code VenueErrorCode, detail string, cause error) error {
	return &VenueError{Code: code, Detail: detail, Cause: cause}
}

func containsSimulationOrderType(values []lifecycle.OrderType, target lifecycle.OrderType) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsSimulationTimeInForce(values []lifecycle.TimeInForce, target lifecycle.TimeInForce) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func simulationTimeInSession(calendar CalendarPolicy, evaluatedAt time.Time) bool {
	for _, session := range calendar.Sessions {
		if !evaluatedAt.Before(session.OpenAt) && evaluatedAt.Before(session.CloseAt) {
			return true
		}
	}
	return false
}

func simulationLevelCrosses(order *lifecycle.Order, price decimal.Decimal) bool {
	if order.OrderType == lifecycle.OrderMarket {
		return true
	}
	if order.OrderType != lifecycle.OrderLimit || order.LimitPrice == nil {
		return false
	}
	if order.Side == lifecycle.SideBuy {
		return price.LessThanOrEqual(*order.LimitPrice)
	}
	return price.GreaterThanOrEqual(*order.LimitPrice)
}

func floorSimulationLot(value, lot decimal.Decimal) decimal.Decimal {
	if !value.IsPositive() || !lot.IsPositive() {
		return decimal.Zero
	}
	return value.Div(lot).Floor().Mul(lot)
}

func simulationFilledQuantity(aggregate *lifecycle.Aggregate) decimal.Decimal {
	total := decimal.Zero
	if aggregate == nil {
		return total
	}
	for _, fill := range aggregate.Fills {
		total = total.Add(fill.Quantity)
	}
	return total
}

func simulationAggregateHasSourceEvent(aggregate *lifecycle.Aggregate, sourceEventID string) bool {
	if aggregate == nil {
		return false
	}
	for _, fill := range aggregate.Fills {
		if fill.Source == "simulation" && fill.SourceEventID == sourceEventID {
			return true
		}
	}
	return false
}

func simulationSnapshotSourceAt(snapshot marketdata.QuoteSnapshot) time.Time {
	if snapshot.ExchangeAt != nil {
		return *snapshot.ExchangeAt
	}
	return snapshot.ReceivedAt
}

func terminalReceivedAt(request EvaluationRequest, snapshot *marketdata.QuoteSnapshot) time.Time {
	if snapshot != nil && !snapshot.ReceivedAt.IsZero() && snapshot.ReceivedAt.After(request.Aggregate.Order.RoutedAt) {
		return snapshot.ReceivedAt
	}
	return request.EvaluatedAt
}

func terminalSimulationState(state lifecycle.State) bool {
	switch state {
	case lifecycle.StateFilled, lifecycle.StateCancelled, lifecycle.StateExpired,
		lifecycle.StateRejected, lifecycle.StateRiskRejected, lifecycle.StateFailedReconciliation:
		return true
	default:
		return false
	}
}

func simulationExternalOrderID(orderID string) string {
	return "simulation/" + orderID
}
