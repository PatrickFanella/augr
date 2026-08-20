// Package researchworkflow owns immutable, evidence-bound hypothesis and critic
// artifacts. It cannot invoke models/search, create experiments or deployments,
// mutate lifecycle state, schedule work, or emit intents/orders.
package researchworkflow

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/dataset"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/generativestrategy"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	HypothesisSchemaV1 = "evidence-bound-hypothesis-v1"
	timeLayout         = "2006-01-02T15:04:05.000000Z"
)

var (
	tokenPattern  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,95}$`)
	digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

type Parents struct {
	Manifest         *dataset.Manifest
	RobustnessPolicy *robustness.Policy
	RobustnessFamily *robustness.Family
	Assessment       *robustness.Assessment
	Spec             *generativestrategy.Spec
	Version          *strategycatalog.Version
	Receipt          *generativestrategy.Receipt
}

type SourceInput struct {
	Key                string
	URI                string
	Publisher          string
	Title              string
	PublishedAt        time.Time
	AvailableAt        time.Time
	ContentSHA256      string
	License            string
	ManifestSourceKeys []string
}

type SearchResultInput struct {
	SourceKey string
	Rank      int
	Selected  bool
}

type SearchInput struct {
	Key         string
	Provider    string
	QuerySHA256 string
	ExecutedAt  time.Time
	Results     []SearchResultInput
}

type ProvenanceInput struct {
	Provider              string
	Model                 string
	SystemPromptSHA256    string
	DeveloperPromptSHA256 string
	UserPromptSHA256      string
	InputTokens           int64
	OutputTokens          int64
	Currency              string
	Cost                  string
}

type TestInput struct {
	Key             string
	Type            string
	ExpectedOutcome string
	AcceptanceRule  string
	SpecTestKey     string
}

type HypothesisInput struct {
	Parents              Parents
	WorkflowKey          string
	Claim                string
	Mechanism            string
	PredictedObservation string
	NullHypothesis       string
	RefutationThreshold  string
	EvaluationHorizon    string
	AbstentionCondition  string
	Sources              []SourceInput
	Searches             []SearchInput
	Provenance           ProvenanceInput
	Tests                []TestInput
}

type parentCanonical struct {
	ManifestID             string `json:"manifest_id"`
	ManifestSHA256         string `json:"manifest_sha256"`
	RobustnessPolicyID     string `json:"robustness_policy_id"`
	RobustnessPolicySHA256 string `json:"robustness_policy_sha256"`
	RobustnessFamilyID     string `json:"robustness_family_id"`
	RobustnessFamilySHA256 string `json:"robustness_family_sha256"`
	AssessmentID           string `json:"assessment_id"`
	AssessmentSHA256       string `json:"assessment_sha256"`
	SpecID                 string `json:"spec_id"`
	SpecSHA256             string `json:"spec_sha256"`
	VersionID              string `json:"version_id"`
	VersionSHA256          string `json:"version_sha256"`
	ReceiptID              string `json:"receipt_id"`
	ReceiptSHA256          string `json:"receipt_sha256"`
}

type sourceCanonical struct {
	Sequence           int      `json:"sequence"`
	Key                string   `json:"key"`
	URI                string   `json:"uri"`
	Publisher          string   `json:"publisher"`
	Title              string   `json:"title"`
	PublishedAt        string   `json:"published_at"`
	AvailableAt        string   `json:"available_at"`
	ContentSHA256      string   `json:"content_sha256"`
	License            string   `json:"license"`
	ManifestSourceKeys []string `json:"manifest_source_keys"`
}

type searchResultCanonical struct {
	Sequence  int    `json:"sequence"`
	SourceKey string `json:"source_key"`
	Rank      int    `json:"rank"`
	Selected  bool   `json:"selected"`
}

type searchCanonical struct {
	Sequence    int                     `json:"sequence"`
	Key         string                  `json:"key"`
	Provider    string                  `json:"provider"`
	QuerySHA256 string                  `json:"query_sha256"`
	ExecutedAt  string                  `json:"executed_at"`
	Results     []searchResultCanonical `json:"results"`
}

type provenanceCanonical struct {
	Provider              string `json:"provider"`
	Model                 string `json:"model"`
	SystemPromptSHA256    string `json:"system_prompt_sha256"`
	DeveloperPromptSHA256 string `json:"developer_prompt_sha256"`
	UserPromptSHA256      string `json:"user_prompt_sha256"`
	InputTokens           int64  `json:"input_tokens"`
	OutputTokens          int64  `json:"output_tokens"`
	Currency              string `json:"currency"`
	Cost                  string `json:"cost"`
}

type testCanonical struct {
	Sequence        int    `json:"sequence"`
	Key             string `json:"key"`
	Type            string `json:"type"`
	ExpectedOutcome string `json:"expected_outcome"`
	AcceptanceRule  string `json:"acceptance_rule"`
	SpecTestKey     string `json:"spec_test_key"`
}

type hypothesisCanonical struct {
	Schema               string              `json:"schema"`
	State                string              `json:"state"`
	WorkflowKey          string              `json:"workflow_key"`
	Parents              parentCanonical     `json:"parents"`
	Claim                string              `json:"claim"`
	Mechanism            string              `json:"mechanism"`
	PredictedObservation string              `json:"predicted_observation"`
	NullHypothesis       string              `json:"null_hypothesis"`
	RefutationThreshold  string              `json:"refutation_threshold"`
	EvaluationHorizon    string              `json:"evaluation_horizon"`
	AbstentionCondition  string              `json:"abstention_condition"`
	Sources              []sourceCanonical   `json:"sources"`
	Searches             []searchCanonical   `json:"searches"`
	Provenance           provenanceCanonical `json:"provenance"`
	Tests                []testCanonical     `json:"tests"`
}

type Hypothesis struct {
	canonical hypothesisCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

type manifestObservation struct {
	contentSHA256 string
	availableAt   string
}

func NewHypothesis(input HypothesisInput) (*Hypothesis, error) {
	parents, err := normalizeParents(input.Parents)
	if err != nil {
		return nil, err
	}
	if !tokenPattern.MatchString(input.WorkflowKey) || !canonicalText(input.Claim, 4096) || !canonicalText(input.Mechanism, 4096) || !canonicalText(input.PredictedObservation, 4096) || !canonicalText(input.NullHypothesis, 4096) || !canonicalText(input.RefutationThreshold, 1024) || !canonicalText(input.EvaluationHorizon, 1024) || !canonicalText(input.AbstentionCondition, 2048) {
		return nil, fmt.Errorf("research hypothesis claim contract is invalid")
	}
	sources, err := normalizeSources(input.Sources, input.Parents.Manifest)
	if err != nil {
		return nil, err
	}
	searches, err := normalizeSearches(input.Searches, sources, input.Parents.Manifest.DecisionCutoff())
	if err != nil {
		return nil, err
	}
	provenance, err := normalizeProvenance(input.Provenance)
	if err != nil {
		return nil, err
	}
	tests, err := normalizeTests(input.Tests, input.Parents.Spec)
	if err != nil {
		return nil, err
	}
	canonical := hypothesisCanonical{HypothesisSchemaV1, "authored", input.WorkflowKey, parents, input.Claim, input.Mechanism, input.PredictedObservation, input.NullHypothesis, input.RefutationThreshold, input.EvaluationHorizon, input.AbstentionCondition, sources, searches, provenance, tests}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal research hypothesis: %w", err)
	}
	digest := hash(encoded)
	return &Hypothesis{canonical, encoded, digest, economicid.DeterministicUUID("evidence-bound-hypothesis", HypothesisSchemaV1+"@sha256:"+digest)}, nil
}

func normalizeParents(value Parents) (parentCanonical, error) {
	if value.Manifest == nil || value.RobustnessPolicy == nil || value.RobustnessFamily == nil || value.Assessment == nil || value.Spec == nil || value.Version == nil || value.Receipt == nil {
		return parentCanonical{}, fmt.Errorf("research hypothesis parents are required")
	}
	if value.Assessment.PolicyID() != value.RobustnessPolicy.ID() || value.Assessment.PolicyDigest() != value.RobustnessPolicy.Digest() || value.Assessment.FamilyID() != value.RobustnessFamily.ID() || value.Assessment.FamilyDigest() != value.RobustnessFamily.Digest() || value.Spec.FamilyID() != value.Version.FamilyID() || value.Spec.FamilyDigest() == "" || value.Receipt.SpecID() != value.Spec.ID() || value.Receipt.SpecDigest() != value.Spec.Digest() || value.Receipt.VersionID() != value.Version.ID() || value.Receipt.VersionDigest() != value.Version.Digest() {
		return parentCanonical{}, fmt.Errorf("research hypothesis parent binding is invalid")
	}
	passed := false
	for _, candidate := range value.Assessment.Candidates() {
		if candidate.VersionID != value.Version.ID().String() {
			continue
		}
		for _, gate := range candidate.Gates {
			if gate.Name == "overall_robustness" && gate.State == robustness.GatePass {
				passed = true
			}
		}
	}
	if !passed {
		return parentCanonical{}, fmt.Errorf("research hypothesis version lacks complete robustness pass")
	}
	return parentCanonical{value.Manifest.ID().String(), value.Manifest.Digest(), value.RobustnessPolicy.ID().String(), value.RobustnessPolicy.Digest(), value.RobustnessFamily.ID().String(), value.RobustnessFamily.Digest(), value.Assessment.ID().String(), value.Assessment.Digest(), value.Spec.ID().String(), value.Spec.Digest(), value.Version.ID().String(), value.Version.Digest(), value.Receipt.ID().String(), value.Receipt.Digest()}, nil
}

func normalizeSources(values []SourceInput, manifest *dataset.Manifest) ([]sourceCanonical, error) {
	if len(values) == 0 || len(values) > 128 {
		return nil, fmt.Errorf("research hypothesis sources are invalid")
	}
	observations := map[string]manifestObservation{}
	for _, partition := range manifest.Partitions() {
		for _, observation := range partition.Observations {
			if _, duplicate := observations[observation.SourceKey]; duplicate {
				return nil, fmt.Errorf("manifest source key is ambiguous")
			}
			observations[observation.SourceKey] = manifestObservation{observation.ContentSHA256, observation.AvailableAt}
		}
	}
	result := make([]sourceCanonical, 0, len(values))
	for _, value := range values {
		parsedURI, uriErr := url.ParseRequestURI(value.URI)
		keys := append([]string(nil), value.ManifestSourceKeys...)
		sort.Strings(keys)
		if !tokenPattern.MatchString(value.Key) || uriErr != nil || parsedURI.Scheme != "https" || parsedURI.Host == "" || !canonicalText(value.Publisher, 256) || !canonicalText(value.Title, 1024) || !canonicalTime(value.PublishedAt) || !canonicalTime(value.AvailableAt) || value.AvailableAt.Before(value.PublishedAt) || value.AvailableAt.After(manifest.DecisionCutoff()) || !digestPattern.MatchString(value.ContentSHA256) || !canonicalText(value.License, 256) || len(keys) == 0 {
			return nil, fmt.Errorf("research hypothesis source is invalid")
		}
		for i, key := range keys {
			observation, ok := observations[key]
			if !ok || i > 0 && keys[i-1] == key || observation.contentSHA256 != value.ContentSHA256 || observation.availableAt != formatTime(value.AvailableAt) {
				return nil, fmt.Errorf("research hypothesis source does not match manifest")
			}
		}
		result = append(result, sourceCanonical{0, value.Key, parsedURI.String(), value.Publisher, value.Title, formatTime(value.PublishedAt), formatTime(value.AvailableAt), value.ContentSHA256, value.License, keys})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for i := range result {
		if i > 0 && result[i-1].Key == result[i].Key {
			return nil, fmt.Errorf("research hypothesis source is duplicated")
		}
		result[i].Sequence = i
	}
	return result, nil
}

func normalizeSearches(values []SearchInput, sources []sourceCanonical, cutoff time.Time) ([]searchCanonical, error) {
	if len(values) == 0 || len(values) > 64 {
		return nil, fmt.Errorf("research hypothesis searches are invalid")
	}
	sourceKeys := make(map[string]bool, len(sources))
	for _, source := range sources {
		sourceKeys[source.Key] = false
	}
	result := make([]searchCanonical, 0, len(values))
	for _, value := range values {
		if !tokenPattern.MatchString(value.Key) || !tokenPattern.MatchString(value.Provider) || !digestPattern.MatchString(value.QuerySHA256) || !canonicalTime(value.ExecutedAt) || value.ExecutedAt.After(cutoff) || len(value.Results) == 0 || len(value.Results) > 256 {
			return nil, fmt.Errorf("research hypothesis search is invalid")
		}
		rows := make([]searchResultCanonical, 0, len(value.Results))
		seenRanks := map[int]struct{}{}
		seenSources := map[string]struct{}{}
		for _, row := range value.Results {
			if row.Rank <= 0 || !tokenPattern.MatchString(row.SourceKey) {
				return nil, fmt.Errorf("research hypothesis search result is invalid")
			}
			if _, duplicate := seenRanks[row.Rank]; duplicate {
				return nil, fmt.Errorf("research hypothesis search rank is duplicated")
			}
			if _, duplicate := seenSources[row.SourceKey]; duplicate {
				return nil, fmt.Errorf("research hypothesis search source is duplicated")
			}
			seenRanks[row.Rank] = struct{}{}
			seenSources[row.SourceKey] = struct{}{}
			if row.Selected {
				if _, ok := sourceKeys[row.SourceKey]; !ok {
					return nil, fmt.Errorf("selected search result lacks retained source")
				}
				sourceKeys[row.SourceKey] = true
			}
			rows = append(rows, searchResultCanonical{SourceKey: row.SourceKey, Rank: row.Rank, Selected: row.Selected})
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Rank == rows[j].Rank {
				return rows[i].SourceKey < rows[j].SourceKey
			}
			return rows[i].Rank < rows[j].Rank
		})
		for i := range rows {
			rows[i].Sequence = i
		}
		result = append(result, searchCanonical{Key: value.Key, Provider: value.Provider, QuerySHA256: value.QuerySHA256, ExecutedAt: formatTime(value.ExecutedAt), Results: rows})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for i := range result {
		if i > 0 && result[i-1].Key == result[i].Key {
			return nil, fmt.Errorf("research hypothesis search is duplicated")
		}
		result[i].Sequence = i
	}
	for _, selected := range sourceKeys {
		if !selected {
			return nil, fmt.Errorf("research hypothesis source is absent from selected search lineage")
		}
	}
	return result, nil
}

func normalizeProvenance(value ProvenanceInput) (provenanceCanonical, error) {
	cost, err := exactDecimal(value.Cost)
	currency := strings.ToUpper(value.Currency)
	if !tokenPattern.MatchString(value.Provider) || !canonicalText(value.Model, 256) || !digestPattern.MatchString(value.SystemPromptSHA256) || !digestPattern.MatchString(value.DeveloperPromptSHA256) || !digestPattern.MatchString(value.UserPromptSHA256) || value.InputTokens < 0 || value.OutputTokens < 0 || len(currency) != 3 || currency != value.Currency || err != nil || cost.IsNegative() {
		return provenanceCanonical{}, fmt.Errorf("research workflow provenance is invalid")
	}
	return provenanceCanonical{value.Provider, value.Model, value.SystemPromptSHA256, value.DeveloperPromptSHA256, value.UserPromptSHA256, value.InputTokens, value.OutputTokens, currency, cost.String()}, nil
}

func normalizeTests(values []TestInput, spec *generativestrategy.Spec) ([]testCanonical, error) {
	if len(values) == 0 || len(values) > 128 {
		return nil, fmt.Errorf("research hypothesis tests are invalid")
	}
	var specTests struct {
		PropertyTests []string `json:"property_tests"`
		ExampleTests  []struct {
			Key string `json:"key"`
		} `json:"example_tests"`
	}
	if err := json.Unmarshal(spec.CanonicalBytes(), &specTests); err != nil {
		return nil, fmt.Errorf("decode generated strategy tests: %w", err)
	}
	required := map[string]bool{}
	for _, key := range specTests.PropertyTests {
		required["spec_property:"+key] = false
	}
	for _, value := range specTests.ExampleTests {
		required["spec_example:"+value.Key] = false
	}
	requiredKinds := map[string]bool{"leakage": false, "cost": false, "baseline": false, "refutation": false}
	allowed := map[string]bool{"spec_property": true, "spec_example": true, "leakage": true, "cost": true, "baseline": true, "refutation": true}
	result := make([]testCanonical, 0, len(values))
	for _, value := range values {
		if !tokenPattern.MatchString(value.Key) || !allowed[value.Type] || !canonicalText(value.ExpectedOutcome, 2048) || !canonicalText(value.AcceptanceRule, 2048) {
			return nil, fmt.Errorf("research hypothesis declared test is invalid")
		}
		if strings.HasPrefix(value.Type, "spec_") {
			binding := value.Type + ":" + value.SpecTestKey
			covered, ok := required[binding]
			if !ok || covered || !tokenPattern.MatchString(value.SpecTestKey) {
				return nil, fmt.Errorf("research hypothesis generated test binding is invalid")
			}
			required[binding] = true
		} else {
			if value.SpecTestKey != "" {
				return nil, fmt.Errorf("research hypothesis non-spec test has spec binding")
			}
			requiredKinds[value.Type] = true
		}
		result = append(result, testCanonical{Key: value.Key, Type: value.Type, ExpectedOutcome: value.ExpectedOutcome, AcceptanceRule: value.AcceptanceRule, SpecTestKey: value.SpecTestKey})
	}
	for _, covered := range required {
		if !covered {
			return nil, fmt.Errorf("research hypothesis generated test coverage is incomplete")
		}
	}
	for _, covered := range requiredKinds {
		if !covered {
			return nil, fmt.Errorf("research hypothesis experiment test coverage is incomplete")
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for i := range result {
		if i > 0 && result[i-1].Key == result[i].Key {
			return nil, fmt.Errorf("research hypothesis declared test is duplicated")
		}
		result[i].Sequence = i
	}
	return result, nil
}

func HypothesisFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, parents Parents) (*Hypothesis, error) {
	var canonical hypothesisCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != HypothesisSchemaV1 || canonical.State != "authored" {
		return nil, fmt.Errorf("stored research hypothesis is invalid")
	}
	input := HypothesisInput{Parents: parents, WorkflowKey: canonical.WorkflowKey, Claim: canonical.Claim, Mechanism: canonical.Mechanism, PredictedObservation: canonical.PredictedObservation, NullHypothesis: canonical.NullHypothesis, RefutationThreshold: canonical.RefutationThreshold, EvaluationHorizon: canonical.EvaluationHorizon, AbstentionCondition: canonical.AbstentionCondition, Provenance: ProvenanceInput{canonical.Provenance.Provider, canonical.Provenance.Model, canonical.Provenance.SystemPromptSHA256, canonical.Provenance.DeveloperPromptSHA256, canonical.Provenance.UserPromptSHA256, canonical.Provenance.InputTokens, canonical.Provenance.OutputTokens, canonical.Provenance.Currency, canonical.Provenance.Cost}}
	for _, source := range canonical.Sources {
		input.Sources = append(input.Sources, SourceInput{source.Key, source.URI, source.Publisher, source.Title, mustTime(source.PublishedAt), mustTime(source.AvailableAt), source.ContentSHA256, source.License, append([]string(nil), source.ManifestSourceKeys...)})
	}
	for _, search := range canonical.Searches {
		row := SearchInput{Key: search.Key, Provider: search.Provider, QuerySHA256: search.QuerySHA256, ExecutedAt: mustTime(search.ExecutedAt)}
		for _, result := range search.Results {
			row.Results = append(row.Results, SearchResultInput{result.SourceKey, result.Rank, result.Selected})
		}
		input.Searches = append(input.Searches, row)
	}
	for _, test := range canonical.Tests {
		input.Tests = append(input.Tests, TestInput{test.Key, test.Type, test.ExpectedOutcome, test.AcceptanceRule, test.SpecTestKey})
	}
	rebuilt, err := NewHypothesis(input)
	if err != nil || rebuilt.id != id || rebuilt.digest != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("stored research hypothesis does not reconstruct")
	}
	return rebuilt, nil
}

func (h *Hypothesis) ID() uuid.UUID       { return h.id }
func (h *Hypothesis) Digest() string      { return h.digest }
func (h *Hypothesis) WorkflowKey() string { return h.canonical.WorkflowKey }
func (h *Hypothesis) CanonicalBytes() json.RawMessage {
	return append(json.RawMessage(nil), h.bytes...)
}

func (h *Hypothesis) ManifestID() uuid.UUID { return uuid.MustParse(h.canonical.Parents.ManifestID) }

func (h *Hypothesis) ManifestDigest() string {
	if h == nil {
		return ""
	}
	return h.canonical.Parents.ManifestSHA256
}

func (h *Hypothesis) ProvenanceCost() string {
	if h == nil {
		return ""
	}
	return h.canonical.Provenance.Cost
}

func (h *Hypothesis) ProvenanceCurrency() string {
	if h == nil {
		return ""
	}
	return h.canonical.Provenance.Currency
}

func (h *Hypothesis) AssessmentID() uuid.UUID {
	return uuid.MustParse(h.canonical.Parents.AssessmentID)
}
func (h *Hypothesis) SpecID() uuid.UUID    { return uuid.MustParse(h.canonical.Parents.SpecID) }
func (h *Hypothesis) VersionID() uuid.UUID { return uuid.MustParse(h.canonical.Parents.VersionID) }
func (h *Hypothesis) VersionDigest() string {
	if h == nil {
		return ""
	}
	return h.canonical.Parents.VersionSHA256
}

func (h *Hypothesis) AssessmentDigest() string {
	if h == nil {
		return ""
	}
	return h.canonical.Parents.AssessmentSHA256
}

func canonicalText(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum && !strings.ContainsRune(value, '\x00')
}

func canonicalTime(value time.Time) bool {
	return !value.IsZero() && value.Location() == time.UTC && value.Nanosecond()%1000 == 0
}

func formatTime(value time.Time) string { return value.Format(timeLayout) }

func mustTime(value string) time.Time {
	parsed, _ := time.Parse(timeLayout, value)
	return parsed
}

func exactDecimal(value string) (decimal.Decimal, error) {
	parsed, err := decimal.NewFromString(value)
	if err != nil || parsed.String() != value {
		return decimal.Zero, fmt.Errorf("decimal is not canonical")
	}
	return parsed, nil
}

func hash(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}
