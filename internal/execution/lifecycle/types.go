// Package lifecycle defines the immutable common intent, order, and fill
// lifecycle used by simulation and venue adapters. Legacy mutable order models
// remain outside this package until an explicit adapter cutover.
package lifecycle

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
)

const (
	intentIDDomain = "execution-intent"
	eventIDDomain  = "execution-lifecycle-event"
)

// State is the replayed state of one intent and its single routed order.
type State string

const (
	StateNone                 State = ""
	StateProposed             State = "proposed"
	StateAllocated            State = "allocated"
	StateRiskApproved         State = "risk_approved"
	StateRiskRejected         State = "risk_rejected"
	StateRouted               State = "routed"
	StateWorking              State = "working"
	StatePartiallyFilled      State = "partially_filled"
	StateFilled               State = "filled"
	StateCancelled            State = "cancelled"
	StateExpired              State = "expired"
	StateRejected             State = "rejected"
	StateFailedReconciliation State = "failed_reconciliation"
)

// EventKind distinguishes an observation/command from the state it projects.
type EventKind string

const (
	EventIntentProposed          EventKind = "intent_proposed"
	EventIntentAllocated         EventKind = "intent_allocated"
	EventRiskApproved            EventKind = "risk_approved"
	EventRiskRejected            EventKind = "risk_rejected"
	EventOrderRouted             EventKind = "order_routed"
	EventOrderWorking            EventKind = "order_working"
	EventCancelRequested         EventKind = "cancel_requested"
	EventFillAcknowledged        EventKind = "fill_acknowledged"
	EventFillRecorded            EventKind = "fill_recorded"
	EventOrderCancelled          EventKind = "order_cancelled"
	EventOrderExpired            EventKind = "order_expired"
	EventOrderRejected           EventKind = "order_rejected"
	EventUnknownVenueState       EventKind = "unknown_venue_state"
	EventContradictoryVenueState EventKind = "contradictory_venue_state"
	EventFillCorrectionObserved  EventKind = "fill_correction_observed"
	EventFillBustObserved        EventKind = "fill_bust_observed"
)

// ObservationClass keeps correction/bust identity separate from ordinary
// event replay when a provider revises an execution in place.
type ObservationClass string

const (
	ObservationOrdinary   ObservationClass = "ordinary"
	ObservationCorrection ObservationClass = "correction"
	ObservationBust       ObservationClass = "bust"
)

// PolicyKind says whether the routed order is governed by a simulator or a
// venue policy. The version remains an immutable order/event fact.
type PolicyKind string

const (
	PolicySimulation PolicyKind = "simulation"
	PolicyVenue      PolicyKind = "venue"
)

// EventInput contains exact source evidence common to lifecycle commands.
type EventInput struct {
	Source                   string
	SourceNamespace          string
	SourceEventID            string
	SourceRevision           string
	ObservationClass         ObservationClass
	ObservationDiscriminator string
	SourceAt                 time.Time
	ReceivedAt               time.Time
	Actor                    string
	ReasonCode               string
	Reason                   string
	Evidence                 json.RawMessage
	OriginalFillID           *uuid.UUID
	OriginalSourceEventID    string
}

// Event is one immutable transition or command observation. IngestSequence is
// assigned by PostgreSQL and is not part of semantic replay equality.
type Event struct {
	ID                       uuid.UUID
	IngestSequence           int64
	IntentID                 uuid.UUID
	OrderID                  *uuid.UUID
	BindingID                *uuid.UUID
	FillID                   *uuid.UUID
	Kind                     EventKind
	ObservationClass         ObservationClass
	ObservationDiscriminator string
	PriorState               State
	NextState                State
	AccountID                uuid.UUID
	Environment              domain.AccountEnvironment
	OriginType               ledger.ExecutionOriginType
	OriginID                 string
	StrategyVersionID        string
	PolicyKind               PolicyKind
	PolicyVersion            string
	QuantityDelta            *decimal.Decimal
	CumulativeFillQuantity   *decimal.Decimal
	QuoteSnapshotID          *uuid.UUID
	Source                   string
	SourceNamespace          string
	SourceEventID            string
	SourceRevision           string
	SourceAt                 time.Time
	ReceivedAt               time.Time
	Actor                    string
	ReasonCode               string
	Reason                   string
	Evidence                 json.RawMessage
	EvidenceSHA256           string
	OriginalFillID           *uuid.UUID
	OriginalSourceEventID    string
	CreatedAt                time.Time
}

// Aggregate is a fully replayed intent lifecycle. Child order/fill types are
// added by the subsequent vertical slices.
type Aggregate struct {
	Intent            Intent
	State             State
	AllocatedQuantity *decimal.Decimal
	Order             *Order
	Binding           *OrderBinding
	Fills             []Fill
	Events            []Event
}

func newEvent(intent Intent, kind EventKind, prior, next State, input EventInput, createdAt time.Time) (Event, error) {
	observationClass := input.ObservationClass
	if observationClass == "" {
		observationClass = ObservationOrdinary
	}
	source := strings.ToLower(strings.TrimSpace(input.Source))
	namespace := strings.TrimSpace(input.SourceNamespace)
	sourceEventID := strings.TrimSpace(input.SourceEventID)
	revision := strings.TrimSpace(input.SourceRevision)
	discriminator := strings.TrimSpace(input.ObservationDiscriminator)
	actor := strings.TrimSpace(input.Actor)
	reasonCode := strings.ToLower(strings.TrimSpace(input.ReasonCode))
	reason := strings.TrimSpace(input.Reason)
	originalSourceEventID := strings.TrimSpace(input.OriginalSourceEventID)

	if source == "" || namespace == "" || sourceEventID == "" {
		return Event{}, fmt.Errorf("lifecycle event source identity is required")
	}
	if actor == "" || reasonCode == "" {
		return Event{}, fmt.Errorf("lifecycle event actor and reason code are required")
	}
	if input.SourceAt.IsZero() || input.ReceivedAt.IsZero() {
		return Event{}, fmt.Errorf("lifecycle event source and receive times are required")
	}
	sourceAt := normalizeTime(input.SourceAt)
	receivedAt := normalizeTime(input.ReceivedAt)
	if sourceAt.After(receivedAt) {
		return Event{}, fmt.Errorf("lifecycle event source time cannot follow receive time")
	}
	evidence := append(json.RawMessage(nil), input.Evidence...)
	if err := validateJSONObject(evidence, "lifecycle event evidence"); err != nil {
		return Event{}, err
	}
	if observationClass == ObservationOrdinary {
		if discriminator != "" || input.OriginalFillID != nil || originalSourceEventID != "" {
			return Event{}, fmt.Errorf("ordinary lifecycle event cannot carry correction identity")
		}
	} else {
		if observationClass != ObservationCorrection && observationClass != ObservationBust {
			return Event{}, fmt.Errorf("invalid lifecycle observation class %q", observationClass)
		}
		if discriminator == "" || input.OriginalFillID == nil || *input.OriginalFillID == uuid.Nil || originalSourceEventID == "" {
			return Event{}, fmt.Errorf("correction or bust observation requires discriminator and original fill identity")
		}
		if strings.HasPrefix(discriminator, "revision:") && (revision == "" || discriminator != "revision:"+revision) {
			return Event{}, fmt.Errorf("correction revision discriminator must match nonempty source revision")
		}
	}
	createdAt = normalizeTime(createdAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	evidenceHash := sha256.Sum256(evidence)
	event := Event{
		IntentID:                 intent.ID,
		Kind:                     kind,
		ObservationClass:         observationClass,
		ObservationDiscriminator: discriminator,
		PriorState:               prior,
		NextState:                next,
		AccountID:                intent.AccountID,
		Environment:              intent.Environment,
		OriginType:               intent.OriginType,
		OriginID:                 intent.OriginID,
		StrategyVersionID:        intent.StrategyVersionID,
		Source:                   source,
		SourceNamespace:          namespace,
		SourceEventID:            sourceEventID,
		SourceRevision:           revision,
		SourceAt:                 sourceAt,
		ReceivedAt:               receivedAt,
		Actor:                    actor,
		ReasonCode:               reasonCode,
		Reason:                   reason,
		Evidence:                 evidence,
		EvidenceSHA256:           hex.EncodeToString(evidenceHash[:]),
		OriginalFillID:           cloneUUID(input.OriginalFillID),
		OriginalSourceEventID:    originalSourceEventID,
		CreatedAt:                createdAt,
	}
	event.ID = economicid.DeterministicUUID(
		eventIDDomain,
		event.IntentID.String(),
		string(event.ObservationClass),
		event.Source,
		event.SourceNamespace,
		eventIdentitySourceEventID(event.ObservationClass, event.SourceEventID, event.OriginalSourceEventID),
		event.ObservationDiscriminator,
	)
	return event, nil
}

func eventIdentitySourceEventID(observationClass ObservationClass, sourceEventID, originalSourceEventID string) string {
	if observationClass == ObservationOrdinary {
		return sourceEventID
	}
	return originalSourceEventID
}

func validOrigin(value ledger.ExecutionOriginType) bool {
	switch value {
	case ledger.ExecutionOriginStrategyVersion, ledger.ExecutionOriginCopySubscription,
		ledger.ExecutionOriginPortfolioRebalance, ledger.ExecutionOriginRiskReduction,
		ledger.ExecutionOriginOperator, ledger.ExecutionOriginSettlement,
		ledger.ExecutionOriginReconciliation:
		return true
	default:
		return false
	}
}

func validExactDecimal(value decimal.Decimal, allowZero bool) bool {
	if (!allowZero && value.IsZero()) || !value.Equal(value.Round(12)) {
		return false
	}
	limit := decimal.New(1, 26)
	return value.Abs().LessThan(limit)
}

func validateJSONObject(value json.RawMessage, label string) error {
	if len(value) == 0 {
		return fmt.Errorf("%s must be a JSON object", label)
	}
	var object map[string]any
	decoder := json.NewDecoder(bytes.NewReader(value))
	decoder.UseNumber()
	if err := decoder.Decode(&object); err != nil || object == nil {
		return fmt.Errorf("%s must be a JSON object", label)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return fmt.Errorf("%s must contain exactly one JSON object", label)
	}
	return nil
}

func jsonObjectEqual(left, right json.RawMessage) bool {
	var leftValue, rightValue any
	leftDecoder := json.NewDecoder(bytes.NewReader(left))
	leftDecoder.UseNumber()
	rightDecoder := json.NewDecoder(bytes.NewReader(right))
	rightDecoder.UseNumber()
	if leftDecoder.Decode(&leftValue) != nil || rightDecoder.Decode(&rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
}

func normalizeTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC().Truncate(time.Microsecond)
}

func cloneUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneDecimal(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
