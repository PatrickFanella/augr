package ledger

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const markObservationIDDomain = "canonical-mark-observation"

// MarkObservation is one immutable, canonical instrument-price observation.
// SourceRevision is evidence, not identity: a correction must use a distinct
// SourceObservationID.
type MarkObservation struct {
	ID                  uuid.UUID       `json:"id"`
	InstrumentID        uuid.UUID       `json:"instrument_id"`
	Price               decimal.Decimal `json:"price"`
	PriceCurrency       string          `json:"price_currency"`
	Source              string          `json:"source"`
	SourceNamespace     string          `json:"source_namespace"`
	SourceObservationID string          `json:"source_observation_id"`
	SourceRevision      string          `json:"source_revision"`
	EffectiveAt         time.Time       `json:"effective_at"`
	ObservedAt          time.Time       `json:"observed_at"`
	Metadata            json.RawMessage `json:"metadata"`
	CreatedAt           time.Time       `json:"created_at"`
}

// MarkObservationInput contains exact provider evidence for a canonical mark.
type MarkObservationInput struct {
	InstrumentID        uuid.UUID
	Price               decimal.Decimal
	PriceCurrency       string
	Source              string
	SourceNamespace     string
	SourceObservationID string
	SourceRevision      string
	EffectiveAt         time.Time
	ObservedAt          time.Time
	Metadata            json.RawMessage
}

// NewMarkObservation normalizes one canonical observation and derives its
// source-identity UUID. It deliberately preserves the supplied JSON bytes.
func NewMarkObservation(input MarkObservationInput) (*MarkObservation, error) {
	metadata, err := normalizeJSONObject(input.Metadata, "mark metadata")
	if err != nil {
		return nil, err
	}
	mark := &MarkObservation{
		InstrumentID:        input.InstrumentID,
		Price:               input.Price,
		PriceCurrency:       strings.ToUpper(strings.TrimSpace(input.PriceCurrency)),
		Source:              strings.ToLower(strings.TrimSpace(input.Source)),
		SourceNamespace:     strings.TrimSpace(input.SourceNamespace),
		SourceObservationID: strings.TrimSpace(input.SourceObservationID),
		SourceRevision:      strings.TrimSpace(input.SourceRevision),
		EffectiveAt:         input.EffectiveAt.UTC().Truncate(time.Microsecond),
		ObservedAt:          input.ObservedAt.UTC().Truncate(time.Microsecond),
		Metadata:            metadata,
		CreatedAt:           time.Now().UTC().Truncate(time.Microsecond),
	}
	mark.ID = markObservationID(mark)
	if err := mark.Validate(); err != nil {
		return nil, err
	}
	return mark, nil
}

// Validate checks the exact schema-69 canonical mark contract, including its
// deterministic identity. Instrument currency equality is checked by the
// repository/database because that reference fact is not duplicated here.
func (mark MarkObservation) Validate() error {
	if mark.ID == uuid.Nil || mark.InstrumentID == uuid.Nil {
		return fmt.Errorf("mark and instrument IDs are required")
	}
	if mark.Price.IsNegative() {
		return fmt.Errorf("mark price cannot be negative")
	}
	if !validProjectionDecimal(mark.Price) {
		return fmt.Errorf("mark price supports at most 12 fractional and 26 integer digits")
	}
	if !isCurrencyUnit(mark.PriceCurrency) {
		return fmt.Errorf("mark price currency %q must be a normalized three-letter code", mark.PriceCurrency)
	}
	if !isNormalizedRequired(mark.Source) || mark.Source != strings.ToLower(mark.Source) {
		return fmt.Errorf("mark source must be non-empty and lowercase normalized")
	}
	if !isNormalizedRequired(mark.SourceNamespace) || !isNormalizedRequired(mark.SourceObservationID) {
		return fmt.Errorf("mark source namespace and observation ID must be non-empty and normalized")
	}
	if mark.SourceRevision != strings.TrimSpace(mark.SourceRevision) {
		return fmt.Errorf("mark source revision must be normalized")
	}
	if mark.EffectiveAt.IsZero() || mark.ObservedAt.IsZero() || mark.CreatedAt.IsZero() {
		return fmt.Errorf("mark effective, observed, and creation times are required")
	}
	for _, value := range []time.Time{mark.EffectiveAt, mark.ObservedAt, mark.CreatedAt} {
		if value.Location() != time.UTC || !hasPostgresTimestampPrecision(value) {
			return fmt.Errorf("mark timestamps must use UTC PostgreSQL microsecond precision")
		}
	}
	if mark.ObservedAt.Before(mark.EffectiveAt) {
		return fmt.Errorf("mark observed time cannot precede effective time")
	}
	if _, err := normalizeJSONObject(mark.Metadata, "mark metadata"); err != nil {
		return err
	}
	if expected := markObservationID(&mark); mark.ID != expected {
		return fmt.Errorf("mark ID %s does not match deterministic identity %s", mark.ID, expected)
	}
	return nil
}

// SameMarkObservation reports exact idempotent-retry equality. Creation time
// is excluded because it is local persistence metadata; all provider evidence
// and normalized semantics must otherwise match byte-for-byte where relevant.
func SameMarkObservation(left, right *MarkObservation) bool {
	if left == nil || right == nil {
		return left == right
	}
	return left.ID == right.ID &&
		left.InstrumentID == right.InstrumentID &&
		left.Price.Equal(right.Price) &&
		left.PriceCurrency == right.PriceCurrency &&
		left.Source == right.Source &&
		left.SourceNamespace == right.SourceNamespace &&
		left.SourceObservationID == right.SourceObservationID &&
		left.SourceRevision == right.SourceRevision &&
		left.EffectiveAt.Equal(right.EffectiveAt) &&
		left.ObservedAt.Equal(right.ObservedAt) &&
		jsonSemanticEqual(left.Metadata, right.Metadata)
}

func markObservationID(mark *MarkObservation) uuid.UUID {
	return economicid.DeterministicUUID(
		markObservationIDDomain,
		mark.InstrumentID.String(),
		mark.PriceCurrency,
		mark.Source,
		mark.SourceNamespace,
		mark.SourceObservationID,
	)
}

func validProjectionDecimal(value decimal.Decimal) bool {
	if !value.Equal(value.Round(12)) {
		return false
	}
	return value.NumDigits()+int(value.Exponent()) <= 26
}
