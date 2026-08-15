package instrument

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const optionContractTermsIDDomain = "option-contract-terms"

// OptionContractType identifies the exercise direction encoded by immutable
// option contract terms.
type OptionContractType string

const (
	OptionContractCall OptionContractType = "call"
	OptionContractPut  OptionContractType = "put"
)

// OptionContractTerms supplies sourced economic terms that are intentionally
// not inferred from a ticker or asserted by an exercise adapter caller.
type OptionContractTerms struct {
	ID                     uuid.UUID          `json:"id"`
	OptionInstrumentID     uuid.UUID          `json:"option_instrument_id"`
	UnderlyingInstrumentID uuid.UUID          `json:"underlying_instrument_id"`
	ContractType           OptionContractType `json:"contract_type"`
	StrikePrice            decimal.Decimal    `json:"strike_price"`
	StrikeCurrency         string             `json:"strike_currency"`
	DeliverableQuantity    decimal.Decimal    `json:"deliverable_quantity"`
	Source                 string             `json:"source"`
	SourceNamespace        string             `json:"source_namespace"`
	SourceRecordID         string             `json:"source_record_id"`
	SourceRevision         string             `json:"source_revision"`
	EffectiveAt            time.Time          `json:"effective_at"`
	ObservedAt             time.Time          `json:"observed_at"`
	RawPayload             json.RawMessage    `json:"raw_payload"`
	PayloadSHA256          string             `json:"payload_sha256"`
	Metadata               json.RawMessage    `json:"metadata"`
	CreatedAt              time.Time          `json:"created_at"`
}

// OptionContractTermsInput contains one provenance-backed terms fact.
type OptionContractTermsInput struct {
	OptionInstrumentID     uuid.UUID
	UnderlyingInstrumentID uuid.UUID
	ContractType           OptionContractType
	StrikePrice            decimal.Decimal
	StrikeCurrency         string
	DeliverableQuantity    decimal.Decimal
	Source                 string
	SourceNamespace        string
	SourceRecordID         string
	SourceRevision         string
	EffectiveAt            time.Time
	ObservedAt             time.Time
	RawPayload             json.RawMessage
	Metadata               json.RawMessage
	CreatedAt              time.Time
}

// NewOptionContractTerms normalizes one immutable sourced terms record.
// SourceRevision remains evidence and cannot manufacture another identity.
func NewOptionContractTerms(input OptionContractTermsInput) (*OptionContractTerms, error) {
	createdAt := input.CreatedAt.UTC().Truncate(time.Microsecond)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	metadata, err := normalizeJSONObject(input.Metadata, "option contract terms metadata")
	if err != nil {
		return nil, err
	}
	rawPayload := append(json.RawMessage(nil), input.RawPayload...)
	payloadHash := sha256.Sum256(rawPayload)
	terms := &OptionContractTerms{
		OptionInstrumentID:     input.OptionInstrumentID,
		UnderlyingInstrumentID: input.UnderlyingInstrumentID,
		ContractType:           input.ContractType,
		StrikePrice:            input.StrikePrice,
		StrikeCurrency:         strings.ToUpper(strings.TrimSpace(input.StrikeCurrency)),
		DeliverableQuantity:    input.DeliverableQuantity,
		Source:                 strings.ToLower(strings.TrimSpace(input.Source)),
		SourceNamespace:        strings.TrimSpace(input.SourceNamespace),
		SourceRecordID:         strings.TrimSpace(input.SourceRecordID),
		SourceRevision:         strings.TrimSpace(input.SourceRevision),
		EffectiveAt:            input.EffectiveAt.UTC().Truncate(time.Microsecond),
		ObservedAt:             input.ObservedAt.UTC().Truncate(time.Microsecond),
		RawPayload:             rawPayload,
		PayloadSHA256:          hex.EncodeToString(payloadHash[:]),
		Metadata:               metadata,
		CreatedAt:              createdAt,
	}
	terms.ID = economicid.DeterministicUUID(
		optionContractTermsIDDomain,
		terms.OptionInstrumentID.String(),
		terms.Source,
		terms.SourceNamespace,
		terms.SourceRecordID,
	)
	if err := terms.Validate(); err != nil {
		return nil, err
	}
	return terms, nil
}

// Validate checks the standalone term shape and provenance. Cross-record
// option, underlying, venue, and effective-history rules live at boundaries
// that can load the referenced immutable records.
func (terms OptionContractTerms) Validate() error {
	if terms.OptionInstrumentID == uuid.Nil || terms.UnderlyingInstrumentID == uuid.Nil {
		return fmt.Errorf("option contract terms require option and underlying instrument IDs")
	}
	if terms.OptionInstrumentID == terms.UnderlyingInstrumentID {
		return fmt.Errorf("option contract terms underlying must differ from the option instrument")
	}
	if terms.ContractType != OptionContractCall && terms.ContractType != OptionContractPut {
		return fmt.Errorf("option contract type %q is invalid", terms.ContractType)
	}
	if err := validatePositiveReferenceDecimal("option strike price", terms.StrikePrice); err != nil {
		return err
	}
	if !isCurrency(terms.StrikeCurrency) {
		return fmt.Errorf("option strike currency %q must be a normalized three-letter code", terms.StrikeCurrency)
	}
	if err := validatePositiveReferenceDecimal("option deliverable quantity", terms.DeliverableQuantity); err != nil {
		return err
	}
	if terms.Source == "" || terms.Source != strings.ToLower(strings.TrimSpace(terms.Source)) {
		return fmt.Errorf("option terms source must be non-empty and normalized lowercase")
	}
	if !normalizedRequiredReference(terms.SourceNamespace) || !normalizedRequiredReference(terms.SourceRecordID) {
		return fmt.Errorf("option terms source namespace and record ID must be non-empty and normalized")
	}
	if terms.SourceRevision != strings.TrimSpace(terms.SourceRevision) {
		return fmt.Errorf("option terms source revision must be normalized")
	}
	if terms.EffectiveAt.IsZero() || !isNormalizedReferenceTime(terms.EffectiveAt) {
		return fmt.Errorf("option terms effective time must use normalized UTC microsecond precision")
	}
	if terms.ObservedAt.IsZero() || !isNormalizedReferenceTime(terms.ObservedAt) {
		return fmt.Errorf("option terms observed time must use normalized UTC microsecond precision")
	}
	if terms.ObservedAt.Before(terms.EffectiveAt) {
		return fmt.Errorf("option terms observed time cannot precede effective time")
	}
	if len(terms.RawPayload) == 0 || !json.Valid(terms.RawPayload) {
		return fmt.Errorf("option terms raw payload must be valid JSON")
	}
	payloadHash := sha256.Sum256(terms.RawPayload)
	if terms.PayloadSHA256 != hex.EncodeToString(payloadHash[:]) {
		return fmt.Errorf("option terms payload SHA-256 does not match raw bytes")
	}
	if _, err := normalizeJSONObject(terms.Metadata, "option contract terms metadata"); err != nil {
		return err
	}
	if terms.CreatedAt.IsZero() || !isNormalizedReferenceTime(terms.CreatedAt) {
		return fmt.Errorf("option terms creation time must use normalized UTC microsecond precision")
	}
	expectedID := economicid.DeterministicUUID(
		optionContractTermsIDDomain,
		terms.OptionInstrumentID.String(),
		terms.Source,
		terms.SourceNamespace,
		terms.SourceRecordID,
	)
	if terms.ID != expectedID {
		return fmt.Errorf("option contract terms ID does not match deterministic source identity")
	}
	return nil
}

// SameOptionContractTermsPayload compares every immutable semantic and source
// field except local creation time.
func SameOptionContractTermsPayload(left, right *OptionContractTerms) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID &&
		left.OptionInstrumentID == right.OptionInstrumentID &&
		left.UnderlyingInstrumentID == right.UnderlyingInstrumentID &&
		left.ContractType == right.ContractType &&
		left.StrikePrice.Equal(right.StrikePrice) &&
		left.StrikeCurrency == right.StrikeCurrency &&
		left.DeliverableQuantity.Equal(right.DeliverableQuantity) &&
		left.Source == right.Source &&
		left.SourceNamespace == right.SourceNamespace &&
		left.SourceRecordID == right.SourceRecordID &&
		left.SourceRevision == right.SourceRevision &&
		left.EffectiveAt.Equal(right.EffectiveAt) &&
		left.ObservedAt.Equal(right.ObservedAt) &&
		left.PayloadSHA256 == right.PayloadSHA256 &&
		bytes.Equal(left.RawPayload, right.RawPayload) &&
		jsonBytesEqual(left.Metadata, right.Metadata)
}

func normalizedRequiredReference(value string) bool {
	return value != "" && value == strings.TrimSpace(value)
}

func jsonBytesEqual(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	leftJSON, leftErr := json.Marshal(leftValue)
	rightJSON, rightErr := json.Marshal(rightValue)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
