package researchworkflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const CriticSchemaV1 = "independent-research-critic-v1"

var requiredCriticChecks = []string{"cost_capacity", "leakage", "multiple_testing", "reproducibility", "source_coverage", "test_completeness"}

type FindingInput struct {
	Key         string
	Category    string
	Severity    string
	Status      string
	References  []string
	Explanation string
}

type CheckInput struct {
	Name        string
	State       string
	References  []string
	Explanation string
}

type CriticInput struct {
	Hypothesis *Hypothesis
	ReviewKey  string
	Provenance ProvenanceInput
	Findings   []FindingInput
	Checks     []CheckInput
}

type findingCanonical struct {
	Sequence    int      `json:"sequence"`
	Key         string   `json:"key"`
	Category    string   `json:"category"`
	Severity    string   `json:"severity"`
	Status      string   `json:"status"`
	References  []string `json:"references"`
	Explanation string   `json:"explanation"`
}

type checkCanonical struct {
	Sequence    int      `json:"sequence"`
	Name        string   `json:"name"`
	State       string   `json:"state"`
	References  []string `json:"references"`
	Explanation string   `json:"explanation"`
}

type criticCanonical struct {
	Schema           string              `json:"schema"`
	State            string              `json:"state"`
	ReviewKey        string              `json:"review_key"`
	HypothesisID     string              `json:"hypothesis_id"`
	HypothesisSHA256 string              `json:"hypothesis_sha256"`
	Provenance       provenanceCanonical `json:"provenance"`
	Findings         []findingCanonical  `json:"findings"`
	Checks           []checkCanonical    `json:"checks"`
	Recommendation   string              `json:"recommendation"`
}

type Critic struct {
	canonical criticCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewCritic(input CriticInput) (*Critic, error) {
	if input.Hypothesis == nil || !tokenPattern.MatchString(input.ReviewKey) {
		return nil, fmt.Errorf("research critic identity is invalid")
	}
	provenance, err := normalizeProvenance(input.Provenance)
	if err != nil {
		return nil, err
	}
	author := input.Hypothesis.canonical.Provenance
	if provenance.Provider == author.Provider && provenance.Model == author.Model && provenance.SystemPromptSHA256 == author.SystemPromptSHA256 && provenance.DeveloperPromptSHA256 == author.DeveloperPromptSHA256 && provenance.UserPromptSHA256 == author.UserPromptSHA256 {
		return nil, fmt.Errorf("research critic provenance is not independent")
	}
	references := hypothesisReferences(input.Hypothesis)
	findings, err := normalizeFindings(input.Findings, references)
	if err != nil {
		return nil, err
	}
	checks, err := normalizeChecks(input.Checks, references)
	if err != nil {
		return nil, err
	}
	recommendation := criticRecommendation(findings, checks)
	canonical := criticCanonical{CriticSchemaV1, "reviewed", input.ReviewKey, input.Hypothesis.ID().String(), input.Hypothesis.Digest(), provenance, findings, checks, recommendation}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal research critic: %w", err)
	}
	digest := hash(encoded)
	return &Critic{canonical, encoded, digest, economicid.DeterministicUUID("independent-research-critic", CriticSchemaV1+"@sha256:"+digest)}, nil
}

func hypothesisReferences(hypothesis *Hypothesis) map[string]struct{} {
	result := map[string]struct{}{
		"hypothesis:sha256:" + hypothesis.Digest():                           {},
		"manifest:sha256:" + hypothesis.canonical.Parents.ManifestSHA256:     {},
		"assessment:sha256:" + hypothesis.canonical.Parents.AssessmentSHA256: {},
		"spec:sha256:" + hypothesis.canonical.Parents.SpecSHA256:             {},
		"version:sha256:" + hypothesis.canonical.Parents.VersionSHA256:       {},
	}
	for _, source := range hypothesis.canonical.Sources {
		result["source:"+source.Key] = struct{}{}
	}
	for _, search := range hypothesis.canonical.Searches {
		result["search:"+search.Key] = struct{}{}
	}
	for _, test := range hypothesis.canonical.Tests {
		result["test:"+test.Key] = struct{}{}
	}
	return result
}

func normalizeFindings(values []FindingInput, allowedReferences map[string]struct{}) ([]findingCanonical, error) {
	if len(values) > 128 {
		return nil, fmt.Errorf("research critic findings are invalid")
	}
	categories := map[string]bool{"source_coverage": true, "leakage": true, "multiple_testing": true, "cost_capacity": true, "test_completeness": true, "reproducibility": true}
	severities := map[string]bool{"low": true, "medium": true, "high": true, "critical": true}
	statuses := map[string]bool{"open": true, "resolved": true}
	result := make([]findingCanonical, 0, len(values))
	for _, value := range values {
		references, err := normalizeReferences(value.References, allowedReferences)
		if !tokenPattern.MatchString(value.Key) || !categories[value.Category] || !severities[value.Severity] || !statuses[value.Status] || !canonicalText(value.Explanation, 4096) || err != nil {
			return nil, fmt.Errorf("research critic finding is invalid")
		}
		result = append(result, findingCanonical{Key: value.Key, Category: value.Category, Severity: value.Severity, Status: value.Status, References: references, Explanation: value.Explanation})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	for index := range result {
		if index > 0 && result[index-1].Key == result[index].Key {
			return nil, fmt.Errorf("research critic finding is duplicated")
		}
		result[index].Sequence = index
	}
	return result, nil
}

func normalizeChecks(values []CheckInput, allowedReferences map[string]struct{}) ([]checkCanonical, error) {
	if len(values) != len(requiredCriticChecks) {
		return nil, fmt.Errorf("research critic checks are incomplete")
	}
	allowedNames := map[string]bool{}
	for _, name := range requiredCriticChecks {
		allowedNames[name] = true
	}
	states := map[string]bool{"pass": true, "fail": true, "unknown": true}
	result := make([]checkCanonical, 0, len(values))
	for _, value := range values {
		references, err := normalizeReferences(value.References, allowedReferences)
		if !allowedNames[value.Name] || !states[value.State] || !canonicalText(value.Explanation, 4096) || err != nil {
			return nil, fmt.Errorf("research critic check is invalid")
		}
		result = append(result, checkCanonical{Name: value.Name, State: value.State, References: references, Explanation: value.Explanation})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	for index := range result {
		if index > 0 && result[index-1].Name == result[index].Name {
			return nil, fmt.Errorf("research critic check is duplicated")
		}
		result[index].Sequence = index
	}
	return result, nil
}

func normalizeReferences(values []string, allowed map[string]struct{}) ([]string, error) {
	if len(values) == 0 || len(values) > 128 {
		return nil, fmt.Errorf("research critic references are invalid")
	}
	result := append([]string(nil), values...)
	sort.Strings(result)
	for index, value := range result {
		if _, ok := allowed[value]; !ok || index > 0 && result[index-1] == value {
			return nil, fmt.Errorf("research critic reference is invalid")
		}
	}
	return result, nil
}

func criticRecommendation(findings []findingCanonical, checks []checkCanonical) string {
	for _, finding := range findings {
		if finding.Status == "open" && (finding.Severity == "high" || finding.Severity == "critical") {
			return "reject"
		}
	}
	for _, check := range checks {
		if check.State != "pass" {
			return "revise"
		}
	}
	return "ready_for_experiment_review"
}

func CriticFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, hypothesis *Hypothesis) (*Critic, error) {
	var canonical criticCanonical
	if id == uuid.Nil || hypothesis == nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil || canonical.Schema != CriticSchemaV1 || canonical.State != "reviewed" || canonical.HypothesisID != hypothesis.ID().String() || canonical.HypothesisSHA256 != hypothesis.Digest() {
		return nil, fmt.Errorf("stored research critic is invalid")
	}
	input := CriticInput{Hypothesis: hypothesis, ReviewKey: canonical.ReviewKey, Provenance: ProvenanceInput{canonical.Provenance.Provider, canonical.Provenance.Model, canonical.Provenance.SystemPromptSHA256, canonical.Provenance.DeveloperPromptSHA256, canonical.Provenance.UserPromptSHA256, canonical.Provenance.InputTokens, canonical.Provenance.OutputTokens, canonical.Provenance.Currency, canonical.Provenance.Cost}}
	for _, finding := range canonical.Findings {
		input.Findings = append(input.Findings, FindingInput{finding.Key, finding.Category, finding.Severity, finding.Status, append([]string(nil), finding.References...), finding.Explanation})
	}
	for _, check := range canonical.Checks {
		input.Checks = append(input.Checks, CheckInput{check.Name, check.State, append([]string(nil), check.References...), check.Explanation})
	}
	rebuilt, err := NewCritic(input)
	if err != nil || rebuilt.id != id || rebuilt.digest != digest || !bytes.Equal(rebuilt.bytes, raw) || rebuilt.canonical.Recommendation != canonical.Recommendation {
		return nil, fmt.Errorf("stored research critic does not reconstruct")
	}
	return rebuilt, nil
}

func (c *Critic) ID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.id
}

func (c *Critic) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}

func (c *Critic) ReviewKey() string {
	if c == nil {
		return ""
	}
	return c.canonical.ReviewKey
}

func (c *Critic) Recommendation() string {
	if c == nil {
		return ""
	}
	return c.canonical.Recommendation
}

func (c *Critic) HypothesisID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return uuid.MustParse(c.canonical.HypothesisID)
}

func (c *Critic) HypothesisDigest() string {
	if c == nil {
		return ""
	}
	return c.canonical.HypothesisSHA256
}

func (c *Critic) CanonicalBytes() json.RawMessage {
	if c == nil {
		return nil
	}
	return append(json.RawMessage(nil), c.bytes...)
}
