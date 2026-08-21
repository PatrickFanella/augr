package ledger

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const economicSourceEventIDDomain = "economic-source-event"

// EconomicSourceEvent is immutable provider/system evidence recorded before
// any canonical economic interpretation is attempted.
type EconomicSourceEvent struct {
	ID              uuid.UUID       `json:"id"`
	AccountID       uuid.UUID       `json:"account_id"`
	Source          string          `json:"source"`
	SourceNamespace string          `json:"source_namespace"`
	SourceEventID   string          `json:"source_event_id"`
	SourceRevision  string          `json:"source_revision"`
	ObservedAt      time.Time       `json:"observed_at"`
	RawPayload      json.RawMessage `json:"raw_payload"`
	PayloadSHA256   string          `json:"payload_sha256"`
	CreatedAt       time.Time       `json:"created_at"`
}

// EconomicSourceEventInput contains the stable source identity and exact wire
// evidence needed to record one raw event.
type EconomicSourceEventInput struct {
	AccountID       uuid.UUID
	Source          string
	SourceNamespace string
	SourceEventID   string
	SourceRevision  string
	ObservedAt      time.Time
	RawPayload      json.RawMessage
	CreatedAt       time.Time
}

// NewEconomicSourceEvent normalizes source identity without changing the raw
// JSON bytes. SourceRevision is evidence and is not part of durable identity.
func NewEconomicSourceEvent(input EconomicSourceEventInput) (*EconomicSourceEvent, error) {
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	rawPayload := append(json.RawMessage(nil), input.RawPayload...)
	payloadHash := sha256.Sum256(rawPayload)
	event := &EconomicSourceEvent{
		AccountID:       input.AccountID,
		Source:          strings.ToLower(strings.TrimSpace(input.Source)),
		SourceNamespace: strings.TrimSpace(input.SourceNamespace),
		SourceEventID:   strings.TrimSpace(input.SourceEventID),
		SourceRevision:  strings.TrimSpace(input.SourceRevision),
		ObservedAt:      input.ObservedAt.UTC().Truncate(time.Microsecond),
		RawPayload:      rawPayload,
		PayloadSHA256:   hex.EncodeToString(payloadHash[:]),
		CreatedAt:       createdAt,
	}
	event.ID = economicid.DeterministicUUID(
		economicSourceEventIDDomain,
		event.AccountID.String(),
		event.Source,
		event.SourceNamespace,
		event.SourceEventID,
	)
	if err := event.Validate(); err != nil {
		return nil, err
	}
	return event, nil
}

// Validate verifies exact raw evidence, normalized identity, timestamps, hash,
// and the deterministic source-event UUID.
func (event EconomicSourceEvent) Validate() error {
	if event.AccountID == uuid.Nil {
		return fmt.Errorf("economic source event account ID is required")
	}
	if event.Source == "" || event.Source != strings.ToLower(strings.TrimSpace(event.Source)) {
		return fmt.Errorf("economic source event source must be non-empty and normalized lowercase")
	}
	if !isNormalizedRequired(event.SourceNamespace) || !isNormalizedRequired(event.SourceEventID) {
		return fmt.Errorf("economic source event namespace and source event ID must be non-empty and normalized")
	}
	if event.SourceRevision != strings.TrimSpace(event.SourceRevision) {
		return fmt.Errorf("economic source event revision must be normalized")
	}
	if event.ObservedAt.IsZero() || event.ObservedAt.Location() != time.UTC || !hasPostgresTimestampPrecision(event.ObservedAt) {
		return fmt.Errorf("economic source event observed time must use normalized UTC microsecond precision")
	}
	if event.CreatedAt.IsZero() || event.CreatedAt.Location() != time.UTC || !hasPostgresTimestampPrecision(event.CreatedAt) {
		return fmt.Errorf("economic source event creation time must use normalized UTC microsecond precision")
	}
	if len(event.RawPayload) == 0 || !json.Valid(event.RawPayload) {
		return fmt.Errorf("economic source event raw payload must be valid JSON")
	}
	payloadHash := sha256.Sum256(event.RawPayload)
	if event.PayloadSHA256 != hex.EncodeToString(payloadHash[:]) {
		return fmt.Errorf("economic source event payload SHA-256 does not match raw bytes")
	}
	expectedID := economicid.DeterministicUUID(
		economicSourceEventIDDomain,
		event.AccountID.String(),
		event.Source,
		event.SourceNamespace,
		event.SourceEventID,
	)
	if event.ID != expectedID {
		return fmt.Errorf("economic source event ID does not match deterministic source identity")
	}
	return nil
}

// SameEconomicSourceEventPayload reports whether two rows are the same exact
// retry. CreatedAt is intentionally excluded; revision and wire bytes are not.
func SameEconomicSourceEventPayload(left, right *EconomicSourceEvent) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID &&
		left.AccountID == right.AccountID &&
		left.Source == right.Source &&
		left.SourceNamespace == right.SourceNamespace &&
		left.SourceEventID == right.SourceEventID &&
		left.SourceRevision == right.SourceRevision &&
		left.ObservedAt.Equal(right.ObservedAt) &&
		left.PayloadSHA256 == right.PayloadSHA256 &&
		bytes.Equal(left.RawPayload, right.RawPayload)
}
