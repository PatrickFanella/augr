package lifecycle

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/marketdata"
)

// Intent is the immutable desired economic change and decision evidence. It is
// deliberately not a broker order.
type Intent struct {
	ID                      uuid.UUID
	AccountID               uuid.UUID
	Environment             domain.AccountEnvironment
	InstrumentID            uuid.UUID
	IdempotencyKey          string
	DesiredQuantityDelta    decimal.Decimal
	DecisionQuoteSnapshotID uuid.UUID
	DecisionAt              time.Time
	OriginType              ledger.ExecutionOriginType
	OriginID                string
	StrategyVersionID       string
	Metadata                json.RawMessage
	CreatedAt               time.Time
}

// ProposeInput contains canonical reference facts and exact source evidence for
// the initial lifecycle proposal.
type ProposeInput struct {
	Account              domain.Account
	Instrument           instrument.Instrument
	DecisionSnapshot     marketdata.QuoteSnapshot
	IdempotencyKey       string
	DesiredQuantityDelta decimal.Decimal
	DecisionAt           time.Time
	OriginType           ledger.ExecutionOriginType
	OriginID             string
	StrategyVersionID    string
	Metadata             json.RawMessage
	Event                EventInput
	CreatedAt            time.Time
}

// Propose creates one deterministic intent and its required initial event.
func Propose(input ProposeInput) (*Aggregate, error) {
	if err := input.Account.Validate(); err != nil {
		return nil, fmt.Errorf("propose execution intent: invalid account: %w", err)
	}
	if input.Account.Status != domain.AccountStatusActive {
		return nil, fmt.Errorf("propose execution intent: account must be active")
	}
	if err := input.Instrument.Validate(); err != nil {
		return nil, fmt.Errorf("propose execution intent: invalid instrument: %w", err)
	}
	if input.Instrument.Status != instrument.StatusActive {
		return nil, fmt.Errorf("propose execution intent: instrument must be active")
	}
	if err := input.DecisionSnapshot.Validate(); err != nil {
		return nil, fmt.Errorf("propose execution intent: invalid decision snapshot: %w", err)
	}
	if input.DecisionSnapshot.InstrumentID != input.Instrument.ID {
		return nil, fmt.Errorf("propose execution intent: decision snapshot instrument mismatch")
	}
	decisionAt := normalizeTime(input.DecisionAt)
	if decisionAt.IsZero() {
		return nil, fmt.Errorf("propose execution intent: decision time is required")
	}
	if input.Instrument.Expiration != nil && !decisionAt.Before(*input.Instrument.Expiration) {
		return nil, fmt.Errorf("propose execution intent: instrument is expired at decision time")
	}
	if input.DecisionSnapshot.AvailableAt == nil {
		return nil, fmt.Errorf("propose execution intent: decision snapshot availability is required")
	}
	if input.DecisionSnapshot.AvailableAt.After(decisionAt) {
		return nil, fmt.Errorf("propose execution intent: decision snapshot was not available")
	}
	if !validExactDecimal(input.DesiredQuantityDelta, false) {
		return nil, fmt.Errorf("propose execution intent: desired quantity must be nonzero with at most 12 fractional and 26 integer digits")
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	originID := strings.TrimSpace(input.OriginID)
	strategyVersionID := strings.TrimSpace(input.StrategyVersionID)
	if idempotencyKey == "" {
		return nil, fmt.Errorf("propose execution intent: idempotency key is required")
	}
	if !validOrigin(input.OriginType) || originID == "" {
		return nil, fmt.Errorf("propose execution intent: execution origin is required")
	}
	if input.OriginType == ledger.ExecutionOriginStrategyVersion && strategyVersionID != originID {
		return nil, fmt.Errorf("propose execution intent: strategy origin must match strategy version")
	}
	if err := validateJSONObject(input.Metadata, "execution intent metadata"); err != nil {
		return nil, err
	}
	createdAt := normalizeTime(input.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	intent := Intent{
		AccountID:               input.Account.ID,
		Environment:             input.Account.Environment,
		InstrumentID:            input.Instrument.ID,
		IdempotencyKey:          idempotencyKey,
		DesiredQuantityDelta:    input.DesiredQuantityDelta,
		DecisionQuoteSnapshotID: input.DecisionSnapshot.ID,
		DecisionAt:              decisionAt,
		OriginType:              input.OriginType,
		OriginID:                originID,
		StrategyVersionID:       strategyVersionID,
		Metadata:                append(json.RawMessage(nil), input.Metadata...),
		CreatedAt:               createdAt,
	}
	intent.ID = economicid.DeterministicUUID(intentIDDomain, intent.AccountID.String(), intent.IdempotencyKey)
	if err := intent.Validate(); err != nil {
		return nil, err
	}
	event, err := newEvent(intent, EventIntentProposed, StateNone, StateProposed, input.Event, createdAt)
	if err != nil {
		return nil, fmt.Errorf("propose execution intent: %w", err)
	}
	event.QuantityDelta = cloneDecimal(&intent.DesiredQuantityDelta)
	quoteID := intent.DecisionQuoteSnapshotID
	event.QuoteSnapshotID = &quoteID
	if err := event.Validate(); err != nil {
		return nil, fmt.Errorf("propose execution intent: %w", err)
	}
	return &Aggregate{
		Intent: intent,
		State:  StateProposed,
		Events: []Event{event},
	}, nil
}

// Validate checks normalized intent shape independently of reference lookups.
func (intent Intent) Validate() error {
	if intent.ID == uuid.Nil || intent.AccountID == uuid.Nil || intent.InstrumentID == uuid.Nil || intent.DecisionQuoteSnapshotID == uuid.Nil {
		return fmt.Errorf("execution intent IDs are required")
	}
	if !intent.Environment.IsValid() {
		return fmt.Errorf("execution intent environment is invalid")
	}
	if strings.TrimSpace(intent.IdempotencyKey) == "" || intent.IdempotencyKey != strings.TrimSpace(intent.IdempotencyKey) {
		return fmt.Errorf("execution intent idempotency key must be normalized")
	}
	if !validExactDecimal(intent.DesiredQuantityDelta, false) {
		return fmt.Errorf("execution intent desired quantity is invalid")
	}
	if intent.DecisionAt.IsZero() || intent.DecisionAt.Location() != time.UTC || !intent.DecisionAt.Equal(intent.DecisionAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("execution intent decision time must use UTC microsecond precision")
	}
	if !validOrigin(intent.OriginType) || intent.OriginID == "" || intent.OriginID != strings.TrimSpace(intent.OriginID) {
		return fmt.Errorf("execution intent origin is invalid")
	}
	if intent.StrategyVersionID != strings.TrimSpace(intent.StrategyVersionID) ||
		(intent.OriginType == ledger.ExecutionOriginStrategyVersion && intent.StrategyVersionID != intent.OriginID) {
		return fmt.Errorf("execution intent strategy version is invalid")
	}
	if err := validateJSONObject(intent.Metadata, "execution intent metadata"); err != nil {
		return err
	}
	if intent.CreatedAt.IsZero() || intent.CreatedAt.Location() != time.UTC || !intent.CreatedAt.Equal(intent.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("execution intent creation time must use UTC microsecond precision")
	}
	expectedID := economicid.DeterministicUUID(intentIDDomain, intent.AccountID.String(), intent.IdempotencyKey)
	if intent.ID != expectedID {
		return fmt.Errorf("execution intent ID does not match deterministic identity")
	}
	return nil
}

// SameIntentPayload reports semantic retry equality. Local persistence time is
// evidence rather than part of the durable idempotency payload.
func SameIntentPayload(left, right *Intent) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID &&
		left.AccountID == right.AccountID &&
		left.Environment == right.Environment &&
		left.InstrumentID == right.InstrumentID &&
		left.IdempotencyKey == right.IdempotencyKey &&
		left.DesiredQuantityDelta.Equal(right.DesiredQuantityDelta) &&
		left.DecisionQuoteSnapshotID == right.DecisionQuoteSnapshotID &&
		left.DecisionAt.Equal(right.DecisionAt) &&
		left.OriginType == right.OriginType &&
		left.OriginID == right.OriginID &&
		left.StrategyVersionID == right.StrategyVersionID &&
		jsonObjectEqual(left.Metadata, right.Metadata)
}
