package venuerecon

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

const (
	providerCaptureSchemaV1 = "venue-provider-capture-v1"
	stableSnapshotSchemaV1  = "venue-provider-stable-snapshot-v1"
	providerCaptureDomain   = "venue-reconciliation-provider-capture"
	stableSnapshotDomain    = "venue-reconciliation-stable-snapshot"
)

// ContractResolver resolves provider contract identity at the provider fact's
// effective time. Implementations must reject missing or ambiguous bindings.
type ContractResolver interface {
	ResolveVenueContract(context.Context, venue.Provider, string, time.Time) (instrument.VenueContract, error)
}

// RawPage is immutable provider pagination evidence. Raw bytes are retained;
// their digest participates in provider-state equality.
type RawPage struct {
	Cursor     string
	NextCursor string
	Terminal   bool
	Raw        json.RawMessage
}

// PositionInput is one provider position before canonical contract resolution.
type PositionInput struct {
	ContractID string
	Quantity   string
	Currency   string
	SourceAt   time.Time
}

// FillInput is one provider fill before canonical contract resolution.
// Revisions identify their original ordinary fill and never replace its key.
type FillInput struct {
	SourceID                 string
	OriginalSourceID         string
	ObservationClass         lifecycle.ObservationClass
	ObservationDiscriminator string
	ExternalOrderID          string
	ClientOrderID            string
	ContractID               string
	Side                     lifecycle.Side
	Quantity                 string
	Price                    string
	Fee                      string
	Currency                 string
	SourceRevision           string
	SourceAt                 time.Time
}

// CaptureInput contains one complete read-only provider capture.
type CaptureInput struct {
	Provider     venue.Provider
	Namespace    string
	AccountID    string
	Currency     string
	HorizonStart time.Time
	HorizonEnd   time.Time
	CaptureStart time.Time
	CaptureEnd   time.Time
	ProviderAsOf time.Time
	Cash         string
	Equity       string
	Pages        []RawPage
	Positions    []PositionInput
	Fills        []FillInput
}

type normalizedPage struct {
	Sequence   int             `json:"sequence"`
	Cursor     string          `json:"cursor"`
	NextCursor string          `json:"next_cursor"`
	Terminal   bool            `json:"terminal"`
	SHA256     string          `json:"sha256"`
	Raw        json.RawMessage `json:"raw"`
}

// ProviderPosition is a canonical, exactly-valued provider position.
type ProviderPosition struct {
	InstrumentID  uuid.UUID `json:"instrument_id"`
	VenueContract uuid.UUID `json:"venue_contract_id"`
	ContractID    string    `json:"contract_id"`
	Quantity      string    `json:"quantity"`
	Currency      string    `json:"currency"`
	SourceAt      string    `json:"source_at"`
}

// ProviderFill is a canonical provider fill or revision observation.
type ProviderFill struct {
	SourceID                 string                     `json:"source_id"`
	OriginalSourceID         string                     `json:"original_source_id"`
	ObservationClass         lifecycle.ObservationClass `json:"observation_class"`
	ObservationDiscriminator string                     `json:"observation_discriminator"`
	ExternalOrderID          string                     `json:"external_order_id"`
	ClientOrderID            string                     `json:"client_order_id"`
	InstrumentID             uuid.UUID                  `json:"instrument_id"`
	VenueContract            uuid.UUID                  `json:"venue_contract_id"`
	ContractID               string                     `json:"contract_id"`
	Side                     lifecycle.Side             `json:"side"`
	Quantity                 string                     `json:"quantity"`
	Price                    string                     `json:"price"`
	Fee                      string                     `json:"fee"`
	Currency                 string                     `json:"currency"`
	SourceRevision           string                     `json:"source_revision"`
	SourceAt                 string                     `json:"source_at"`
}

type captureCanonical struct {
	Schema       string             `json:"schema"`
	Provider     venue.Provider     `json:"provider"`
	Namespace    string             `json:"namespace"`
	AccountID    string             `json:"account_id"`
	Currency     string             `json:"currency"`
	HorizonStart string             `json:"horizon_start"`
	HorizonEnd   string             `json:"horizon_end"`
	ProviderAsOf string             `json:"provider_as_of"`
	Cash         string             `json:"cash"`
	Equity       string             `json:"equity"`
	Pages        []normalizedPage   `json:"pages"`
	Positions    []ProviderPosition `json:"positions"`
	Fills        []ProviderFill     `json:"fills"`
}

// ProviderCapture is one validated complete provider-state observation.
type ProviderCapture struct {
	canonical captureCanonical
	start     time.Time
	end       time.Time
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

// StableProviderSnapshot is admitted only from two equal provider-state digests.
type StableProviderSnapshot struct {
	first, second *ProviderCapture
	bytes         json.RawMessage
	digest        string
	id            uuid.UUID
}

// SnapshotAdmission retains instability evidence without exposing values as a
// comparable snapshot.
type SnapshotAdmission struct {
	Snapshot *StableProviderSnapshot
	Reason   ReasonCode
	First    *ProviderCapture
	Second   *ProviderCapture
}

// CaptureReader has only a read operation; reconciliation cannot mutate a venue.
type CaptureReader interface {
	Capture(context.Context) (*ProviderCapture, error)
}

// CaptureFailure preserves why a read-only provider capture could not produce
// comparable evidence without exposing transport-specific error types.
type CaptureFailure struct {
	Reason ReasonCode
	Err    error
}

func (failure *CaptureFailure) Error() string {
	if failure == nil || failure.Err == nil {
		return "provider capture failed"
	}
	return failure.Err.Error()
}

func (failure *CaptureFailure) Unwrap() error {
	if failure == nil {
		return nil
	}
	return failure.Err
}

func NewCaptureFailure(reason ReasonCode, err error) error {
	if reason != ReasonProviderUnavailable && reason != ReasonSnapshotIncomplete && reason != ReasonSnapshotMappingFailure {
		return fmt.Errorf("provider capture failure reason is invalid")
	}
	if err == nil {
		err = fmt.Errorf("provider capture failed")
	}
	return &CaptureFailure{Reason: reason, Err: err}
}

func captureFailureReason(err error) ReasonCode {
	var failure *CaptureFailure
	if errors.As(err, &failure) {
		return failure.Reason
	}
	return ReasonProviderUnavailable
}

// CaptureTwice performs the policy-mandated two reads and admits only equality.
func CaptureTwice(ctx context.Context, reader CaptureReader) (*SnapshotAdmission, error) {
	if reader == nil {
		return nil, fmt.Errorf("provider capture reader is required")
	}
	first, err := reader.Capture(ctx)
	if err != nil {
		return &SnapshotAdmission{Reason: captureFailureReason(err)}, nil
	}
	second, err := reader.Capture(ctx)
	if err != nil {
		return &SnapshotAdmission{Reason: captureFailureReason(err), First: first}, nil
	}
	return AdmitStableProviderSnapshot(first, second)
}

type contractResolutionError struct{ err error }

func (value *contractResolutionError) Error() string { return value.err.Error() }
func (value *contractResolutionError) Unwrap() error { return value.err }

// NewProviderCapture validates, resolves, sorts, and content-addresses a capture.
func newProviderCapture(ctx context.Context, input CaptureInput, resolver ContractResolver) (*ProviderCapture, error) {
	rule, ok := mustPolicyProvider(input.Provider)
	if !ok || resolver == nil {
		return nil, fmt.Errorf("supported provider and contract resolver are required")
	}
	if input.Namespace != rule.AuthoritativeFillNamespace || strings.TrimSpace(input.AccountID) == "" || strings.TrimSpace(input.AccountID) != input.AccountID {
		return nil, fmt.Errorf("provider namespace and account are invalid")
	}
	currency, err := normalizeCurrency(input.Currency)
	if err != nil {
		return nil, err
	}
	if err := validateCaptureTimes(input); err != nil {
		return nil, err
	}
	cash, err := exactDecimal(input.Cash)
	if err != nil {
		return nil, fmt.Errorf("cash: %w", err)
	}
	equity, err := exactDecimal(input.Equity)
	if err != nil {
		return nil, fmt.Errorf("equity: %w", err)
	}
	pages, err := normalizePages(input.Pages)
	if err != nil {
		return nil, err
	}
	positions, err := normalizePositions(ctx, input.Provider, currency, input.Positions, resolver)
	if err != nil {
		return nil, err
	}
	fills, err := normalizeFills(ctx, input.Provider, currency, input.HorizonStart, input.HorizonEnd, rule.SupportsRevisions, input.Fills, resolver)
	if err != nil {
		return nil, err
	}
	canonical := captureCanonical{
		Schema: providerCaptureSchemaV1, Provider: input.Provider, Namespace: rule.AuthoritativeFillNamespace,
		AccountID: strings.TrimSpace(input.AccountID), Currency: currency,
		HorizonStart: canonicalTime(input.HorizonStart), HorizonEnd: canonicalTime(input.HorizonEnd),
		ProviderAsOf: canonicalTime(input.ProviderAsOf), Cash: cash, Equity: equity,
		Pages: pages, Positions: positions, Fills: fills,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal provider capture: %w", err)
	}
	digest := sha256Hex(encoded)
	version := providerCaptureSchemaV1 + "@sha256:" + digest
	return &ProviderCapture{
		canonical: canonical, start: input.CaptureStart, end: input.CaptureEnd,
		bytes: encoded, digest: digest, id: economicid.DeterministicUUID(providerCaptureDomain, version),
	}, nil
}

func mustPolicyProvider(provider venue.Provider) (ProviderRule, bool) {
	policy, err := NewPolicy(ReviewedPolicyV1Input())
	if err != nil {
		return ProviderRule{}, false
	}
	return policy.ProviderRule(provider)
}

func validateCaptureTimes(input CaptureInput) error {
	times := []time.Time{input.HorizonStart, input.HorizonEnd, input.CaptureStart, input.CaptureEnd, input.ProviderAsOf}
	for _, value := range times {
		if value.IsZero() || value.Location() != time.UTC || value.Nanosecond()%1000 != 0 {
			return fmt.Errorf("capture times must be UTC with microsecond precision")
		}
	}
	if !input.HorizonStart.Before(input.HorizonEnd) || input.CaptureEnd.Before(input.CaptureStart) ||
		input.ProviderAsOf.Before(input.HorizonStart) || input.ProviderAsOf.After(input.CaptureEnd) {
		return fmt.Errorf("capture time ordering is invalid")
	}
	return nil
}

func normalizePages(inputs []RawPage) ([]normalizedPage, error) {
	if len(inputs) == 0 {
		return nil, fmt.Errorf("complete page evidence is required")
	}
	seen := make(map[string]struct{}, len(inputs))
	result := make([]normalizedPage, 0, len(inputs))
	for index, page := range inputs {
		if !json.Valid(page.Raw) || len(page.Raw) == 0 {
			return nil, fmt.Errorf("page %d raw bytes are invalid", index)
		}
		key := page.Cursor
		if index == 0 && key == "" {
			key = "<initial>"
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate page cursor %q", page.Cursor)
		}
		seen[key] = struct{}{}
		if index < len(inputs)-1 && (page.Terminal || page.NextCursor == "" || page.NextCursor != inputs[index+1].Cursor) {
			return nil, fmt.Errorf("page %d pagination chain is incomplete", index)
		}
		if index == len(inputs)-1 && (!page.Terminal || page.NextCursor != "") {
			return nil, fmt.Errorf("terminal page proof is required")
		}
		result = append(result, normalizedPage{
			Sequence: index, Cursor: page.Cursor, NextCursor: page.NextCursor,
			Terminal: page.Terminal, SHA256: sha256Hex(page.Raw), Raw: append(json.RawMessage(nil), page.Raw...),
		})
	}
	return result, nil
}

func normalizePositions(ctx context.Context, provider venue.Provider, currency string, inputs []PositionInput, resolver ContractResolver) ([]ProviderPosition, error) {
	result := make([]ProviderPosition, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	seenInstruments := make(map[uuid.UUID]struct{}, len(inputs))
	for _, value := range inputs {
		if strings.TrimSpace(value.ContractID) == "" || !validEvidenceTime(value.SourceAt) {
			return nil, fmt.Errorf("position identity or source time is invalid")
		}
		if _, ok := seen[value.ContractID]; ok {
			return nil, fmt.Errorf("duplicate position contract %q", value.ContractID)
		}
		seen[value.ContractID] = struct{}{}
		quantity, err := exactDecimal(value.Quantity)
		if err != nil {
			return nil, fmt.Errorf("position %s quantity: %w", value.ContractID, err)
		}
		rowCurrency, err := normalizeCurrency(value.Currency)
		if err != nil || rowCurrency != currency {
			return nil, fmt.Errorf("position %s currency mismatch", value.ContractID)
		}
		contract, err := resolver.ResolveVenueContract(ctx, provider, value.ContractID, value.SourceAt)
		if err != nil || contract.ID == uuid.Nil || contract.InstrumentID == uuid.Nil || contract.ContractID != value.ContractID ||
			contract.Currency != currency || contract.Venue != string(provider) {
			return nil, &contractResolutionError{err: fmt.Errorf("position %s canonical contract resolution failed", value.ContractID)}
		}
		if _, ok := seenInstruments[contract.InstrumentID]; ok {
			return nil, fmt.Errorf("duplicate canonical position instrument %s", contract.InstrumentID)
		}
		seenInstruments[contract.InstrumentID] = struct{}{}
		result = append(result, ProviderPosition{
			InstrumentID: contract.InstrumentID, VenueContract: contract.ID,
			ContractID: value.ContractID, Quantity: quantity, Currency: currency, SourceAt: canonicalTime(value.SourceAt),
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].InstrumentID.String() < result[j].InstrumentID.String() })
	return result, nil
}

func normalizeFills(ctx context.Context, provider venue.Provider, currency string, horizonStart, horizonEnd time.Time, revisions bool, inputs []FillInput, resolver ContractResolver) ([]ProviderFill, error) {
	result := make([]ProviderFill, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	seenSourceIDs := make(map[string]struct{}, len(inputs))
	for _, value := range inputs {
		class := value.ObservationClass
		if class == "" {
			class = lifecycle.ObservationOrdinary
		}
		if class != lifecycle.ObservationOrdinary && class != lifecycle.ObservationCorrection && class != lifecycle.ObservationBust {
			return nil, fmt.Errorf("fill %q observation class is invalid", value.SourceID)
		}
		if class != lifecycle.ObservationOrdinary && !revisions {
			return nil, fmt.Errorf("provider does not support revision evidence")
		}
		if !canonicalNonempty(value.SourceID) || !canonicalNonempty(value.ContractID) ||
			!canonicalNonempty(value.ExternalOrderID) || !canonicalNonempty(value.ClientOrderID) ||
			!canonicalNonempty(value.SourceRevision) || !validEvidenceTime(value.SourceAt) ||
			value.SourceAt.Before(horizonStart) || value.SourceAt.After(horizonEnd) {
			return nil, fmt.Errorf("fill identity, contract, or horizon is invalid")
		}
		if _, ok := seenSourceIDs[value.SourceID]; ok {
			return nil, fmt.Errorf("duplicate provider source ID %q", value.SourceID)
		}
		seenSourceIDs[value.SourceID] = struct{}{}
		key := value.SourceID
		if class != lifecycle.ObservationOrdinary {
			if !canonicalNonempty(value.OriginalSourceID) || !canonicalNonempty(value.ObservationDiscriminator) {
				return nil, fmt.Errorf("revision fill requires original identity and discriminator")
			}
			key = value.OriginalSourceID + "\x00" + string(class) + "\x00" + value.ObservationDiscriminator
		}
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate provider fill identity %q", key)
		}
		seen[key] = struct{}{}
		quantity, err := exactPositiveDecimal(value.Quantity)
		if err != nil {
			return nil, fmt.Errorf("fill %s quantity: %w", value.SourceID, err)
		}
		price, err := exactNonnegativeDecimal(value.Price)
		if err != nil {
			return nil, fmt.Errorf("fill %s price: %w", value.SourceID, err)
		}
		fee, err := exactDecimal(value.Fee)
		if err != nil || strings.HasPrefix(fee, "-") {
			return nil, fmt.Errorf("fill %s fee is invalid", value.SourceID)
		}
		rowCurrency, err := normalizeCurrency(value.Currency)
		if err != nil || rowCurrency != currency {
			return nil, fmt.Errorf("fill %s currency mismatch", value.SourceID)
		}
		if value.Side != lifecycle.SideBuy && value.Side != lifecycle.SideSell {
			return nil, fmt.Errorf("fill %s side is invalid", value.SourceID)
		}
		contract, err := resolver.ResolveVenueContract(ctx, provider, value.ContractID, value.SourceAt)
		if err != nil || contract.ID == uuid.Nil || contract.InstrumentID == uuid.Nil || contract.ContractID != value.ContractID ||
			contract.Currency != currency || contract.Venue != string(provider) {
			return nil, &contractResolutionError{err: fmt.Errorf("fill %s canonical contract resolution failed", value.SourceID)}
		}
		result = append(result, ProviderFill{
			SourceID: value.SourceID, OriginalSourceID: value.OriginalSourceID,
			ObservationClass: class, ObservationDiscriminator: value.ObservationDiscriminator,
			ExternalOrderID: value.ExternalOrderID, ClientOrderID: value.ClientOrderID,
			InstrumentID: contract.InstrumentID, VenueContract: contract.ID, ContractID: value.ContractID,
			Side: value.Side, Quantity: quantity, Price: price, Fee: fee, Currency: currency,
			SourceRevision: value.SourceRevision, SourceAt: canonicalTime(value.SourceAt),
		})
	}
	sort.Slice(result, func(i, j int) bool {
		left, right := fillSortKey(result[i]), fillSortKey(result[j])
		return left < right
	})
	return result, nil
}

func fillSortKey(value ProviderFill) string {
	if value.ObservationClass == lifecycle.ObservationOrdinary {
		return "0\x00" + value.SourceID
	}
	return "1\x00" + value.OriginalSourceID + "\x00" + string(value.ObservationClass) + "\x00" + value.ObservationDiscriminator
}

func exactDecimal(raw string) (string, error) {
	if raw == "" || strings.TrimSpace(raw) != raw || strings.ContainsAny(raw, "eE+") {
		return "", fmt.Errorf("decimal must use canonical base-10 notation")
	}
	value, err := decimal.NewFromString(raw)
	if err != nil || value.Exponent() < -12 || len(strings.TrimPrefix(value.Coefficient().String(), "-")) > 38 {
		return "", fmt.Errorf("decimal exceeds exact precision")
	}
	return value.String(), nil
}

func exactPositiveDecimal(raw string) (string, error) {
	value, err := exactDecimal(raw)
	if err != nil {
		return "", err
	}
	parsed, _ := decimal.NewFromString(value)
	if !parsed.IsPositive() {
		return "", fmt.Errorf("decimal must be positive")
	}
	return value, nil
}

func exactNonnegativeDecimal(raw string) (string, error) {
	value, err := exactDecimal(raw)
	if err != nil {
		return "", err
	}
	parsed, _ := decimal.NewFromString(value)
	if parsed.IsNegative() {
		return "", fmt.Errorf("decimal must be nonnegative")
	}
	return value, nil
}

func normalizeCurrency(raw string) (string, error) {
	value := strings.ToUpper(strings.TrimSpace(raw))
	if len(value) != 3 || value != raw {
		return "", fmt.Errorf("currency must be a canonical three-letter code")
	}
	for _, char := range value {
		if char < 'A' || char > 'Z' {
			return "", fmt.Errorf("currency must be a canonical three-letter code")
		}
	}
	return value, nil
}

func canonicalTime(value time.Time) string { return value.Format("2006-01-02T15:04:05.000000Z") }

func validEvidenceTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func canonicalNonempty(value string) bool {
	return value != "" && strings.TrimSpace(value) == value
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

// AdmitStableProviderSnapshot retains both reads and exposes comparison state
// only when the complete provider-state digests match.
func AdmitStableProviderSnapshot(first, second *ProviderCapture) (*SnapshotAdmission, error) {
	if first == nil || second == nil {
		return nil, fmt.Errorf("two provider captures are required")
	}
	if first.canonical.Provider != second.canonical.Provider || first.canonical.Namespace != second.canonical.Namespace ||
		first.canonical.AccountID != second.canonical.AccountID || first.canonical.Currency != second.canonical.Currency ||
		first.canonical.HorizonStart != second.canonical.HorizonStart || first.canonical.HorizonEnd != second.canonical.HorizonEnd {
		return nil, fmt.Errorf("capture scope changed between reads")
	}
	admission := &SnapshotAdmission{First: first, Second: second}
	if first.digest != second.digest {
		admission.Reason = ReasonSnapshotUnstable
		return admission, nil
	}
	canonical := struct {
		Schema      string `json:"schema"`
		StateSHA256 string `json:"provider_state_sha256"`
		FirstID     string `json:"first_capture_id"`
		SecondID    string `json:"second_capture_id"`
		FirstStart  string `json:"first_capture_start"`
		FirstEnd    string `json:"first_capture_end"`
		SecondStart string `json:"second_capture_start"`
		SecondEnd   string `json:"second_capture_end"`
	}{
		stableSnapshotSchemaV1, first.digest, first.id.String(), second.id.String(), canonicalTime(first.start),
		canonicalTime(first.end), canonicalTime(second.start), canonicalTime(second.end),
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal stable provider snapshot: %w", err)
	}
	digest := sha256Hex(encoded)
	snapshot := &StableProviderSnapshot{
		first: first, second: second, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(stableSnapshotDomain, stableSnapshotSchemaV1+"@sha256:"+digest),
	}
	admission.Snapshot = snapshot
	return admission, nil
}

func (capture *ProviderCapture) ID() uuid.UUID {
	if capture == nil {
		return uuid.Nil
	}
	return capture.id
}

func (capture *ProviderCapture) Digest() string {
	if capture == nil {
		return ""
	}
	return capture.digest
}

func (capture *ProviderCapture) CanonicalBytes() json.RawMessage {
	if capture == nil {
		return nil
	}
	return append(json.RawMessage(nil), capture.bytes...)
}

func (capture *ProviderCapture) Pages() []RawPage {
	if capture == nil {
		return nil
	}
	result := make([]RawPage, len(capture.canonical.Pages))
	for index, page := range capture.canonical.Pages {
		result[index] = RawPage{Cursor: page.Cursor, NextCursor: page.NextCursor, Terminal: page.Terminal, Raw: append(json.RawMessage(nil), page.Raw...)}
	}
	return result
}

func (capture *ProviderCapture) Positions() []ProviderPosition {
	if capture == nil {
		return nil
	}
	return append([]ProviderPosition(nil), capture.canonical.Positions...)
}

func (capture *ProviderCapture) Fills() []ProviderFill {
	if capture == nil {
		return nil
	}
	return append([]ProviderFill(nil), capture.canonical.Fills...)
}

func (capture *ProviderCapture) Provider() venue.Provider {
	if capture == nil {
		return ""
	}
	return capture.canonical.Provider
}

func (capture *ProviderCapture) AccountID() string {
	if capture == nil {
		return ""
	}
	return capture.canonical.AccountID
}

func (capture *ProviderCapture) Namespace() string {
	if capture == nil {
		return ""
	}
	return capture.canonical.Namespace
}

func (capture *ProviderCapture) Currency() string {
	if capture == nil {
		return ""
	}
	return capture.canonical.Currency
}

func (capture *ProviderCapture) HorizonStart() time.Time {
	if capture == nil {
		return time.Time{}
	}
	value, _ := time.Parse("2006-01-02T15:04:05.000000Z", capture.canonical.HorizonStart)
	return value
}

func (capture *ProviderCapture) HorizonEnd() time.Time {
	if capture == nil {
		return time.Time{}
	}
	value, _ := time.Parse("2006-01-02T15:04:05.000000Z", capture.canonical.HorizonEnd)
	return value
}

func (capture *ProviderCapture) CaptureStart() time.Time {
	if capture == nil {
		return time.Time{}
	}
	return capture.start
}

func (capture *ProviderCapture) CaptureEnd() time.Time {
	if capture == nil {
		return time.Time{}
	}
	return capture.end
}

func (snapshot *StableProviderSnapshot) ID() uuid.UUID {
	if snapshot == nil {
		return uuid.Nil
	}
	return snapshot.id
}

func (snapshot *StableProviderSnapshot) Digest() string {
	if snapshot == nil {
		return ""
	}
	return snapshot.digest
}

func (snapshot *StableProviderSnapshot) CanonicalBytes() json.RawMessage {
	if snapshot == nil {
		return nil
	}
	return append(json.RawMessage(nil), snapshot.bytes...)
}

func (snapshot *StableProviderSnapshot) Capture() *ProviderCapture {
	if snapshot == nil {
		return nil
	}
	return snapshot.first
}

func (snapshot *StableProviderSnapshot) Captures() (*ProviderCapture, *ProviderCapture) {
	if snapshot == nil {
		return nil, nil
	}
	return snapshot.first, snapshot.second
}

func sameCapturePayload(left, right *ProviderCapture) bool {
	return left != nil && right != nil && left.id == right.id && left.digest == right.digest && bytes.Equal(left.bytes, right.bytes)
}
