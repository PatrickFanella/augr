package accountingrecon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func DecodeSnapshot(payloadBytes []byte) (*Snapshot, error) {
	type metricPayload struct {
		Kind       MetricKind      `json:"kind"`
		Value      string          `json:"value"`
		Provenance ValueProvenance `json:"provenance"`
	}
	type positionPayload struct {
		InstrumentID string          `json:"instrument_id"`
		Quantity     string          `json:"quantity"`
		Provenance   ValueProvenance `json:"provenance"`
	}
	type missingPayload struct {
		FactKey     string        `json:"fact_key"`
		ReasonCode  MissingReason `json:"reason_code"`
		EvidenceRef string        `json:"evidence_ref"`
	}
	var payload struct {
		Version                  string            `json:"version"`
		Source                   SnapshotSource    `json:"source"`
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
	}
	decoder := json.NewDecoder(bytes.NewReader(payloadBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode accounting snapshot: %w", err)
	}
	if trailingErr := decoder.Decode(&struct{}{}); payload.Version != SnapshotVersion || trailingErr != io.EOF {
		return nil, fmt.Errorf("decode accounting snapshot: version or trailing data is invalid")
	}
	accountID, err := uuid.Parse(payload.AccountID)
	if err != nil {
		return nil, fmt.Errorf("decode accounting snapshot account: %w", err)
	}
	asOf, err := time.Parse(timestampLayout, payload.AsOf)
	if err != nil {
		return nil, fmt.Errorf("decode accounting snapshot as_of: %w", err)
	}
	observedAt, err := time.Parse(timestampLayout, payload.ObservedAt)
	if err != nil {
		return nil, fmt.Errorf("decode accounting snapshot observed_at: %w", err)
	}
	input := SnapshotInput{
		Source: payload.Source, AccountID: accountID, AsOf: asOf, ObservedAt: observedAt,
		Currency: payload.Currency, ProjectionVersion: payload.ProjectionVersion,
		MarkSource: payload.MarkSource, MarkNamespace: payload.MarkNamespace,
		MaxMarkAge:     time.Duration(payload.MaxMarkAgeMicroseconds) * time.Microsecond,
		CaptureFenceID: payload.CaptureFenceID, CaptureEpoch: payload.CaptureEpoch,
		EvidenceID: payload.EvidenceID, EvidenceChecksum: payload.EvidenceChecksum,
		Synthetic: payload.Synthetic, PositionCoverageComplete: payload.PositionCoverageComplete,
	}
	for _, metric := range payload.Metrics {
		value, parseErr := decimal.NewFromString(metric.Value)
		if parseErr != nil {
			return nil, fmt.Errorf("decode accounting snapshot metric %s: %w", metric.Kind, parseErr)
		}
		input.Metrics = append(input.Metrics, MetricInput{Kind: metric.Kind, Value: value, Provenance: metric.Provenance})
	}
	for _, position := range payload.Positions {
		instrumentID, parseErr := uuid.Parse(position.InstrumentID)
		if parseErr != nil {
			return nil, fmt.Errorf("decode accounting snapshot position identity: %w", parseErr)
		}
		quantity, parseErr := decimal.NewFromString(position.Quantity)
		if parseErr != nil {
			return nil, fmt.Errorf("decode accounting snapshot position quantity: %w", parseErr)
		}
		input.Positions = append(input.Positions, PositionInput{InstrumentID: instrumentID, Quantity: quantity, Provenance: position.Provenance})
	}
	for _, missing := range payload.Missing {
		input.Missing = append(input.Missing, MissingFactInput(missing))
	}
	snapshot, err := NewSnapshot(input)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(snapshot.PayloadBytes, payloadBytes) {
		return nil, fmt.Errorf("accounting snapshot bytes are not canonical")
	}
	return snapshot, nil
}

func DecodeRun(payloadBytes []byte) (*Run, error) {
	type explanationPayload struct {
		FactKey          string          `json:"fact_key"`
		Code             ExplanationCode `json:"code"`
		Rationale        string          `json:"rationale"`
		EvidenceRef      string          `json:"evidence_ref"`
		EvidenceChecksum string          `json:"evidence_checksum"`
		Generator        string          `json:"generator"`
		Reviewer         string          `json:"reviewer"`
		ReviewedAt       string          `json:"reviewed_at"`
	}
	type resultPayload struct {
		Explanation *explanationPayload `json:"explanation"`
	}
	var payload struct {
		Version        string          `json:"version"`
		PolicyVersion  string          `json:"policy_version"`
		GeneratedAt    string          `json:"generated_at"`
		Generator      string          `json:"generator"`
		LegacySnapshot json.RawMessage `json:"legacy_snapshot"`
		LedgerSnapshot json.RawMessage `json:"ledger_snapshot"`
		Results        []resultPayload `json:"results"`
	}
	if err := json.Unmarshal(payloadBytes, &payload); err != nil {
		return nil, fmt.Errorf("decode accounting reconciliation: %w", err)
	}
	if payload.Version != ComparisonVersion || payload.PolicyVersion != PolicyVersion {
		return nil, fmt.Errorf("decode accounting reconciliation version is invalid")
	}
	legacy, err := DecodeSnapshot(payload.LegacySnapshot)
	if err != nil {
		return nil, fmt.Errorf("decode accounting reconciliation legacy snapshot: %w", err)
	}
	ledgerSnapshot, err := DecodeSnapshot(payload.LedgerSnapshot)
	if err != nil {
		return nil, fmt.Errorf("decode accounting reconciliation ledger snapshot: %w", err)
	}
	generatedAt, err := time.Parse(timestampLayout, payload.GeneratedAt)
	if err != nil {
		return nil, fmt.Errorf("decode accounting reconciliation generated_at: %w", err)
	}
	explanations := make([]ExplanationInput, 0)
	for _, result := range payload.Results {
		if result.Explanation == nil {
			continue
		}
		reviewedAt, parseErr := time.Parse(timestampLayout, result.Explanation.ReviewedAt)
		if parseErr != nil {
			return nil, fmt.Errorf("decode accounting reconciliation explanation time: %w", parseErr)
		}
		explanations = append(explanations, ExplanationInput{
			FactKey: result.Explanation.FactKey, Code: result.Explanation.Code,
			Rationale: result.Explanation.Rationale, EvidenceRef: result.Explanation.EvidenceRef,
			EvidenceChecksum: result.Explanation.EvidenceChecksum, Generator: result.Explanation.Generator,
			Reviewer: result.Explanation.Reviewer, ReviewedAt: reviewedAt,
		})
	}
	run, err := Compare(ComparisonInput{Legacy: legacy, Ledger: ledgerSnapshot, Generator: payload.Generator, GeneratedAt: generatedAt, Explanations: explanations})
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(run.PayloadBytes, payloadBytes) {
		return nil, fmt.Errorf("accounting reconciliation bytes are not canonical")
	}
	return run, nil
}
