// Package dataset owns immutable point-in-time research dataset evidence and
// deterministic quality results. It does not fetch data or start experiments.
package dataset

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
)

const (
	PolicySchemaV1       = "dataset-quality-policy-v1"
	policyArtifactDomain = "dataset-quality-policy-artifact"
)

type Kind string

const (
	KindBars                Kind = "bars"
	KindBenchmarkMembership Kind = "benchmark_membership"
	KindCorporateActions    Kind = "corporate_actions"
	KindDepth               Kind = "depth"
	KindExternalObject      Kind = "external_object"
	KindFilings             Kind = "filings"
	KindFundamentals        Kind = "fundamentals"
	KindOptionChains        Kind = "option_chains"
	KindOptionContracts     Kind = "option_contracts"
	KindPredictionBooks     Kind = "prediction_books"
	KindPredictionFees      Kind = "prediction_fees"
	KindPredictionRules     Kind = "prediction_rules"
	KindPredictionTrades    Kind = "prediction_trades"
	KindResolutions         Kind = "resolutions"
	KindQuotes              Kind = "quotes"
)

type CheckCode string

const (
	CheckBidAsk               CheckCode = "bid_ask"
	CheckContentIntegrity     CheckCode = "content_integrity"
	CheckCorporateActions     CheckCode = "corporate_action_reconciliation"
	CheckCorrectionLineage    CheckCode = "correction_lineage"
	CheckInstrumentValidity   CheckCode = "instrument_validity"
	CheckMonotonicTime        CheckCode = "monotonic_time"
	CheckNonnegativeDepth     CheckCode = "nonnegative_depth"
	CheckNonnegativeVolume    CheckCode = "nonnegative_volume"
	CheckNoLookahead          CheckCode = "no_lookahead"
	CheckProviderSpotCompare  CheckCode = "provider_spot_comparison"
	CheckSessionCoverage      CheckCode = "session_coverage"
	CheckUniqueSourceIdentity CheckCode = "unique_source_identity"
)

type Severity string

const (
	SeverityHigh     Severity = "high"
	SeverityCritical Severity = "critical"
)

type CheckRule struct {
	Code     CheckCode
	Kinds    []Kind
	Required bool
	Severity Severity
}

type PolicyInput struct {
	Schema string
	Kinds  []Kind
	Rules  []CheckRule
}

type Policy struct {
	schema         string
	kinds          []Kind
	rules          []CheckRule
	canonicalBytes json.RawMessage
	digest         string
	version        string
	artifactID     uuid.UUID
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
	Schema string          `json:"schema"`
	Kinds  []Kind          `json:"kinds"`
	Rules  []canonicalRule `json:"rules"`
}

type canonicalRule struct {
	Code     CheckCode `json:"code"`
	Kinds    []Kind    `json:"kinds"`
	Required bool      `json:"required"`
	Severity Severity  `json:"severity"`
}

func ReviewedPolicyV1Input() PolicyInput {
	return PolicyInput{Schema: PolicySchemaV1, Kinds: reviewedKinds(), Rules: reviewedRules()}
}

func reviewedKinds() []Kind {
	return []Kind{
		KindBars, KindBenchmarkMembership, KindCorporateActions, KindDepth,
		KindExternalObject, KindFilings, KindFundamentals, KindOptionChains,
		KindOptionContracts, KindPredictionBooks, KindPredictionFees,
		KindPredictionRules, KindPredictionTrades, KindQuotes, KindResolutions,
	}
}

func reviewedRules() []CheckRule {
	all := reviewedKinds()
	market := []Kind{KindBars, KindDepth, KindOptionChains, KindPredictionBooks, KindPredictionTrades, KindQuotes}
	identified := []Kind{KindBars, KindBenchmarkMembership, KindCorporateActions, KindDepth, KindFundamentals, KindOptionChains, KindOptionContracts, KindQuotes}
	correctable := []Kind{KindBenchmarkMembership, KindCorporateActions, KindFilings, KindFundamentals, KindOptionChains, KindOptionContracts, KindPredictionFees, KindPredictionRules, KindResolutions}
	return []CheckRule{
		{CheckBidAsk, []Kind{KindDepth, KindPredictionBooks, KindQuotes}, true, SeverityCritical},
		{CheckContentIntegrity, all, true, SeverityCritical},
		{CheckCorporateActions, []Kind{KindBars}, true, SeverityHigh},
		{CheckCorrectionLineage, correctable, true, SeverityHigh},
		{CheckInstrumentValidity, identified, true, SeverityCritical},
		{CheckMonotonicTime, all, true, SeverityHigh},
		{CheckNonnegativeDepth, []Kind{KindDepth, KindPredictionBooks}, true, SeverityCritical},
		{CheckNonnegativeVolume, []Kind{KindBars, KindPredictionTrades}, true, SeverityCritical},
		{CheckNoLookahead, all, true, SeverityCritical},
		{CheckProviderSpotCompare, market, false, SeverityHigh},
		{CheckSessionCoverage, []Kind{KindBars, KindDepth, KindOptionChains, KindQuotes}, true, SeverityHigh},
		{CheckUniqueSourceIdentity, all, true, SeverityCritical},
	}
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	if input.Schema != PolicySchemaV1 {
		return nil, fmt.Errorf("dataset quality policy schema must be %q", PolicySchemaV1)
	}
	kinds := append([]Kind(nil), input.Kinds...)
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	if !sameKinds(kinds, reviewedKinds()) {
		return nil, fmt.Errorf("dataset quality policy kinds do not match reviewed v1")
	}
	rules := cloneRules(input.Rules)
	for index := range rules {
		sort.Slice(rules[index].Kinds, func(i, j int) bool { return rules[index].Kinds[i] < rules[index].Kinds[j] })
	}
	sort.Slice(rules, func(i, j int) bool { return rules[i].Code < rules[j].Code })
	want := reviewedRules()
	for index := range want {
		sort.Slice(want[index].Kinds, func(i, j int) bool { return want[index].Kinds[i] < want[index].Kinds[j] })
	}
	sort.Slice(want, func(i, j int) bool { return want[i].Code < want[j].Code })
	if !sameRules(rules, want) {
		return nil, fmt.Errorf("dataset quality policy rules do not match reviewed v1")
	}
	canonical := canonicalPolicy{Schema: input.Schema, Kinds: kinds}
	for _, rule := range rules {
		canonical.Rules = append(canonical.Rules, canonicalRule(rule))
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal dataset quality policy: %w", err)
	}
	digest := hashBytes(encoded)
	version := input.Schema + "@sha256:" + digest
	return &Policy{
		schema: input.Schema, kinds: kinds, rules: rules, canonicalBytes: encoded,
		digest: digest, version: version,
		artifactID: economicid.DeterministicUUID(policyArtifactDomain, version),
	}, nil
}

func PolicyFromArtifact(artifact PolicyArtifact) (*Policy, error) {
	if err := artifact.Validate(); err != nil {
		return nil, fmt.Errorf("restore dataset quality policy: %w", err)
	}
	var canonical canonicalPolicy
	decoder := json.NewDecoder(bytes.NewReader(artifact.CanonicalBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, fmt.Errorf("restore dataset quality policy: %w", err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	input := PolicyInput{Schema: canonical.Schema, Kinds: canonical.Kinds}
	for _, rule := range canonical.Rules {
		input.Rules = append(input.Rules, CheckRule(rule))
	}
	policy, err := NewPolicy(input)
	if err != nil {
		return nil, err
	}
	if policy.artifactID != artifact.ID || policy.version != artifact.Version ||
		policy.digest != artifact.SHA256 || !bytes.Equal(policy.canonicalBytes, artifact.CanonicalBytes) {
		return nil, fmt.Errorf("dataset quality policy artifact differs from reviewed evidence")
	}
	return policy, nil
}

func (policy *Policy) NewArtifact(createdAt time.Time) (*PolicyArtifact, error) {
	if policy == nil || !canonicalTimeValue(createdAt) {
		return nil, fmt.Errorf("dataset quality policy and UTC microsecond creation time are required")
	}
	artifact := &PolicyArtifact{
		ID: policy.artifactID, Schema: policy.schema, Version: policy.version,
		SHA256: policy.digest, CanonicalBytes: append(json.RawMessage(nil), policy.canonicalBytes...), CreatedAt: createdAt,
	}
	return artifact, artifact.Validate()
}

func (artifact PolicyArtifact) Validate() error {
	if artifact.ID == uuid.Nil || artifact.Schema != PolicySchemaV1 ||
		!strings.HasPrefix(artifact.Version, PolicySchemaV1+"@sha256:") ||
		len(artifact.SHA256) != 64 || hashBytes(artifact.CanonicalBytes) != artifact.SHA256 ||
		artifact.Version != artifact.Schema+"@sha256:"+artifact.SHA256 ||
		artifact.ID != economicid.DeterministicUUID(policyArtifactDomain, artifact.Version) ||
		!canonicalTimeValue(artifact.CreatedAt) {
		return fmt.Errorf("dataset quality policy artifact is invalid")
	}
	return nil
}

func (policy *Policy) Schema() string {
	if policy == nil {
		return ""
	}
	return policy.schema
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
func (policy *Policy) ID() uuid.UUID {
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
func (policy *Policy) Kinds() []Kind {
	if policy == nil {
		return nil
	}
	return append([]Kind(nil), policy.kinds...)
}
func (policy *Policy) Rules() []CheckRule {
	if policy == nil {
		return nil
	}
	return cloneRules(policy.rules)
}

func cloneRules(values []CheckRule) []CheckRule {
	result := make([]CheckRule, len(values))
	for index, value := range values {
		result[index] = value
		result[index].Kinds = append([]Kind(nil), value.Kinds...)
	}
	return result
}

func sameKinds(left, right []Kind) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func sameRules(left, right []CheckRule) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index].Code != right[index].Code || left[index].Required != right[index].Required ||
			left[index].Severity != right[index].Severity || !sameKinds(left[index].Kinds, right[index].Kinds) {
			return false
		}
	}
	return true
}

func hashBytes(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func canonicalTimeValue(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func requireJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("dataset canonical JSON has trailing content")
	}
	return nil
}
