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

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const ComparisonVersion = "accounting_comparison_v1"

type ResultStatus string

const (
	StatusEqual         ResultStatus = "equal"
	StatusExplained     ResultStatus = "explained"
	StatusUnexplained   ResultStatus = "unexplained"
	StatusNotComparable ResultStatus = "not_comparable"
)

func (status ResultStatus) valid() bool {
	switch status {
	case StatusEqual, StatusExplained, StatusUnexplained, StatusNotComparable:
		return true
	default:
		return false
	}
}

type ExplanationCode string

const (
	ExplanationLegacyFloat            ExplanationCode = "legacy_binary_float_representation"
	ExplanationLegacyMarkPolicy       ExplanationCode = "legacy_mark_source_timing"
	ExplanationLegacyOptionMultiplier ExplanationCode = "legacy_option_multiplier_semantics"
	ExplanationSourceCorrection       ExplanationCode = "source_correction_evidence"
)

func (code ExplanationCode) valid() bool {
	switch code {
	case ExplanationLegacyFloat, ExplanationLegacyMarkPolicy, ExplanationLegacyOptionMultiplier, ExplanationSourceCorrection:
		return true
	default:
		return false
	}
}

type ExplanationInput struct {
	FactKey          string
	Code             ExplanationCode
	Rationale        string
	EvidenceRef      string
	EvidenceChecksum string
	Generator        string
	Reviewer         string
	ReviewedAt       time.Time
}

type Explanation struct {
	FactKey          string
	Code             ExplanationCode
	Rationale        string
	EvidenceRef      string
	EvidenceChecksum string
	Generator        string
	Reviewer         string
	ReviewedAt       time.Time
}

type ComparisonInput struct {
	Legacy       *Snapshot
	Ledger       *Snapshot
	Generator    string
	GeneratedAt  time.Time
	Explanations []ExplanationInput
}

type Result struct {
	ID          uuid.UUID
	FactKey     string
	LegacyValue *decimal.Decimal
	LedgerValue *decimal.Decimal
	Delta       *decimal.Decimal
	Status      ResultStatus
	ReasonCode  string
	Explanation *Explanation
}

type Run struct {
	ID                 uuid.UUID
	Version            string
	PolicyVersion      string
	AccountID          uuid.UUID
	AsOf               time.Time
	GeneratedAt        time.Time
	Generator          string
	ProjectionVersion  string
	MarkSource         string
	MarkNamespace      string
	MaxMarkAge         time.Duration
	CaptureFenceID     string
	CaptureEpoch       uint64
	Legacy             *Snapshot
	Ledger             *Snapshot
	Results            []Result
	EqualCount         int
	ExplainedCount     int
	UnexplainedCount   int
	NotComparableCount int
	Synthetic          bool
	PayloadBytes       []byte
	Checksum           string
	AttestationType    string
	AttestationKeyID   string
	Attestation        []byte
}

func Compare(input ComparisonInput) (*Run, error) {
	if input.Legacy == nil || input.Ledger == nil {
		return nil, fmt.Errorf("accounting comparison requires both snapshots")
	}
	if err := input.Legacy.Validate(); err != nil {
		return nil, fmt.Errorf("accounting comparison legacy snapshot: %w", err)
	}
	if err := input.Ledger.Validate(); err != nil {
		return nil, fmt.Errorf("accounting comparison ledger snapshot: %w", err)
	}
	if input.Legacy.Source != SourceLegacy || input.Ledger.Source != SourceLedger {
		return nil, fmt.Errorf("accounting comparison source roles are invalid")
	}
	if input.Legacy.AccountID != input.Ledger.AccountID || !input.Legacy.AsOf.Equal(input.Ledger.AsOf) ||
		input.Legacy.Currency != input.Ledger.Currency || input.Legacy.ProjectionVersion != input.Ledger.ProjectionVersion ||
		input.Legacy.MarkSource != input.Ledger.MarkSource || input.Legacy.MarkNamespace != input.Ledger.MarkNamespace ||
		input.Legacy.MaxMarkAge != input.Ledger.MaxMarkAge {
		return nil, fmt.Errorf("accounting comparison snapshots do not share one account/time/mark boundary")
	}
	if input.Legacy.CaptureFenceID != input.Ledger.CaptureFenceID || input.Legacy.CaptureEpoch != input.Ledger.CaptureEpoch {
		return nil, fmt.Errorf("accounting comparison snapshots do not share one capture fence")
	}
	if !normalizedRequired(input.Generator, 256) {
		return nil, fmt.Errorf("accounting comparison generator is required")
	}
	if err := requireUTCMicrosecond("comparison generated_at", input.GeneratedAt); err != nil {
		return nil, err
	}
	latestObservation := input.Legacy.ObservedAt
	if input.Ledger.ObservedAt.After(latestObservation) {
		latestObservation = input.Ledger.ObservedAt
	}
	if input.GeneratedAt.Before(latestObservation) {
		return nil, fmt.Errorf("accounting comparison generated_at precedes source observation")
	}

	explanations, err := validateExplanations(input.Explanations, input.Generator, latestObservation, input.GeneratedAt)
	if err != nil {
		return nil, err
	}

	run := &Run{
		Version: ComparisonVersion, PolicyVersion: PolicyVersion,
		AccountID: input.Legacy.AccountID, AsOf: input.Legacy.AsOf,
		GeneratedAt: input.GeneratedAt, Generator: input.Generator,
		ProjectionVersion: input.Legacy.ProjectionVersion,
		MarkSource:        input.Legacy.MarkSource, MarkNamespace: input.Legacy.MarkNamespace,
		MaxMarkAge: input.Legacy.MaxMarkAge, CaptureFenceID: input.Legacy.CaptureFenceID,
		CaptureEpoch: input.Legacy.CaptureEpoch, Legacy: input.Legacy, Ledger: input.Ledger,
		Synthetic: input.Legacy.Synthetic || input.Ledger.Synthetic,
	}

	legacyMetrics := metricIndex(input.Legacy)
	ledgerMetrics := metricIndex(input.Ledger)
	legacyMissing := missingIndex(input.Legacy)
	ledgerMissing := missingIndex(input.Ledger)
	for _, kind := range RequiredMetrics() {
		key := MetricFactKey(kind)
		legacyMetric, legacyOK := legacyMetrics[kind]
		ledgerMetric, ledgerOK := ledgerMetrics[kind]
		result, resultErr := compareFact(key, decimalPointer(legacyMetric.Value, legacyOK), decimalPointer(ledgerMetric.Value, ledgerOK), legacyMissing[key], ledgerMissing[key], explanations[key])
		if resultErr != nil {
			return nil, resultErr
		}
		run.Results = append(run.Results, result)
	}

	legacyPositions := positionIndex(input.Legacy)
	ledgerPositions := positionIndex(input.Ledger)
	positionIDs := make(map[uuid.UUID]struct{}, len(legacyPositions)+len(ledgerPositions))
	for id := range legacyPositions {
		positionIDs[id] = struct{}{}
	}
	for id := range ledgerPositions {
		positionIDs[id] = struct{}{}
	}
	orderedPositionIDs := make([]uuid.UUID, 0, len(positionIDs))
	for id := range positionIDs {
		orderedPositionIDs = append(orderedPositionIDs, id)
	}
	sort.Slice(orderedPositionIDs, func(i, j int) bool { return orderedPositionIDs[i].String() < orderedPositionIDs[j].String() })
	for _, id := range orderedPositionIDs {
		key := PositionFactKey(id)
		legacyPosition, legacyOK := legacyPositions[id]
		ledgerPosition, ledgerOK := ledgerPositions[id]
		var legacyValue, ledgerValue *decimal.Decimal
		if legacyOK {
			legacyValue = cloneDecimalPointer(legacyPosition.Quantity)
		} else if input.Legacy.PositionCoverageComplete {
			legacyValue = cloneDecimalPointer(decimal.Zero)
		}
		if ledgerOK {
			ledgerValue = cloneDecimalPointer(ledgerPosition.Quantity)
		} else if input.Ledger.PositionCoverageComplete {
			ledgerValue = cloneDecimalPointer(decimal.Zero)
		}
		result, resultErr := compareFact(key, legacyValue, ledgerValue, legacyMissing[key], ledgerMissing[key], explanations[key])
		if resultErr != nil {
			return nil, resultErr
		}
		run.Results = append(run.Results, result)
	}

	resultKeys := make(map[string]struct{}, len(run.Results))
	for _, result := range run.Results {
		resultKeys[result.FactKey] = struct{}{}
	}
	for key := range unionMissingKeys(legacyMissing, ledgerMissing) {
		if _, exists := resultKeys[key]; exists {
			continue
		}
		result, resultErr := compareFact(key, nil, nil, legacyMissing[key], ledgerMissing[key], explanations[key])
		if resultErr != nil {
			return nil, resultErr
		}
		run.Results = append(run.Results, result)
		resultKeys[key] = struct{}{}
	}
	for key := range explanations {
		if _, exists := resultKeys[key]; !exists {
			return nil, fmt.Errorf("accounting explanation targets unknown fact %q", key)
		}
	}

	sort.Slice(run.Results, func(i, j int) bool { return run.Results[i].FactKey < run.Results[j].FactKey })
	for index := range run.Results {
		result := &run.Results[index]
		result.ID = economicid.DeterministicUUID("accounting-reconciliation-result", input.Legacy.ID.String(), input.Ledger.ID.String(), result.FactKey, string(result.Status), canonicalOptionalDecimal(result.LegacyValue), canonicalOptionalDecimal(result.LedgerValue), canonicalOptionalDecimal(result.Delta), explanationIdentity(result.Explanation))
		switch result.Status {
		case StatusEqual:
			run.EqualCount++
		case StatusExplained:
			run.ExplainedCount++
		case StatusUnexplained:
			run.UnexplainedCount++
		case StatusNotComparable:
			run.NotComparableCount++
		}
	}

	payload, err := run.canonicalPayload()
	if err != nil {
		return nil, err
	}
	run.PayloadBytes = payload
	sum := sha256.Sum256(payload)
	run.Checksum = hex.EncodeToString(sum[:])
	run.ID = economicid.DeterministicUUID("accounting-reconciliation-run", run.Version, run.PolicyVersion, run.AccountID.String(), run.Legacy.Checksum, run.Ledger.Checksum, run.Checksum)
	return run, nil
}

func (run *Run) Validate() error {
	if run == nil || run.Legacy == nil || run.Ledger == nil {
		return fmt.Errorf("accounting reconciliation run is required")
	}
	currentPayload, err := run.canonicalPayload()
	if err != nil {
		return err
	}
	if !bytes.Equal(currentPayload, run.PayloadBytes) {
		return fmt.Errorf("accounting reconciliation mutable fields differ from canonical bytes")
	}
	explanations := make([]ExplanationInput, 0)
	for _, result := range run.Results {
		if result.Explanation != nil {
			explanations = append(explanations, ExplanationInput(*result.Explanation))
		}
	}
	rebuilt, err := Compare(ComparisonInput{Legacy: run.Legacy, Ledger: run.Ledger, Generator: run.Generator, GeneratedAt: run.GeneratedAt, Explanations: explanations})
	if err != nil {
		return err
	}
	if rebuilt.ID != run.ID || rebuilt.Checksum != run.Checksum || !bytes.Equal(rebuilt.PayloadBytes, run.PayloadBytes) ||
		rebuilt.EqualCount != run.EqualCount || rebuilt.ExplainedCount != run.ExplainedCount ||
		rebuilt.UnexplainedCount != run.UnexplainedCount || rebuilt.NotComparableCount != run.NotComparableCount {
		return fmt.Errorf("accounting reconciliation run canonical identity, bytes, or counts do not match")
	}
	return nil
}

func compareFact(key string, legacyValue, ledgerValue *decimal.Decimal, legacyMissing, ledgerMissing *MissingFact, explanation *Explanation) (Result, error) {
	result := Result{FactKey: key, LegacyValue: cloneOptionalDecimal(legacyValue), LedgerValue: cloneOptionalDecimal(ledgerValue)}
	if legacyValue == nil || ledgerValue == nil {
		if explanation != nil {
			return Result{}, fmt.Errorf("accounting explanation cannot classify missing fact %q", key)
		}
		result.Status = StatusNotComparable
		result.ReasonCode = missingReasonSummary(legacyValue, ledgerValue, legacyMissing, ledgerMissing)
		return result, nil
	}
	delta := ledgerValue.Sub(*legacyValue)
	result.Delta = cloneDecimalPointer(delta)
	if delta.IsZero() {
		if explanation != nil {
			return Result{}, fmt.Errorf("accounting explanation cannot classify equal fact %q", key)
		}
		result.Status = StatusEqual
		result.ReasonCode = "exact_match"
		return result, nil
	}
	if explanation == nil {
		result.Status = StatusUnexplained
		result.ReasonCode = "exact_delta_unexplained"
		return result, nil
	}
	cloned := *explanation
	result.Status = StatusExplained
	result.ReasonCode = string(cloned.Code)
	result.Explanation = &cloned
	return result, nil
}

func validateExplanations(inputs []ExplanationInput, generator string, earliest, generatedAt time.Time) (map[string]*Explanation, error) {
	out := make(map[string]*Explanation, len(inputs))
	for _, input := range inputs {
		if !validFactKey(input.FactKey) || !input.Code.valid() || !normalizedRequired(input.Rationale, 2000) ||
			!normalizedRequired(input.EvidenceRef, maxEvidenceLength) || !validSHA256(input.EvidenceChecksum) ||
			!normalizedRequired(input.Generator, 256) || !normalizedRequired(input.Reviewer, 256) || input.Generator != generator || input.Generator == input.Reviewer {
			return nil, fmt.Errorf("accounting explanation for %q is invalid", input.FactKey)
		}
		if err := requireUTCMicrosecond("explanation reviewed_at", input.ReviewedAt); err != nil {
			return nil, err
		}
		if input.ReviewedAt.Before(earliest) || input.ReviewedAt.After(generatedAt) {
			return nil, fmt.Errorf("accounting explanation review time for %q is outside the evidence window", input.FactKey)
		}
		if _, exists := out[input.FactKey]; exists {
			return nil, fmt.Errorf("accounting explanation for %q is duplicated", input.FactKey)
		}
		value := Explanation(input)
		out[input.FactKey] = &value
	}
	return out, nil
}

func (run *Run) canonicalPayload() ([]byte, error) {
	type explanationPayload struct {
		FactKey          string `json:"fact_key"`
		Code             string `json:"code"`
		Rationale        string `json:"rationale"`
		EvidenceRef      string `json:"evidence_ref"`
		EvidenceChecksum string `json:"evidence_checksum"`
		Generator        string `json:"generator"`
		Reviewer         string `json:"reviewer"`
		ReviewedAt       string `json:"reviewed_at"`
	}
	type resultPayload struct {
		ID          string              `json:"id"`
		FactKey     string              `json:"fact_key"`
		LegacyValue *string             `json:"legacy_value"`
		LedgerValue *string             `json:"ledger_value"`
		Delta       *string             `json:"delta"`
		Status      string              `json:"status"`
		ReasonCode  string              `json:"reason_code"`
		Explanation *explanationPayload `json:"explanation"`
	}
	payload := struct {
		Version                string          `json:"version"`
		PolicyVersion          string          `json:"policy_version"`
		AccountID              string          `json:"account_id"`
		AsOf                   string          `json:"as_of"`
		GeneratedAt            string          `json:"generated_at"`
		Generator              string          `json:"generator"`
		ProjectionVersion      string          `json:"projection_version"`
		MarkSource             string          `json:"mark_source"`
		MarkNamespace          string          `json:"mark_namespace"`
		MaxMarkAgeMicroseconds int64           `json:"max_mark_age_microseconds"`
		CaptureFenceID         string          `json:"capture_fence_id"`
		CaptureEpoch           uint64          `json:"capture_epoch"`
		LegacySnapshot         json.RawMessage `json:"legacy_snapshot"`
		LedgerSnapshot         json.RawMessage `json:"ledger_snapshot"`
		Synthetic              bool            `json:"synthetic"`
		EqualCount             int             `json:"equal_count"`
		ExplainedCount         int             `json:"explained_count"`
		UnexplainedCount       int             `json:"unexplained_count"`
		NotComparableCount     int             `json:"not_comparable_count"`
		Results                []resultPayload `json:"results"`
	}{
		Version: run.Version, PolicyVersion: run.PolicyVersion, AccountID: run.AccountID.String(),
		AsOf: run.AsOf.Format(timestampLayout), GeneratedAt: run.GeneratedAt.Format(timestampLayout), Generator: run.Generator,
		ProjectionVersion: run.ProjectionVersion, MarkSource: run.MarkSource, MarkNamespace: run.MarkNamespace,
		MaxMarkAgeMicroseconds: run.MaxMarkAge.Microseconds(), CaptureFenceID: run.CaptureFenceID, CaptureEpoch: run.CaptureEpoch,
		LegacySnapshot: append(json.RawMessage(nil), run.Legacy.PayloadBytes...), LedgerSnapshot: append(json.RawMessage(nil), run.Ledger.PayloadBytes...),
		Synthetic: run.Synthetic, EqualCount: run.EqualCount, ExplainedCount: run.ExplainedCount,
		UnexplainedCount: run.UnexplainedCount, NotComparableCount: run.NotComparableCount,
		Results: make([]resultPayload, 0, len(run.Results)),
	}
	for _, result := range run.Results {
		encoded := resultPayload{ID: result.ID.String(), FactKey: result.FactKey, LegacyValue: decimalStringPointer(result.LegacyValue), LedgerValue: decimalStringPointer(result.LedgerValue), Delta: decimalStringPointer(result.Delta), Status: string(result.Status), ReasonCode: result.ReasonCode}
		if result.Explanation != nil {
			explanation := result.Explanation
			encoded.Explanation = &explanationPayload{FactKey: explanation.FactKey, Code: string(explanation.Code), Rationale: explanation.Rationale, EvidenceRef: explanation.EvidenceRef, EvidenceChecksum: explanation.EvidenceChecksum, Generator: explanation.Generator, Reviewer: explanation.Reviewer, ReviewedAt: explanation.ReviewedAt.Format(timestampLayout)}
		}
		payload.Results = append(payload.Results, encoded)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal accounting comparison: %w", err)
	}
	return encoded, nil
}

func metricIndex(snapshot *Snapshot) map[MetricKind]Metric {
	out := make(map[MetricKind]Metric, len(snapshot.Metrics))
	for _, value := range snapshot.Metrics {
		out[value.Kind] = value
	}
	return out
}

func positionIndex(snapshot *Snapshot) map[uuid.UUID]Position {
	out := make(map[uuid.UUID]Position, len(snapshot.Positions))
	for _, value := range snapshot.Positions {
		out[value.InstrumentID] = value
	}
	return out
}

func missingIndex(snapshot *Snapshot) map[string]*MissingFact {
	out := make(map[string]*MissingFact, len(snapshot.Missing))
	for index := range snapshot.Missing {
		value := snapshot.Missing[index]
		out[value.FactKey] = &value
	}
	return out
}

func unionMissingKeys(left, right map[string]*MissingFact) map[string]struct{} {
	out := make(map[string]struct{}, len(left)+len(right))
	for key := range left {
		out[key] = struct{}{}
	}
	for key := range right {
		out[key] = struct{}{}
	}
	return out
}

func decimalPointer(value decimal.Decimal, present bool) *decimal.Decimal {
	if !present {
		return nil
	}
	return cloneDecimalPointer(value)
}

func cloneDecimalPointer(value decimal.Decimal) *decimal.Decimal {
	cloned := value
	return &cloned
}

func cloneOptionalDecimal(value *decimal.Decimal) *decimal.Decimal {
	if value == nil {
		return nil
	}
	return cloneDecimalPointer(*value)
}

func decimalStringPointer(value *decimal.Decimal) *string {
	if value == nil {
		return nil
	}
	encoded := value.String()
	return &encoded
}

func canonicalOptionalDecimal(value *decimal.Decimal) string {
	if value == nil {
		return "missing"
	}
	return value.String()
}

func explanationIdentity(value *Explanation) string {
	if value == nil {
		return ""
	}
	return strings.Join([]string{value.FactKey, string(value.Code), value.Rationale, value.EvidenceRef, value.EvidenceChecksum, value.Generator, value.Reviewer, value.ReviewedAt.Format(timestampLayout)}, "\x1f")
}

func missingReasonSummary(legacyValue, ledgerValue *decimal.Decimal, legacyMissing, ledgerMissing *MissingFact) string {
	parts := make([]string, 0, 2)
	if legacyValue == nil {
		parts = append(parts, "legacy:"+missingReason(legacyMissing))
	}
	if ledgerValue == nil {
		parts = append(parts, "ledger:"+missingReason(ledgerMissing))
	}
	return strings.Join(parts, ",")
}

func missingReason(value *MissingFact) string {
	if value == nil {
		return string(MissingSourceUnavailable)
	}
	return string(value.ReasonCode)
}
