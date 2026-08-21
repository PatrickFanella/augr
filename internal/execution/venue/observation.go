package venue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const observationIDDomain = "venue-observation"

var policyVersionPattern = regexp.MustCompile(`^venue-adapter-policy-v1@sha256:[0-9a-f]{64}$`)

// ObservationKind distinguishes raw provider object families without
// collapsing the provider's own state/event token.
type ObservationKind string

const (
	ObservationSubmitResponse    ObservationKind = "submit_response"
	ObservationOrderSnapshot     ObservationKind = "order_snapshot"
	ObservationTradeUpdate       ObservationKind = "trade_update"
	ObservationFill              ObservationKind = "fill"
	ObservationCorrection        ObservationKind = "correction"
	ObservationBust              ObservationKind = "bust"
	ObservationCancelResponse    ObservationKind = "cancel_response"
	ObservationMalformedResponse ObservationKind = "malformed_response"
)

// SourceIdentityKind makes a provider-supplied event identity distinguishable
// from a locally constructed response identity used when the provider omitted
// one. It never changes after persistence.
type SourceIdentityKind string

const (
	SourceIdentityProvider      SourceIdentityKind = "provider"
	SourceIdentityLocalResponse SourceIdentityKind = "local_response"
)

// ObservationInput contains exact provider context and wire bytes. The
// constructor normalizes timestamps and identity whitespace but never rewrites
// the wire payload or provider token casing.
type ObservationInput struct {
	AccountID          uuid.UUID
	IntentID           uuid.UUID
	OrderID            uuid.UUID
	BindingID          *uuid.UUID
	VenueContractID    uuid.UUID
	Provider           Provider
	Venue              string
	PolicyVersion      string
	Kind               ObservationKind
	ProviderState      string
	MappedOutcome      MappedOutcome
	ExternalOrderID    string
	ClientOrderID      string
	ProviderContractID string
	CanonicalOutcome   string
	ProviderBookSide   string
	ProviderAction     string
	ProviderPrice      *decimal.Decimal
	IdentityKind       SourceIdentityKind
	SourceNamespace    string
	SourceEventID      string
	SourceRevision     string
	SourceAt           time.Time
	ReceivedAt         time.Time
	RawPayload         json.RawMessage
	CreatedAt          time.Time
}

// Observation is one immutable raw venue fact. Payload is the deterministic
// parsed-object representation; RawPayload retains byte-for-byte wire evidence.
type Observation struct {
	ID                 uuid.UUID
	AccountID          uuid.UUID
	IntentID           uuid.UUID
	OrderID            uuid.UUID
	BindingID          *uuid.UUID
	VenueContractID    uuid.UUID
	Provider           Provider
	Venue              string
	PolicyVersion      string
	Kind               ObservationKind
	ProviderState      string
	MappedOutcome      MappedOutcome
	ExternalOrderID    string
	ClientOrderID      string
	ProviderContractID string
	CanonicalOutcome   string
	ProviderBookSide   string
	ProviderAction     string
	ProviderPrice      *decimal.Decimal
	IdentityKind       SourceIdentityKind
	SourceNamespace    string
	SourceEventID      string
	SourceRevision     string
	SourceAt           time.Time
	ReceivedAt         time.Time
	RawPayload         json.RawMessage
	PayloadSHA256      string
	Payload            json.RawMessage
	CreatedAt          time.Time
}

// NewObservation constructs immutable exact evidence. Revision, timestamps,
// mapping, and bytes are deliberately excluded from identity so changed reuse
// of a provider identity is detectable as a conflict.
func NewObservation(input ObservationInput) (*Observation, error) {
	rawPayload := append(json.RawMessage(nil), input.RawPayload...)
	parsedPayload, err := canonicalJSONObject(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("venue observation: %w", err)
	}
	digestBytes := sha256.Sum256(rawPayload)
	createdAt := normalizeObservationTime(input.CreatedAt)
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	provider := Provider(strings.ToLower(strings.TrimSpace(string(input.Provider))))
	observation := &Observation{
		AccountID: input.AccountID, IntentID: input.IntentID, OrderID: input.OrderID,
		BindingID: cloneObservationUUID(input.BindingID), VenueContractID: input.VenueContractID,
		Provider: provider, Venue: strings.ToLower(strings.TrimSpace(input.Venue)),
		PolicyVersion: strings.TrimSpace(input.PolicyVersion), Kind: input.Kind,
		ProviderState: strings.TrimSpace(input.ProviderState), MappedOutcome: input.MappedOutcome,
		ExternalOrderID: strings.TrimSpace(input.ExternalOrderID), ClientOrderID: strings.TrimSpace(input.ClientOrderID),
		ProviderContractID: strings.TrimSpace(input.ProviderContractID), CanonicalOutcome: strings.TrimSpace(input.CanonicalOutcome),
		ProviderBookSide: strings.TrimSpace(input.ProviderBookSide), ProviderAction: strings.TrimSpace(input.ProviderAction),
		ProviderPrice: cloneObservationDecimal(input.ProviderPrice),
		IdentityKind:  input.IdentityKind, SourceNamespace: strings.TrimSpace(input.SourceNamespace),
		SourceEventID: strings.TrimSpace(input.SourceEventID), SourceRevision: strings.TrimSpace(input.SourceRevision),
		SourceAt: normalizeObservationTime(input.SourceAt), ReceivedAt: normalizeObservationTime(input.ReceivedAt),
		RawPayload: rawPayload, PayloadSHA256: hex.EncodeToString(digestBytes[:]), Payload: parsedPayload,
		CreatedAt: createdAt,
	}
	observation.ID = economicid.DeterministicUUID(
		observationIDDomain, observation.AccountID.String(), string(observation.Provider),
		observation.SourceNamespace, observation.SourceEventID,
	)
	if err := observation.Validate(); err != nil {
		return nil, err
	}
	return observation, nil
}

// Validate checks bounded vocabulary, normalized identity, exact bytes/hash/
// parsed JSON, timestamp ordering, and deterministic identity.
func (observation Observation) Validate() error {
	if observation.AccountID == uuid.Nil || observation.IntentID == uuid.Nil || observation.OrderID == uuid.Nil ||
		observation.VenueContractID == uuid.Nil {
		return fmt.Errorf("venue observation account, intent, order, and contract IDs are required")
	}
	if observation.BindingID != nil && *observation.BindingID == uuid.Nil {
		return fmt.Errorf("venue observation binding ID cannot be nil")
	}
	if !validProvider(observation.Provider) || observation.Venue != string(observation.Provider) {
		return fmt.Errorf("venue observation provider and venue are invalid")
	}
	if !policyVersionPattern.MatchString(observation.PolicyVersion) {
		return fmt.Errorf("venue observation policy version is invalid")
	}
	if !validObservationKind(observation.Kind) || !validMappedOutcome(observation.MappedOutcome) {
		return fmt.Errorf("venue observation kind or mapped outcome is invalid")
	}
	if !normalizedRequired(observation.ProviderState, 128) || !normalizedRequired(observation.ClientOrderID, 256) {
		return fmt.Errorf("venue observation provider state and client order ID are required")
	}
	for label, value := range map[string]string{
		"external order ID": observation.ExternalOrderID, "provider contract ID": observation.ProviderContractID,
	} {
		if value != strings.TrimSpace(value) || len(value) > 512 {
			return fmt.Errorf("venue observation %s is invalid", label)
		}
	}
	if observation.Kind != ObservationMalformedResponse && observation.ProviderContractID == "" {
		return fmt.Errorf("venue observation provider contract ID is required")
	}
	if !validOptionalToken(observation.CanonicalOutcome, "yes", "no") ||
		!validOptionalToken(observation.ProviderBookSide, "bid", "ask") ||
		!validOptionalToken(observation.ProviderAction, "buy", "sell") ||
		!validObservationPrice(observation.ProviderPrice) {
		return fmt.Errorf("venue observation outcome, book side, action, or price is invalid")
	}
	if !validSourceIdentityKind(observation.IdentityKind) ||
		!normalizedRequired(observation.SourceNamespace, 256) || !normalizedRequired(observation.SourceEventID, 512) ||
		observation.SourceRevision != strings.TrimSpace(observation.SourceRevision) || len(observation.SourceRevision) > 256 {
		return fmt.Errorf("venue observation source identity is invalid")
	}
	if observation.IdentityKind == SourceIdentityLocalResponse {
		if !strings.HasPrefix(observation.SourceEventID, "local-response/") {
			return fmt.Errorf("venue observation local response identity must be explicitly labelled")
		}
	} else if strings.HasPrefix(observation.SourceEventID, "local-response/") {
		return fmt.Errorf("venue observation provider identity cannot use the local response label")
	}
	if observation.SourceAt.IsZero() || observation.ReceivedAt.IsZero() || observation.CreatedAt.IsZero() ||
		!normalizedObservationTime(observation.SourceAt) || !normalizedObservationTime(observation.ReceivedAt) ||
		!normalizedObservationTime(observation.CreatedAt) || observation.SourceAt.After(observation.ReceivedAt) {
		return fmt.Errorf("venue observation timestamps must be ordered UTC microseconds")
	}
	parsedPayload, err := canonicalJSONObject(observation.RawPayload)
	if err != nil {
		return fmt.Errorf("venue observation raw payload: %w", err)
	}
	digestBytes := sha256.Sum256(observation.RawPayload)
	if observation.PayloadSHA256 != hex.EncodeToString(digestBytes[:]) {
		return fmt.Errorf("venue observation payload SHA-256 does not match raw bytes")
	}
	if !bytes.Equal(observation.Payload, parsedPayload) {
		return fmt.Errorf("venue observation parsed payload does not match raw bytes")
	}
	wantID := economicid.DeterministicUUID(
		observationIDDomain, observation.AccountID.String(), string(observation.Provider),
		observation.SourceNamespace, observation.SourceEventID,
	)
	if observation.ID != wantID {
		return fmt.Errorf("venue observation ID does not match source identity")
	}
	return nil
}

// SameObservationPayload reports exact retry equality. CreatedAt is local
// persistence evidence and is intentionally excluded; all provider facts are
// included.
func SameObservationPayload(left, right *Observation) bool {
	if left == nil || right == nil {
		return false
	}
	return left.ID == right.ID && left.AccountID == right.AccountID && left.IntentID == right.IntentID &&
		left.OrderID == right.OrderID && equalObservationUUID(left.BindingID, right.BindingID) &&
		left.VenueContractID == right.VenueContractID && left.Provider == right.Provider && left.Venue == right.Venue &&
		left.PolicyVersion == right.PolicyVersion && left.Kind == right.Kind && left.ProviderState == right.ProviderState &&
		left.MappedOutcome == right.MappedOutcome && left.ExternalOrderID == right.ExternalOrderID &&
		left.ClientOrderID == right.ClientOrderID && left.ProviderContractID == right.ProviderContractID &&
		left.CanonicalOutcome == right.CanonicalOutcome && left.ProviderBookSide == right.ProviderBookSide &&
		left.ProviderAction == right.ProviderAction && equalObservationDecimal(left.ProviderPrice, right.ProviderPrice) &&
		left.IdentityKind == right.IdentityKind &&
		left.SourceNamespace == right.SourceNamespace && left.SourceEventID == right.SourceEventID &&
		left.SourceRevision == right.SourceRevision && left.SourceAt.Equal(right.SourceAt) &&
		left.ReceivedAt.Equal(right.ReceivedAt) && left.PayloadSHA256 == right.PayloadSHA256 &&
		bytes.Equal(left.RawPayload, right.RawPayload) && bytes.Equal(left.Payload, right.Payload)
}

func cloneObservation(value Observation) Observation {
	value.BindingID = cloneObservationUUID(value.BindingID)
	value.ProviderPrice = cloneObservationDecimal(value.ProviderPrice)
	value.RawPayload = append(json.RawMessage(nil), value.RawPayload...)
	value.Payload = append(json.RawMessage(nil), value.Payload...)
	return value
}

func canonicalJSONObject(raw json.RawMessage) (json.RawMessage, error) {
	if err := validateUniqueJSONObject(raw); err != nil {
		return nil, err
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil || object == nil {
		return nil, fmt.Errorf("payload must be a valid JSON object")
	}
	encoded, err := json.Marshal(object)
	if err != nil {
		return nil, fmt.Errorf("marshal parsed JSON object: %w", err)
	}
	return encoded, nil
}

func validateUniqueJSONObject(raw json.RawMessage) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("payload must be a valid JSON object")
	}
	opener, ok := token.(json.Delim)
	if !ok || opener != '{' {
		return fmt.Errorf("payload must be a valid JSON object")
	}
	if err := validateJSONComposite(decoder, opener); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("payload must contain exactly one JSON object")
	}
	return nil
}

func validateJSONComposite(decoder *json.Decoder, opener json.Delim) error {
	if opener == '{' {
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return fmt.Errorf("decode JSON object key: %w", err)
			}
			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf("payload JSON object key is not a string")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("payload JSON object contains duplicate key %q", key)
			}
			seen[key] = struct{}{}
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
	} else {
		for decoder.More() {
			if err := validateJSONValue(decoder); err != nil {
				return err
			}
		}
	}
	closer, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON closing delimiter: %w", err)
	}
	want := json.Delim(']')
	if opener == '{' {
		want = '}'
	}
	if closer != want {
		return fmt.Errorf("payload JSON closing delimiter is invalid")
	}
	return nil
}

func validateJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("decode JSON value: %w", err)
	}
	if delimiter, ok := token.(json.Delim); ok {
		if delimiter != '{' && delimiter != '[' {
			return fmt.Errorf("payload JSON delimiter is invalid")
		}
		return validateJSONComposite(decoder, delimiter)
	}
	return nil
}

func validObservationKind(value ObservationKind) bool {
	switch value {
	case ObservationSubmitResponse, ObservationOrderSnapshot, ObservationTradeUpdate,
		ObservationFill, ObservationCorrection, ObservationBust, ObservationCancelResponse,
		ObservationMalformedResponse:
		return true
	default:
		return false
	}
}

func validSourceIdentityKind(value SourceIdentityKind) bool {
	return value == SourceIdentityProvider || value == SourceIdentityLocalResponse
}

func validOptionalToken(value string, allowed ...string) bool {
	if value == "" {
		return true
	}
	if value != strings.TrimSpace(value) || len(value) > 64 {
		return false
	}
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

func normalizeObservationTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC().Truncate(time.Microsecond)
}

func normalizedObservationTime(value time.Time) bool {
	return value.Location() == time.UTC && value.Equal(value.Truncate(time.Microsecond))
}

func cloneObservationUUID(value *uuid.UUID) *uuid.UUID {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalObservationUUID(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validObservationPrice(value *decimal.Decimal) bool {
	if value == nil {
		return true
	}
	maximum := decimal.RequireFromString("100000000000000000000000000")
	return !value.IsNegative() && value.Equal(value.Round(12)) && value.LessThan(maximum)
}

func cloneObservationDecimal(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func equalObservationDecimal(left, right *decimal.Decimal) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}
