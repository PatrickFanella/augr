// Package evidencereview owns immutable reviews of research and promotion
// evidence. It cannot construct or mutate promotion decisions or lifecycle.
package evidencereview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/promotion"
	"github.com/PatrickFanella/get-rich-quick/internal/researchworkflow"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	CaseSchemaV1         = "evidence-review-case-v1"
	ReviewSchemaV1       = "evidence-review-v1"
	DispositionSupported = "evidence_supported"
	DispositionChanges   = "changes_requested"
	DispositionRejected  = "reject_evidence"
	timeLayout           = "2006-01-02T15:04:05.000000Z"
)

var tokenPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,95}$`)
var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var requiredChecks = []string{"cost_capacity", "policy_decision_consistency", "reproducibility", "safety_boundaries", "source_integrity", "statistical_controls"}

type CaseInput struct {
	Hypothesis        *researchworkflow.Hypothesis
	Critic            *researchworkflow.Critic
	PromotionPolicy   *promotion.Policy
	PromotionDecision *promotion.Decision
	Deployment        *strategycatalog.Deployment
	Assessment        *robustness.Assessment
}
type caseCanonical struct {
	Schema                  string   `json:"schema"`
	State                   string   `json:"state"`
	HypothesisID            string   `json:"hypothesis_id"`
	HypothesisSHA256        string   `json:"hypothesis_sha256"`
	CriticID                string   `json:"critic_id"`
	CriticSHA256            string   `json:"critic_sha256"`
	CriticRecommendation    string   `json:"critic_recommendation"`
	VersionID               string   `json:"version_id"`
	VersionSHA256           string   `json:"version_sha256"`
	PromotionPolicyID       string   `json:"promotion_policy_id"`
	PromotionPolicySHA256   string   `json:"promotion_policy_sha256"`
	PromotionDecisionID     string   `json:"promotion_decision_id"`
	PromotionDecisionSHA256 string   `json:"promotion_decision_sha256"`
	DeploymentID            string   `json:"deployment_id"`
	DeploymentSHA256        string   `json:"deployment_sha256"`
	AssessmentID            string   `json:"assessment_id"`
	AssessmentSHA256        string   `json:"assessment_sha256"`
	EvidenceReferences      []string `json:"evidence_references"`
	AuthoritativeOutcome    string   `json:"authoritative_outcome"`
	AuthoritativeNextState  string   `json:"authoritative_next_state"`
}
type Case struct {
	canonical caseCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewCase(input CaseInput) (*Case, error) {
	if input.Hypothesis == nil || input.Critic == nil || input.PromotionPolicy == nil || input.PromotionDecision == nil || input.Deployment == nil || input.Assessment == nil {
		return nil, fmt.Errorf("evidence review case parents are required")
	}
	if input.Critic.HypothesisID() != input.Hypothesis.ID() || input.Critic.HypothesisDigest() != input.Hypothesis.Digest() || input.Hypothesis.VersionID() != input.PromotionDecision.VersionID() || input.Hypothesis.VersionDigest() == "" || input.Hypothesis.AssessmentID() != input.Assessment.ID() || input.Hypothesis.AssessmentDigest() != input.Assessment.Digest() || input.PromotionDecision.AssessmentID() != input.Assessment.ID() || input.PromotionDecision.AssessmentDigest() != input.Assessment.Digest() || input.PromotionDecision.PolicyID() != input.PromotionPolicy.ID() || input.PromotionDecision.PolicyDigest() != input.PromotionPolicy.Digest() || input.PromotionDecision.DeploymentID() != input.Deployment.ID() || input.PromotionDecision.DeploymentDigest() != input.Deployment.Digest() || input.Deployment.VersionID() != input.Hypothesis.VersionID() {
		return nil, fmt.Errorf("evidence review case parent binding is invalid")
	}
	references, err := caseReferences(input)
	if err != nil {
		return nil, err
	}
	c := caseCanonical{Schema: CaseSchemaV1, State: "open", HypothesisID: input.Hypothesis.ID().String(), HypothesisSHA256: input.Hypothesis.Digest(), CriticID: input.Critic.ID().String(), CriticSHA256: input.Critic.Digest(), CriticRecommendation: input.Critic.Recommendation(), VersionID: input.Hypothesis.VersionID().String(), VersionSHA256: input.Hypothesis.VersionDigest(), PromotionPolicyID: input.PromotionPolicy.ID().String(), PromotionPolicySHA256: input.PromotionPolicy.Digest(), PromotionDecisionID: input.PromotionDecision.ID().String(), PromotionDecisionSHA256: input.PromotionDecision.Digest(), DeploymentID: input.Deployment.ID().String(), DeploymentSHA256: input.Deployment.Digest(), AssessmentID: input.Assessment.ID().String(), AssessmentSHA256: input.Assessment.Digest(), EvidenceReferences: references, AuthoritativeOutcome: input.PromotionDecision.Outcome(), AuthoritativeNextState: input.PromotionDecision.NextState()}
	raw, _ := json.Marshal(c)
	digest := hash(raw)
	return &Case{c, raw, digest, economicid.DeterministicUUID("evidence-review-case", CaseSchemaV1+"@sha256:"+digest)}, nil
}

type ReviewerInput struct{ Key, Kind, Organization, IdentitySHA256, SystemPromptSHA256, DeveloperPromptSHA256, UserPromptSHA256 string }
type CheckInput struct {
	Name, State, Explanation string
	References               []string
}
type ReviewInput struct {
	Case            *Case
	Reviewer        ReviewerInput
	ReviewedAt      time.Time
	Checks          []CheckInput
	Notes, Conflict string
	Prior           *Review
}
type reviewerCanonical struct {
	Key                   string `json:"key"`
	Kind                  string `json:"kind"`
	Organization          string `json:"organization"`
	IdentitySHA256        string `json:"identity_sha256"`
	SystemPromptSHA256    string `json:"system_prompt_sha256"`
	DeveloperPromptSHA256 string `json:"developer_prompt_sha256"`
	UserPromptSHA256      string `json:"user_prompt_sha256"`
}
type checkCanonical struct {
	Sequence    int      `json:"sequence"`
	Name        string   `json:"name"`
	Severity    string   `json:"severity"`
	State       string   `json:"state"`
	References  []string `json:"references"`
	Explanation string   `json:"explanation"`
}
type reviewCanonical struct {
	Schema                 string            `json:"schema"`
	State                  string            `json:"state"`
	CaseID                 string            `json:"case_id"`
	CaseSHA256             string            `json:"case_sha256"`
	Reviewer               reviewerCanonical `json:"reviewer"`
	ReviewedAt             string            `json:"reviewed_at"`
	PriorReviewID          string            `json:"prior_review_id"`
	PriorReviewSHA256      string            `json:"prior_review_sha256"`
	Checks                 []checkCanonical  `json:"checks"`
	Notes                  string            `json:"notes"`
	Conflict               string            `json:"conflict"`
	Disposition            string            `json:"disposition"`
	AuthoritativeOutcome   string            `json:"authoritative_outcome"`
	AuthoritativeNextState string            `json:"authoritative_next_state"`
}
type Review struct {
	canonical reviewCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewReview(input ReviewInput) (*Review, error) {
	if input.Case == nil || !canonicalTime(input.ReviewedAt) || !safeReviewText(input.Notes, 4096) || !safeReviewText(input.Conflict, 2048) {
		return nil, fmt.Errorf("evidence review identity is invalid")
	}
	reviewer, err := normalizeReviewer(input.Reviewer)
	if err != nil {
		return nil, err
	}
	checks, disposition, err := normalizeChecks(input.Checks, allowedReferences(input.Case))
	if err != nil {
		return nil, err
	}
	priorID, priorDigest := "", ""
	if input.Prior != nil {
		if input.Prior.CaseID() != input.Case.ID() || input.Prior.ReviewerKey() != reviewer.Key || !input.Prior.ReviewedAt().Before(input.ReviewedAt) {
			return nil, fmt.Errorf("evidence review prior binding is invalid")
		}
		priorID, priorDigest = input.Prior.ID().String(), input.Prior.Digest()
	}
	c := reviewCanonical{ReviewSchemaV1, "completed", input.Case.ID().String(), input.Case.Digest(), reviewer, formatTime(input.ReviewedAt), priorID, priorDigest, checks, input.Notes, input.Conflict, disposition, input.Case.canonical.AuthoritativeOutcome, input.Case.canonical.AuthoritativeNextState}
	raw, _ := json.Marshal(c)
	digest := hash(raw)
	return &Review{c, raw, digest, economicid.DeterministicUUID("evidence-review", ReviewSchemaV1+"@sha256:"+digest)}, nil
}

func normalizeReviewer(v ReviewerInput) (reviewerCanonical, error) {
	if !tokenPattern.MatchString(v.Key) || !canonicalText(v.Organization, 256) || !digestPattern.MatchString(v.IdentitySHA256) || (v.Kind != "human" && v.Kind != "independent_service") {
		return reviewerCanonical{}, fmt.Errorf("evidence reviewer is invalid")
	}
	if v.Kind == "human" {
		if v.SystemPromptSHA256 != "" || v.DeveloperPromptSHA256 != "" || v.UserPromptSHA256 != "" {
			return reviewerCanonical{}, fmt.Errorf("human reviewer cannot have prompt provenance")
		}
	} else if !digestPattern.MatchString(v.SystemPromptSHA256) || !digestPattern.MatchString(v.DeveloperPromptSHA256) || !digestPattern.MatchString(v.UserPromptSHA256) {
		return reviewerCanonical{}, fmt.Errorf("service reviewer prompt provenance is invalid")
	}
	return reviewerCanonical{v.Key, v.Kind, v.Organization, v.IdentitySHA256, v.SystemPromptSHA256, v.DeveloperPromptSHA256, v.UserPromptSHA256}, nil
}

func normalizeChecks(values []CheckInput, allowed map[string]struct{}) ([]checkCanonical, string, error) {
	if len(values) != len(requiredChecks) {
		return nil, "", fmt.Errorf("evidence review checks are incomplete")
	}
	allowedNames := map[string]bool{}
	for _, name := range requiredChecks {
		allowedNames[name] = true
	}
	states := map[string]bool{"pass": true, "fail": true, "unknown": true}
	critical := map[string]bool{"policy_decision_consistency": true, "safety_boundaries": true, "source_integrity": true, "statistical_controls": true}
	result := make([]checkCanonical, 0, len(values))
	hasConcern, hasCriticalFailure := false, false
	for _, v := range values {
		refs := append([]string(nil), v.References...)
		sort.Strings(refs)
		if !allowedNames[v.Name] || !states[v.State] || !safeReviewText(v.Explanation, 4096) || len(refs) == 0 {
			return nil, "", fmt.Errorf("evidence review check is invalid")
		}
		for i, ref := range refs {
			if _, ok := allowed[ref]; !ok || i > 0 && refs[i-1] == ref {
				return nil, "", fmt.Errorf("evidence review reference is invalid")
			}
		}
		severity := "high"
		if critical[v.Name] {
			severity = "critical"
		}
		result = append(result, checkCanonical{0, v.Name, severity, v.State, refs, v.Explanation})
		hasConcern = hasConcern || v.State != "pass"
		hasCriticalFailure = hasCriticalFailure || (v.State == "fail" && critical[v.Name])
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	for i := range result {
		if i > 0 && result[i-1].Name == result[i].Name {
			return nil, "", fmt.Errorf("evidence review check is duplicated")
		}
		result[i].Sequence = i
	}
	disposition := DispositionSupported
	if hasConcern {
		disposition = DispositionChanges
	}
	if hasCriticalFailure {
		disposition = DispositionRejected
	}
	return result, disposition, nil
}

func allowedReferences(c *Case) map[string]struct{} {
	result := map[string]struct{}{}
	for _, value := range c.canonical.EvidenceReferences {
		result[value] = struct{}{}
	}
	return result
}

func caseReferences(input CaseInput) ([]string, error) {
	result := []string{"hypothesis:sha256:" + input.Hypothesis.Digest(), "critic:sha256:" + input.Critic.Digest(), "promotion_policy:sha256:" + input.PromotionPolicy.Digest(), "promotion_decision:sha256:" + input.PromotionDecision.Digest(), "deployment:sha256:" + input.Deployment.Digest(), "assessment:sha256:" + input.Assessment.Digest(), "version:sha256:" + input.Hypothesis.VersionDigest()}
	var h struct {
		Sources []struct {
			Key string `json:"key"`
		} `json:"sources"`
		Searches []struct {
			Key string `json:"key"`
		} `json:"searches"`
		Tests []struct {
			Key string `json:"key"`
		} `json:"tests"`
	}
	if err := json.Unmarshal(input.Hypothesis.CanonicalBytes(), &h); err != nil {
		return nil, err
	}
	for _, v := range h.Sources {
		result = append(result, "source:"+v.Key)
	}
	for _, v := range h.Searches {
		result = append(result, "search:"+v.Key)
	}
	for _, v := range h.Tests {
		result = append(result, "test:"+v.Key)
	}
	var c struct {
		Findings []struct {
			Key string `json:"key"`
		} `json:"findings"`
		Checks []struct {
			Name string `json:"name"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(input.Critic.CanonicalBytes(), &c); err != nil {
		return nil, err
	}
	for _, v := range c.Findings {
		result = append(result, "critic_finding:"+v.Key)
	}
	for _, v := range c.Checks {
		result = append(result, "critic_check:"+v.Name)
	}
	for _, gate := range input.PromotionDecision.ObservedGates() {
		result = append(result, "promotion_gate:"+gate.Name)
	}
	sort.Strings(result)
	for i := range result {
		if i > 0 && result[i-1] == result[i] {
			return nil, fmt.Errorf("evidence review case reference is duplicated")
		}
	}
	return result, nil
}

func CaseFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, input CaseInput) (*Case, error) {
	var c caseCanonical
	if id == uuid.Nil || hash(raw) != digest || decodeExact(raw, &c) != nil {
		return nil, fmt.Errorf("stored evidence review case is invalid")
	}
	rebuilt, err := NewCase(input)
	if err != nil || rebuilt.id != id || rebuilt.digest != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("stored evidence review case does not reconstruct")
	}
	return rebuilt, nil
}
func ReviewFromCanonical(id uuid.UUID, digest string, raw json.RawMessage, c *Case, prior *Review) (*Review, error) {
	var v reviewCanonical
	if id == uuid.Nil || hash(raw) != digest || decodeExact(raw, &v) != nil {
		return nil, fmt.Errorf("stored evidence review is invalid")
	}
	checks := make([]CheckInput, len(v.Checks))
	for i, row := range v.Checks {
		checks[i] = CheckInput{row.Name, row.State, row.Explanation, append([]string(nil), row.References...)}
	}
	rebuilt, err := NewReview(ReviewInput{c, ReviewerInput{v.Reviewer.Key, v.Reviewer.Kind, v.Reviewer.Organization, v.Reviewer.IdentitySHA256, v.Reviewer.SystemPromptSHA256, v.Reviewer.DeveloperPromptSHA256, v.Reviewer.UserPromptSHA256}, mustTime(v.ReviewedAt), checks, v.Notes, v.Conflict, prior})
	if err != nil || rebuilt.id != id || rebuilt.digest != digest || !bytes.Equal(rebuilt.bytes, raw) {
		return nil, fmt.Errorf("stored evidence review does not reconstruct")
	}
	return rebuilt, nil
}

func (c *Case) ID() uuid.UUID {
	if c == nil {
		return uuid.Nil
	}
	return c.id
}
func (c *Case) Digest() string {
	if c == nil {
		return ""
	}
	return c.digest
}
func (c *Case) CanonicalBytes() json.RawMessage {
	if c == nil {
		return nil
	}
	return append(json.RawMessage(nil), c.bytes...)
}
func (c *Case) CriticRecommendation() string {
	if c == nil {
		return ""
	}
	return c.canonical.CriticRecommendation
}
func (c *Case) AuthoritativeOutcome() string {
	if c == nil {
		return ""
	}
	return c.canonical.AuthoritativeOutcome
}
func (c *Case) AuthoritativeNextState() string {
	if c == nil {
		return ""
	}
	return c.canonical.AuthoritativeNextState
}
func (r *Review) ID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return r.id
}
func (r *Review) Digest() string {
	if r == nil {
		return ""
	}
	return r.digest
}
func (r *Review) CanonicalBytes() json.RawMessage {
	if r == nil {
		return nil
	}
	return append(json.RawMessage(nil), r.bytes...)
}
func (r *Review) Disposition() string {
	if r == nil {
		return ""
	}
	return r.canonical.Disposition
}
func (r *Review) CaseID() uuid.UUID {
	if r == nil {
		return uuid.Nil
	}
	return uuid.MustParse(r.canonical.CaseID)
}
func (r *Review) ReviewerKey() string {
	if r == nil {
		return ""
	}
	return r.canonical.Reviewer.Key
}
func (r *Review) ReviewedAt() time.Time {
	if r == nil {
		return time.Time{}
	}
	return mustTime(r.canonical.ReviewedAt)
}
func hash(v []byte) string { d := sha256.Sum256(v); return hex.EncodeToString(d[:]) }
func canonicalText(v string, max int) bool {
	return v != "" && v == strings.TrimSpace(v) && len(v) <= max && !strings.ContainsRune(v, '\x00')
}
func safeReviewText(v string, max int) bool {
	if v != "" && !canonicalText(v, max) {
		return false
	}
	lower := strings.ToLower(v)
	for _, forbidden := range []string{"api_key", "authorization: bearer", "begin private key", "deploy now", "password=", "risk_limit=", "schedule_cron", "secret="} {
		if strings.Contains(lower, forbidden) {
			return false
		}
	}
	return true
}
func canonicalTime(v time.Time) bool {
	return !v.IsZero() && v.Location() == time.UTC && v.Nanosecond()%1000 == 0
}
func formatTime(v time.Time) string { return v.Format(timeLayout) }
func mustTime(v string) time.Time   { parsed, _ := time.Parse(timeLayout, v); return parsed }
func decodeExact(raw []byte, target any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return err
	}
	if err := d.Decode(&struct{}{}); err != io.EOF {
		return fmt.Errorf("unexpected trailing JSON")
	}
	return nil
}
