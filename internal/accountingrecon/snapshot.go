// Package accountingrecon compares immutable-ledger accounting with legacy
// compatibility accounting without making either source mutable or authoritative
// over the other.
package accountingrecon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	SnapshotVersion   = "accounting_snapshot_v1"
	PolicyVersion     = "legacy_ledger_v1"
	timestampLayout   = "2006-01-02T15:04:05.000000Z"
	maxFactKeyLength  = 256
	maxEvidenceLength = 512
)

type SnapshotSource string

const (
	SourceLegacy SnapshotSource = "legacy_compatibility"
	SourceLedger SnapshotSource = "immutable_ledger"
)

func (source SnapshotSource) String() string { return string(source) }

func (source SnapshotSource) valid() bool { return source == SourceLegacy || source == SourceLedger }

type MetricKind string

const (
	MetricCash          MetricKind = "cash"
	MetricBuyingPower   MetricKind = "buying_power"
	MetricFees          MetricKind = "fees"
	MetricRealizedPnL   MetricKind = "realized_pnl"
	MetricUnrealizedPnL MetricKind = "unrealized_pnl"
	MetricMarketValue   MetricKind = "market_value"
	MetricEquity        MetricKind = "equity"
)

var requiredMetrics = []MetricKind{
	MetricBuyingPower,
	MetricCash,
	MetricEquity,
	MetricFees,
	MetricMarketValue,
	MetricRealizedPnL,
	MetricUnrealizedPnL,
}

func RequiredMetrics() []MetricKind { return append([]MetricKind(nil), requiredMetrics...) }

func (kind MetricKind) valid() bool {
	for _, candidate := range requiredMetrics {
		if kind == candidate {
			return true
		}
	}
	return false
}

type ValueProvenance string

const (
	ProvenanceExactDecimal ValueProvenance = "exact_decimal"
	ProvenanceBinaryFloat  ValueProvenance = "binary_float"
)

func (provenance ValueProvenance) valid() bool {
	return provenance == ProvenanceExactDecimal || provenance == ProvenanceBinaryFloat
}

type MissingReason string

const (
	MissingSourceUnavailable  MissingReason = "source_unavailable"
	MissingUnscopedLegacyRows MissingReason = "unscoped_legacy_rows"
	MissingInstrumentIdentity MissingReason = "instrument_identity_unresolved"
	MissingCaptureAtomicity   MissingReason = "capture_not_atomic"
)

func (reason MissingReason) valid() bool {
	switch reason {
	case MissingSourceUnavailable, MissingUnscopedLegacyRows, MissingInstrumentIdentity, MissingCaptureAtomicity:
		return true
	default:
		return false
	}
}

type MetricInput struct {
	Kind       MetricKind
	Value      decimal.Decimal
	Provenance ValueProvenance
}

type PositionInput struct {
	InstrumentID uuid.UUID
	Quantity     decimal.Decimal
	Provenance   ValueProvenance
}

type MissingFactInput struct {
	FactKey     string
	ReasonCode  MissingReason
	EvidenceRef string
}

type SnapshotInput struct {
	Source                   SnapshotSource
	AccountID                uuid.UUID
	AsOf                     time.Time
	ObservedAt               time.Time
	Currency                 string
	ProjectionVersion        string
	MarkSource               string
	MarkNamespace            string
	MaxMarkAge               time.Duration
	CaptureFenceID           string
	CaptureEpoch             uint64
	EvidenceID               string
	EvidenceChecksum         string
	Synthetic                bool
	PositionCoverageComplete bool
	Metrics                  []MetricInput
	Positions                []PositionInput
	Missing                  []MissingFactInput
}

type Metric struct {
	Kind       MetricKind
	Value      decimal.Decimal
	Provenance ValueProvenance
}

type Position struct {
	InstrumentID uuid.UUID
	Quantity     decimal.Decimal
	Provenance   ValueProvenance
}

type MissingFact struct {
	FactKey     string
	ReasonCode  MissingReason
	EvidenceRef string
}

type Snapshot struct {
	ID                       uuid.UUID
	Version                  string
	Source                   SnapshotSource
	AccountID                uuid.UUID
	AsOf                     time.Time
	ObservedAt               time.Time
	Currency                 string
	ProjectionVersion        string
	MarkSource               string
	MarkNamespace            string
	MaxMarkAge               time.Duration
	CaptureFenceID           string
	CaptureEpoch             uint64
	EvidenceID               string
	EvidenceChecksum         string
	Synthetic                bool
	PositionCoverageComplete bool
	Metrics                  []Metric
	Positions                []Position
	Missing                  []MissingFact
	PayloadBytes             []byte
	Checksum                 string
}

func MetricFactKey(kind MetricKind) string { return "metric:" + string(kind) }

func PositionFactKey(instrumentID uuid.UUID) string {
	return "position:" + instrumentID.String() + ":quantity"
}

func NewSnapshot(input SnapshotInput) (*Snapshot, error) {
	if !input.Source.valid() {
		return nil, fmt.Errorf("accounting snapshot source %q is invalid", input.Source)
	}
	if input.AccountID == uuid.Nil {
		return nil, fmt.Errorf("accounting snapshot account ID is required")
	}
	if err := requireUTCMicrosecond("as_of", input.AsOf); err != nil {
		return nil, err
	}
	if err := requireUTCMicrosecond("observed_at", input.ObservedAt); err != nil {
		return nil, err
	}
	if input.ObservedAt.Before(input.AsOf) {
		return nil, fmt.Errorf("accounting snapshot observed_at precedes as_of")
	}
	if len(input.Currency) != 3 || input.Currency != strings.ToUpper(input.Currency) || !asciiLetters(input.Currency) {
		return nil, fmt.Errorf("accounting snapshot currency %q is invalid", input.Currency)
	}
	if !normalizedRequired(input.ProjectionVersion, 128) || !normalizedLower(input.MarkSource, 128) || !normalizedRequired(input.MarkNamespace, 256) {
		return nil, fmt.Errorf("accounting snapshot projection and mark policy is invalid")
	}
	if input.MaxMarkAge <= 0 || input.MaxMarkAge != input.MaxMarkAge.Truncate(time.Microsecond) {
		return nil, fmt.Errorf("accounting snapshot max mark age must be positive microseconds")
	}
	if !normalizedRequired(input.CaptureFenceID, 256) || input.CaptureEpoch == 0 {
		return nil, fmt.Errorf("accounting snapshot capture fence identity and epoch are required")
	}
	if !normalizedRequired(input.EvidenceID, maxEvidenceLength) || !validSHA256(input.EvidenceChecksum) {
		return nil, fmt.Errorf("accounting snapshot evidence identity and SHA-256 are required")
	}

	snapshot := &Snapshot{
		Version: SnapshotVersion, Source: input.Source, AccountID: input.AccountID,
		AsOf: input.AsOf, ObservedAt: input.ObservedAt, Currency: input.Currency,
		ProjectionVersion: input.ProjectionVersion, MarkSource: input.MarkSource,
		MarkNamespace: input.MarkNamespace, MaxMarkAge: input.MaxMarkAge,
		CaptureFenceID: input.CaptureFenceID, CaptureEpoch: input.CaptureEpoch,
		EvidenceID: input.EvidenceID, EvidenceChecksum: input.EvidenceChecksum,
		Synthetic: input.Synthetic, PositionCoverageComplete: input.PositionCoverageComplete,
	}

	metricKeys := make(map[string]struct{}, len(input.Metrics))
	for _, value := range input.Metrics {
		key := MetricFactKey(value.Kind)
		if !value.Kind.valid() || !value.Provenance.valid() || !validExactDecimal(value.Value) {
			return nil, fmt.Errorf("accounting snapshot metric %q is invalid", value.Kind)
		}
		if _, exists := metricKeys[key]; exists {
			return nil, fmt.Errorf("accounting snapshot metric %q is duplicated", value.Kind)
		}
		metricKeys[key] = struct{}{}
		snapshot.Metrics = append(snapshot.Metrics, Metric{Kind: value.Kind, Value: normalizeDecimal(value.Value), Provenance: value.Provenance})
	}
	sort.Slice(snapshot.Metrics, func(i, j int) bool { return snapshot.Metrics[i].Kind < snapshot.Metrics[j].Kind })

	positionKeys := make(map[string]struct{}, len(input.Positions))
	for _, value := range input.Positions {
		key := PositionFactKey(value.InstrumentID)
		if value.InstrumentID == uuid.Nil || !value.Provenance.valid() || !validExactDecimal(value.Quantity) {
			return nil, fmt.Errorf("accounting snapshot position %s is invalid", value.InstrumentID)
		}
		if _, exists := positionKeys[key]; exists {
			return nil, fmt.Errorf("accounting snapshot position %s is duplicated", value.InstrumentID)
		}
		positionKeys[key] = struct{}{}
		snapshot.Positions = append(snapshot.Positions, Position{InstrumentID: value.InstrumentID, Quantity: normalizeDecimal(value.Quantity), Provenance: value.Provenance})
	}
	sort.Slice(snapshot.Positions, func(i, j int) bool {
		return snapshot.Positions[i].InstrumentID.String() < snapshot.Positions[j].InstrumentID.String()
	})

	missingKeys := make(map[string]struct{}, len(input.Missing))
	for _, value := range input.Missing {
		if !validFactKey(value.FactKey) || !value.ReasonCode.valid() || !normalizedRequired(value.EvidenceRef, maxEvidenceLength) {
			return nil, fmt.Errorf("accounting snapshot missing fact %q is invalid", value.FactKey)
		}
		if _, exists := metricKeys[value.FactKey]; exists {
			return nil, fmt.Errorf("accounting snapshot fact %q is both present and missing", value.FactKey)
		}
		if _, exists := positionKeys[value.FactKey]; exists {
			return nil, fmt.Errorf("accounting snapshot fact %q is both present and missing", value.FactKey)
		}
		if _, exists := missingKeys[value.FactKey]; exists {
			return nil, fmt.Errorf("accounting snapshot missing fact %q is duplicated", value.FactKey)
		}
		missingKeys[value.FactKey] = struct{}{}
		snapshot.Missing = append(snapshot.Missing, MissingFact(value))
	}
	sort.Slice(snapshot.Missing, func(i, j int) bool { return snapshot.Missing[i].FactKey < snapshot.Missing[j].FactKey })

	payload, err := snapshot.canonicalPayload()
	if err != nil {
		return nil, err
	}
	snapshot.PayloadBytes = payload
	sum := sha256.Sum256(payload)
	snapshot.Checksum = hex.EncodeToString(sum[:])
	snapshot.ID = economicid.DeterministicUUID(
		"accounting-reconciliation-snapshot",
		snapshot.Version, snapshot.Source.String(), snapshot.AccountID.String(), snapshot.Checksum,
	)
	return snapshot, nil
}

func (snapshot *Snapshot) Validate() error {
	if snapshot == nil {
		return fmt.Errorf("accounting snapshot is required")
	}
	currentPayload, err := snapshot.canonicalPayload()
	if err != nil {
		return err
	}
	if !bytes.Equal(currentPayload, snapshot.PayloadBytes) {
		return fmt.Errorf("accounting snapshot mutable fields differ from canonical bytes")
	}
	input := SnapshotInput{
		Source: snapshot.Source, AccountID: snapshot.AccountID, AsOf: snapshot.AsOf,
		ObservedAt: snapshot.ObservedAt, Currency: snapshot.Currency,
		ProjectionVersion: snapshot.ProjectionVersion, MarkSource: snapshot.MarkSource,
		MarkNamespace: snapshot.MarkNamespace, MaxMarkAge: snapshot.MaxMarkAge,
		CaptureFenceID: snapshot.CaptureFenceID, CaptureEpoch: snapshot.CaptureEpoch,
		EvidenceID: snapshot.EvidenceID, EvidenceChecksum: snapshot.EvidenceChecksum,
		Synthetic: snapshot.Synthetic, PositionCoverageComplete: snapshot.PositionCoverageComplete,
	}
	for _, metric := range snapshot.Metrics {
		input.Metrics = append(input.Metrics, MetricInput(metric))
	}
	for _, position := range snapshot.Positions {
		input.Positions = append(input.Positions, PositionInput(position))
	}
	for _, missing := range snapshot.Missing {
		input.Missing = append(input.Missing, MissingFactInput(missing))
	}
	rebuilt, err := NewSnapshot(input)
	if err != nil {
		return err
	}
	if rebuilt.ID != snapshot.ID || rebuilt.Checksum != snapshot.Checksum || !bytes.Equal(rebuilt.PayloadBytes, snapshot.PayloadBytes) {
		return fmt.Errorf("accounting snapshot canonical identity or bytes do not match")
	}
	return nil
}

func (snapshot *Snapshot) canonicalPayload() ([]byte, error) {
	type metricPayload struct {
		Kind       string `json:"kind"`
		Value      string `json:"value"`
		Provenance string `json:"provenance"`
	}
	type positionPayload struct {
		InstrumentID string `json:"instrument_id"`
		Quantity     string `json:"quantity"`
		Provenance   string `json:"provenance"`
	}
	type missingPayload struct {
		FactKey     string `json:"fact_key"`
		ReasonCode  string `json:"reason_code"`
		EvidenceRef string `json:"evidence_ref"`
	}
	payload := struct {
		Version                  string            `json:"version"`
		Source                   string            `json:"source"`
		AccountID                string            `json:"account_id"`
		AsOf                     string            `json:"as_of"`
		ObservedAt               string            `json:"observed_at"`
		Currency                 string            `json:"currency"`
		ProjectionVersion        string            `json:"projection_version"`
		MarkSource               string            `json:"mark_source"`
		MarkNamespace            string            `json:"mark_namespace"`
		MaxMarkAgeMicroseconds   int64             `json:"max_mark_age_microseconds"`
		CaptureFenceID           string            `json:"capture_fence_id"`
		CaptureEpoch             uint64            `json:"capture_epoch"`
		EvidenceID               string            `json:"evidence_id"`
		EvidenceChecksum         string            `json:"evidence_checksum"`
		Synthetic                bool              `json:"synthetic"`
		PositionCoverageComplete bool              `json:"position_coverage_complete"`
		Metrics                  []metricPayload   `json:"metrics"`
		Positions                []positionPayload `json:"positions"`
		Missing                  []missingPayload  `json:"missing"`
	}{
		Version: snapshot.Version, Source: snapshot.Source.String(), AccountID: snapshot.AccountID.String(),
		AsOf: snapshot.AsOf.Format(timestampLayout), ObservedAt: snapshot.ObservedAt.Format(timestampLayout), Currency: snapshot.Currency,
		ProjectionVersion: snapshot.ProjectionVersion, MarkSource: snapshot.MarkSource, MarkNamespace: snapshot.MarkNamespace,
		MaxMarkAgeMicroseconds: snapshot.MaxMarkAge.Microseconds(), CaptureFenceID: snapshot.CaptureFenceID,
		CaptureEpoch: snapshot.CaptureEpoch, EvidenceID: snapshot.EvidenceID, EvidenceChecksum: snapshot.EvidenceChecksum,
		Synthetic: snapshot.Synthetic, PositionCoverageComplete: snapshot.PositionCoverageComplete,
		Metrics: make([]metricPayload, 0, len(snapshot.Metrics)), Positions: make([]positionPayload, 0, len(snapshot.Positions)), Missing: make([]missingPayload, 0, len(snapshot.Missing)),
	}
	for _, metric := range snapshot.Metrics {
		payload.Metrics = append(payload.Metrics, metricPayload{Kind: string(metric.Kind), Value: metric.Value.String(), Provenance: string(metric.Provenance)})
	}
	for _, position := range snapshot.Positions {
		payload.Positions = append(payload.Positions, positionPayload{InstrumentID: position.InstrumentID.String(), Quantity: position.Quantity.String(), Provenance: string(position.Provenance)})
	}
	for _, missing := range snapshot.Missing {
		payload.Missing = append(payload.Missing, missingPayload{FactKey: missing.FactKey, ReasonCode: string(missing.ReasonCode), EvidenceRef: missing.EvidenceRef})
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal accounting snapshot: %w", err)
	}
	return encoded, nil
}

func requireUTCMicrosecond(field string, value time.Time) error {
	if value.IsZero() {
		return fmt.Errorf("accounting snapshot %s is required", field)
	}
	_, offset := value.Zone()
	if offset != 0 || value != value.Truncate(time.Microsecond) {
		return fmt.Errorf("accounting snapshot %s must be UTC microsecond precision", field)
	}
	return nil
}

func validExactDecimal(value decimal.Decimal) bool {
	normalized := normalizeDecimal(value)
	parts := strings.Split(normalized.String(), ".")
	integer := strings.TrimPrefix(parts[0], "-")
	if len(integer) > 26 {
		return false
	}
	return len(parts) == 1 || len(parts[1]) <= 18
}

func normalizeDecimal(value decimal.Decimal) decimal.Decimal {
	if value.IsZero() {
		return decimal.Zero
	}
	return value
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func normalizedRequired(value string, maxLength int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maxLength && !strings.ContainsAny(value, "\r\n\x00")
}

func normalizedLower(value string, maxLength int) bool {
	return normalizedRequired(value, maxLength) && value == strings.ToLower(value)
}

func validFactKey(value string) bool {
	if !normalizedRequired(value, maxFactKeyLength) {
		return false
	}
	for _, character := range value {
		if unicode.IsLower(character) || unicode.IsDigit(character) || strings.ContainsRune(":-_", character) {
			continue
		}
		return false
	}
	return true
}

func asciiLetters(value string) bool {
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}
