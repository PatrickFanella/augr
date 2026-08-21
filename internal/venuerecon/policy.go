// Package venuerecon owns exact, read-only venue reconciliation evidence. It
// cannot submit/cancel orders or mutate ledger, fill, cash, or position state.
package venuerecon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
)

const (
	PolicySchemaV1       = "venue-reconciliation-policy-v1"
	policyArtifactDomain = "venue-reconciliation-policy-artifact"
)

type ComparisonKind string

const (
	KindCash     ComparisonKind = "cash"
	KindFill     ComparisonKind = "fill"
	KindPosition ComparisonKind = "position"
	KindSnapshot ComparisonKind = "snapshot"
)

type ResultStatus string

const (
	StatusDrift         ResultStatus = "drift"
	StatusMatched       ResultStatus = "matched"
	StatusNotComparable ResultStatus = "not_comparable"
)

type Severity string

const (
	SeverityNone     Severity = "none"
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type ReasonCode string

const (
	ReasonBustPending              ReasonCode = "bust_pending"
	ReasonCashMatched              ReasonCode = "cash_matched"
	ReasonCashMismatch             ReasonCode = "cash_mismatch"
	ReasonCorrectionPending        ReasonCode = "correction_pending"
	ReasonEquityBasisNotComparable ReasonCode = "equity_basis_not_comparable"
	ReasonFillFeeMismatch          ReasonCode = "fill_fee_mismatch"
	ReasonFillInstrumentMismatch   ReasonCode = "fill_instrument_mismatch"
	ReasonFillLocalMissing         ReasonCode = "fill_local_missing"
	ReasonFillMatched              ReasonCode = "fill_matched"
	ReasonFillOrderMismatch        ReasonCode = "fill_order_mismatch"
	ReasonFillPriceMismatch        ReasonCode = "fill_price_mismatch"
	ReasonFillProviderMissing      ReasonCode = "fill_provider_missing"
	ReasonFillQuantityMismatch     ReasonCode = "fill_quantity_mismatch"
	ReasonFillSideMismatch         ReasonCode = "fill_side_mismatch"
	ReasonLocalFillAfterFrontier   ReasonCode = "local_fill_after_frontier"
	ReasonLocalFillIncomplete      ReasonCode = "local_fill_incomplete"
	ReasonPositionMatched          ReasonCode = "position_matched"
	ReasonPositionLocalMissing     ReasonCode = "position_local_missing"
	ReasonPositionProviderMissing  ReasonCode = "position_provider_missing"
	ReasonPositionQuantityMismatch ReasonCode = "position_quantity_mismatch"
	ReasonProviderUnavailable      ReasonCode = "provider_unavailable"
	ReasonSnapshotIncomplete       ReasonCode = "snapshot_incomplete"
	ReasonSnapshotMappingFailure   ReasonCode = "snapshot_mapping_failure"
	ReasonSnapshotMatched          ReasonCode = "snapshot_matched"
	ReasonSnapshotUnstable         ReasonCode = "snapshot_unstable"
	ReasonUnsupportedFact          ReasonCode = "unsupported_fact"
)

type ProviderRule struct {
	Provider                   venue.Provider
	AuthoritativeFillNamespace string
	SupportsRevisions          bool
}

type ReasonRule struct {
	Code     ReasonCode
	Kind     ComparisonKind
	Status   ResultStatus
	Severity Severity
}

type PolicyInput struct {
	Schema               string
	CaptureCount         int
	ExactDecimals        bool
	CompletePagination   bool
	CompleteFillCoverage bool
	CanonicalContracts   bool
	Providers            []ProviderRule
	Kinds                []ComparisonKind
	Statuses             []ResultStatus
	Reasons              []ReasonRule
}

type Policy struct {
	schema               string
	captureCount         int
	exactDecimals        bool
	completePagination   bool
	completeFillCoverage bool
	canonicalContracts   bool
	providers            []ProviderRule
	kinds                []ComparisonKind
	statuses             []ResultStatus
	reasons              []ReasonRule
	canonicalBytes       json.RawMessage
	digest               string
	version              string
	artifactID           uuid.UUID
}

type PolicyArtifact struct {
	ID             uuid.UUID
	Schema         string
	Version        string
	SHA256         string
	CanonicalBytes json.RawMessage
	CreatedAt      time.Time
}

type canonicalPolicy struct {
	Schema               string                  `json:"schema"`
	CaptureCount         int                     `json:"capture_count"`
	ExactDecimals        bool                    `json:"exact_decimals"`
	CompletePagination   bool                    `json:"complete_pagination"`
	CompleteFillCoverage bool                    `json:"complete_fill_coverage"`
	CanonicalContracts   bool                    `json:"canonical_contracts"`
	Providers            []canonicalProviderRule `json:"providers"`
	Kinds                []string                `json:"kinds"`
	Statuses             []string                `json:"statuses"`
	Reasons              []canonicalReasonRule   `json:"reasons"`
}

type canonicalProviderRule struct {
	Provider                   string `json:"provider"`
	AuthoritativeFillNamespace string `json:"authoritative_fill_namespace"`
	SupportsRevisions          bool   `json:"supports_revisions"`
}

type canonicalReasonRule struct {
	Code     string `json:"code"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Severity string `json:"severity"`
}

func ReviewedPolicyV1Input() PolicyInput {
	return PolicyInput{
		Schema: PolicySchemaV1, CaptureCount: 2, ExactDecimals: true,
		CompletePagination: true, CompleteFillCoverage: true, CanonicalContracts: true,
		Providers: reviewedProviderRules(), Kinds: reviewedKinds(), Statuses: reviewedStatuses(),
		Reasons: reviewedReasonRules(),
	}
}

func reviewedProviderRules() []ProviderRule {
	return []ProviderRule{
		{Provider: venue.ProviderAlpaca, AuthoritativeFillNamespace: "alpaca/account-activities/FILL", SupportsRevisions: true},
		{Provider: venue.ProviderKalshi, AuthoritativeFillNamespace: "kalshi/portfolio/fills", SupportsRevisions: false},
	}
}

func reviewedKinds() []ComparisonKind {
	return []ComparisonKind{KindCash, KindFill, KindPosition, KindSnapshot}
}

func reviewedStatuses() []ResultStatus {
	return []ResultStatus{StatusDrift, StatusMatched, StatusNotComparable}
}

func reviewedReasonRules() []ReasonRule {
	return []ReasonRule{
		{ReasonBustPending, KindFill, StatusNotComparable, SeverityHigh},
		{ReasonCashMatched, KindCash, StatusMatched, SeverityNone},
		{ReasonCashMismatch, KindCash, StatusDrift, SeverityCritical},
		{ReasonCorrectionPending, KindFill, StatusNotComparable, SeverityHigh},
		{ReasonEquityBasisNotComparable, KindCash, StatusNotComparable, SeverityHigh},
		{ReasonFillFeeMismatch, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillInstrumentMismatch, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillLocalMissing, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillMatched, KindFill, StatusMatched, SeverityNone},
		{ReasonFillOrderMismatch, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillPriceMismatch, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillProviderMissing, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillQuantityMismatch, KindFill, StatusDrift, SeverityCritical},
		{ReasonFillSideMismatch, KindFill, StatusDrift, SeverityCritical},
		{ReasonLocalFillAfterFrontier, KindSnapshot, StatusNotComparable, SeverityHigh},
		{ReasonLocalFillIncomplete, KindSnapshot, StatusNotComparable, SeverityHigh},
		{ReasonPositionLocalMissing, KindPosition, StatusDrift, SeverityCritical},
		{ReasonPositionMatched, KindPosition, StatusMatched, SeverityNone},
		{ReasonPositionProviderMissing, KindPosition, StatusDrift, SeverityCritical},
		{ReasonPositionQuantityMismatch, KindPosition, StatusDrift, SeverityCritical},
		{ReasonProviderUnavailable, KindSnapshot, StatusNotComparable, SeverityHigh},
		{ReasonSnapshotIncomplete, KindSnapshot, StatusNotComparable, SeverityHigh},
		{ReasonSnapshotMappingFailure, KindSnapshot, StatusNotComparable, SeverityHigh},
		{ReasonSnapshotMatched, KindSnapshot, StatusMatched, SeverityNone},
		{ReasonSnapshotUnstable, KindSnapshot, StatusNotComparable, SeverityHigh},
		{ReasonUnsupportedFact, KindSnapshot, StatusNotComparable, SeverityHigh},
	}
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	if input.Schema != PolicySchemaV1 || input.CaptureCount != 2 || !input.ExactDecimals ||
		!input.CompletePagination || !input.CompleteFillCoverage || !input.CanonicalContracts {
		return nil, fmt.Errorf("venue reconciliation policy scalar contract does not match reviewed v1")
	}
	providers := append([]ProviderRule(nil), input.Providers...)
	sort.Slice(providers, func(i, j int) bool { return providers[i].Provider < providers[j].Provider })
	if !sameProviderRules(providers, reviewedProviderRules()) {
		return nil, fmt.Errorf("venue reconciliation policy providers do not match reviewed v1")
	}
	kinds := append([]ComparisonKind(nil), input.Kinds...)
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	if !sameKinds(kinds, reviewedKinds()) {
		return nil, fmt.Errorf("venue reconciliation policy kinds do not match reviewed v1")
	}
	statuses := append([]ResultStatus(nil), input.Statuses...)
	sort.Slice(statuses, func(i, j int) bool { return statuses[i] < statuses[j] })
	if !sameStatuses(statuses, reviewedStatuses()) {
		return nil, fmt.Errorf("venue reconciliation policy statuses do not match reviewed v1")
	}
	reasons := append([]ReasonRule(nil), input.Reasons...)
	sort.Slice(reasons, func(i, j int) bool { return reasons[i].Code < reasons[j].Code })
	if !sameReasonRules(reasons, reviewedReasonRules()) {
		return nil, fmt.Errorf("venue reconciliation policy reasons do not match reviewed v1")
	}

	canonical := canonicalPolicy{
		Schema: input.Schema, CaptureCount: input.CaptureCount, ExactDecimals: input.ExactDecimals,
		CompletePagination: input.CompletePagination, CompleteFillCoverage: input.CompleteFillCoverage,
		CanonicalContracts: input.CanonicalContracts,
	}
	for _, rule := range providers {
		canonical.Providers = append(canonical.Providers, canonicalProviderRule{
			Provider: string(rule.Provider), AuthoritativeFillNamespace: rule.AuthoritativeFillNamespace,
			SupportsRevisions: rule.SupportsRevisions,
		})
	}
	for _, kind := range kinds {
		canonical.Kinds = append(canonical.Kinds, string(kind))
	}
	for _, status := range statuses {
		canonical.Statuses = append(canonical.Statuses, string(status))
	}
	for _, rule := range reasons {
		canonical.Reasons = append(canonical.Reasons, canonicalReasonRule{
			Code: string(rule.Code), Kind: string(rule.Kind), Status: string(rule.Status), Severity: string(rule.Severity),
		})
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal venue reconciliation policy: %w", err)
	}
	digestBytes := sha256.Sum256(encoded)
	digest := hex.EncodeToString(digestBytes[:])
	version := input.Schema + "@sha256:" + digest
	return &Policy{
		schema: input.Schema, captureCount: input.CaptureCount, exactDecimals: input.ExactDecimals,
		completePagination: input.CompletePagination, completeFillCoverage: input.CompleteFillCoverage,
		canonicalContracts: input.CanonicalContracts, providers: providers, kinds: kinds, statuses: statuses,
		reasons: reasons, canonicalBytes: encoded, digest: digest, version: version,
		artifactID: economicid.DeterministicUUID(policyArtifactDomain, version),
	}, nil
}

func PolicyFromArtifact(artifact PolicyArtifact) (*Policy, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("restore venue reconciliation policy: %w", err)
	}
	var canonical canonicalPolicy
	decoder := json.NewDecoder(bytes.NewReader(artifact.CanonicalBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, fmt.Errorf("restore venue reconciliation policy: decode: %w", err)
	}
	if err := requirePolicyJSONEOF(decoder); err != nil {
		return nil, fmt.Errorf("restore venue reconciliation policy: %w", err)
	}
	input := PolicyInput{
		Schema: canonical.Schema, CaptureCount: canonical.CaptureCount, ExactDecimals: canonical.ExactDecimals,
		CompletePagination: canonical.CompletePagination, CompleteFillCoverage: canonical.CompleteFillCoverage,
		CanonicalContracts: canonical.CanonicalContracts,
	}
	for _, rule := range canonical.Providers {
		input.Providers = append(input.Providers, ProviderRule{
			Provider: venue.Provider(rule.Provider), AuthoritativeFillNamespace: rule.AuthoritativeFillNamespace,
			SupportsRevisions: rule.SupportsRevisions,
		})
	}
	for _, kind := range canonical.Kinds {
		input.Kinds = append(input.Kinds, ComparisonKind(kind))
	}
	for _, status := range canonical.Statuses {
		input.Statuses = append(input.Statuses, ResultStatus(status))
	}
	for _, rule := range canonical.Reasons {
		input.Reasons = append(input.Reasons, ReasonRule{
			Code: ReasonCode(rule.Code), Kind: ComparisonKind(rule.Kind), Status: ResultStatus(rule.Status), Severity: Severity(rule.Severity),
		})
	}
	restored, err := NewPolicy(input)
	if err != nil {
		return nil, fmt.Errorf("restore venue reconciliation policy: %w", err)
	}
	if restored.artifactID != artifact.ID || restored.schema != artifact.Schema || restored.version != artifact.Version ||
		restored.digest != artifact.SHA256 || !bytes.Equal(restored.canonicalBytes, artifact.CanonicalBytes) {
		return nil, fmt.Errorf("restore venue reconciliation policy: canonical identity differs")
	}
	return restored, nil
}

func (policy *Policy) NewArtifact(createdAt time.Time) (*PolicyArtifact, error) {
	if policy == nil || policy.artifactID == uuid.Nil {
		return nil, fmt.Errorf("venue reconciliation policy is required")
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC().Truncate(time.Microsecond)
	}
	createdAt = createdAt.UTC().Truncate(time.Microsecond)
	artifact := &PolicyArtifact{
		ID: policy.artifactID, Schema: policy.schema, Version: policy.version,
		SHA256: policy.digest, CanonicalBytes: policy.CanonicalBytes(), CreatedAt: createdAt,
	}
	if err := artifact.Validate(); err != nil {
		return nil, err
	}
	return artifact, nil
}

func (artifact PolicyArtifact) Validate() error {
	if artifact.ID == uuid.Nil || artifact.Schema != PolicySchemaV1 ||
		artifact.Version == "" || artifact.Version != strings.TrimSpace(artifact.Version) || len(artifact.Version) > 256 ||
		len(artifact.SHA256) != 64 || artifact.SHA256 != strings.ToLower(artifact.SHA256) ||
		len(artifact.CanonicalBytes) == 0 || artifact.CreatedAt.IsZero() || artifact.CreatedAt.Location() != time.UTC ||
		!artifact.CreatedAt.Equal(artifact.CreatedAt.Truncate(time.Microsecond)) {
		return fmt.Errorf("venue reconciliation policy artifact envelope is invalid")
	}
	digestBytes := sha256.Sum256(artifact.CanonicalBytes)
	digest := hex.EncodeToString(digestBytes[:])
	if artifact.SHA256 != digest || artifact.Version != artifact.Schema+"@sha256:"+digest ||
		artifact.ID != economicid.DeterministicUUID(policyArtifactDomain, artifact.Version) {
		return fmt.Errorf("venue reconciliation policy artifact identity does not match bytes")
	}
	return nil
}

func SamePolicyArtifactPayload(left, right *PolicyArtifact) bool {
	return left != nil && right != nil && left.ID == right.ID && left.Schema == right.Schema &&
		left.Version == right.Version && left.SHA256 == right.SHA256 && bytes.Equal(left.CanonicalBytes, right.CanonicalBytes)
}

func (policy *Policy) Schema() string {
	if policy == nil {
		return ""
	}
	return policy.schema
}

func (policy *Policy) CaptureCount() int {
	if policy == nil {
		return 0
	}
	return policy.captureCount
}

func (policy *Policy) ExactDecimals() bool {
	return policy != nil && policy.exactDecimals
}

func (policy *Policy) CompletePagination() bool {
	return policy != nil && policy.completePagination
}

func (policy *Policy) CompleteFillCoverage() bool {
	return policy != nil && policy.completeFillCoverage
}

func (policy *Policy) CanonicalContracts() bool {
	return policy != nil && policy.canonicalContracts
}

func (policy *Policy) Version() string {
	if policy == nil {
		return ""
	}
	return policy.version
}

func (policy *Policy) Digest() string {
	if policy == nil {
		return ""
	}
	return policy.digest
}

func (policy *Policy) ArtifactID() uuid.UUID {
	if policy == nil {
		return uuid.Nil
	}
	return policy.artifactID
}

func (policy *Policy) CanonicalBytes() json.RawMessage {
	if policy == nil {
		return nil
	}
	return append(json.RawMessage(nil), policy.canonicalBytes...)
}

func (policy *Policy) Providers() []ProviderRule {
	if policy == nil {
		return nil
	}
	return append([]ProviderRule(nil), policy.providers...)
}

func (policy *Policy) Kinds() []ComparisonKind {
	if policy == nil {
		return nil
	}
	return append([]ComparisonKind(nil), policy.kinds...)
}

func (policy *Policy) Statuses() []ResultStatus {
	if policy == nil {
		return nil
	}
	return append([]ResultStatus(nil), policy.statuses...)
}

func (policy *Policy) Reasons() []ReasonRule {
	if policy == nil {
		return nil
	}
	return append([]ReasonRule(nil), policy.reasons...)
}

func (policy *Policy) ProviderRule(provider venue.Provider) (ProviderRule, bool) {
	if policy == nil {
		return ProviderRule{}, false
	}
	index := sort.Search(len(policy.providers), func(i int) bool { return policy.providers[i].Provider >= provider })
	if index >= len(policy.providers) || policy.providers[index].Provider != provider {
		return ProviderRule{}, false
	}
	return policy.providers[index], true
}

func (policy *Policy) Reason(code ReasonCode) (ReasonRule, bool) {
	if policy == nil {
		return ReasonRule{}, false
	}
	index := sort.Search(len(policy.reasons), func(i int) bool { return policy.reasons[i].Code >= code })
	if index >= len(policy.reasons) || policy.reasons[index].Code != code {
		return ReasonRule{}, false
	}
	return policy.reasons[index], true
}

func sameProviderRules(left, right []ProviderRule) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] || left[i].AuthoritativeFillNamespace == "" ||
			left[i].AuthoritativeFillNamespace != strings.TrimSpace(left[i].AuthoritativeFillNamespace) {
			return false
		}
	}
	return true
}

func sameKinds(left, right []ComparisonKind) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameStatuses(left, right []ResultStatus) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func sameReasonRules(left, right []ReasonRule) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func requirePolicyJSONEOF(decoder *json.Decoder) error {
	if _, err := decoder.Token(); err != io.EOF {
		if err == nil {
			return fmt.Errorf("policy contains trailing JSON")
		}
		return fmt.Errorf("read trailing policy JSON: %w", err)
	}
	return nil
}
