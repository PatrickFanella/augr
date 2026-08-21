// Package promotion owns deterministic, evidence-linked deployment lifecycle
// decisions. It does not schedule, activate, allocate, deploy, or execute.
package promotion

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/robustness"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

const (
	PolicySchemaV1   = "promotion-policy-v1"
	DecisionSchemaV1 = "promotion-retirement-decision-v1"
	OutcomeApproved  = "approved"
	OutcomeHeld      = "held"
	OutcomeRetired   = "retired"
	ActionHold       = "hold"
	ActionRetire     = "retire"
	StateShadow      = "shadow"
	StateRetired     = "retired"
)

var digestPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type PolicyInput struct {
	Version       string
	RequiredGates []string
	FailureAction string
}

type policyCanonical struct {
	Schema        string   `json:"schema"`
	Version       string   `json:"version"`
	RequiredGates []string `json:"required_gates"`
	PassAction    string   `json:"pass_action"`
	FailureAction string   `json:"failure_action"`
}

type Policy struct {
	canonical policyCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewPolicy(input PolicyInput) (*Policy, error) {
	gates := append([]string(nil), input.RequiredGates...)
	sort.Strings(gates)
	if !canonicalText(input.Version, 128) || len(gates) == 0 || len(gates) > 64 || input.FailureAction != ActionHold && input.FailureAction != ActionRetire {
		return nil, fmt.Errorf("promotion policy is invalid")
	}
	for index, gate := range gates {
		if !canonicalToken(gate) || index > 0 && gate == gates[index-1] {
			return nil, fmt.Errorf("promotion required gates are invalid")
		}
	}
	hasOverall := false
	for _, gate := range gates {
		hasOverall = hasOverall || gate == "overall_robustness"
	}
	if !hasOverall {
		return nil, fmt.Errorf("promotion policy must require overall robustness")
	}
	canonical := policyCanonical{Schema: PolicySchemaV1, Version: input.Version, RequiredGates: gates, PassAction: StateShadow, FailureAction: input.FailureAction}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Policy{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("promotion-policy", PolicySchemaV1+"@sha256:"+digest)}, nil
}

func PolicyFromCanonical(id uuid.UUID, digest string, raw []byte) (*Policy, error) {
	var canonical policyCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("promotion policy envelope is invalid")
	}
	value, err := NewPolicy(PolicyInput{Version: canonical.Version, RequiredGates: canonical.RequiredGates, FailureAction: canonical.FailureAction})
	if err != nil || canonical.Schema != PolicySchemaV1 || canonical.PassAction != StateShadow || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("promotion policy identity does not reconstruct")
	}
	return value, nil
}

func (p *Policy) ID() uuid.UUID {
	if p == nil {
		return uuid.Nil
	}
	return p.id
}

func (p *Policy) Digest() string {
	if p == nil {
		return ""
	}
	return p.digest
}

func (p *Policy) CanonicalBytes() json.RawMessage {
	if p == nil {
		return nil
	}
	return append(json.RawMessage(nil), p.bytes...)
}

func (p *Policy) Version() string {
	if p == nil {
		return ""
	}
	return p.canonical.Version
}

func (p *Policy) RequiredGates() []string {
	if p == nil {
		return nil
	}
	return append([]string(nil), p.canonical.RequiredGates...)
}

func (p *Policy) FailureAction() string {
	if p == nil {
		return ""
	}
	return p.canonical.FailureAction
}

type DecisionInput struct {
	Deployment    *strategycatalog.Deployment
	Assessment    *robustness.Assessment
	Policy        *Policy
	PriorDecision *Decision
}

type ObservedGate struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Threshold   string `json:"threshold"`
	Observed    string `json:"observed"`
	Reason      string `json:"reason"`
	Description string `json:"description"`
}

type decisionCanonical struct {
	Schema              string                         `json:"schema"`
	DeploymentID        string                         `json:"deployment_id"`
	DeploymentSHA256    string                         `json:"deployment_sha256"`
	VersionID           string                         `json:"version_id"`
	AssessmentID        string                         `json:"assessment_id"`
	AssessmentSHA256    string                         `json:"assessment_sha256"`
	FamilyID            string                         `json:"family_id"`
	RobustnessPolicyID  string                         `json:"robustness_policy_id"`
	Mode                strategycatalog.ExperimentMode `json:"mode"`
	PolicyID            string                         `json:"policy_id"`
	PolicySHA256        string                         `json:"policy_sha256"`
	PriorDecisionID     string                         `json:"prior_decision_id"`
	PriorDecisionSHA256 string                         `json:"prior_decision_sha256"`
	PriorState          string                         `json:"prior_state"`
	NextState           string                         `json:"next_state"`
	Outcome             string                         `json:"outcome"`
	Reason              string                         `json:"reason"`
	ObservedGates       []ObservedGate                 `json:"observed_gates"`
}

type Decision struct {
	canonical decisionCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewDecision(input DecisionInput) (*Decision, error) {
	if input.Deployment == nil || input.Assessment == nil || input.Policy == nil || input.Deployment.State() != strategycatalog.DeploymentProposed || input.Deployment.Mode() != input.Assessment.Mode() {
		return nil, fmt.Errorf("promotion decision parents are invalid")
	}
	var candidate *robustness.CandidateEvidence
	for _, value := range input.Assessment.Candidates() {
		if value.VersionID == input.Deployment.VersionID().String() {
			if candidate != nil {
				return nil, fmt.Errorf("promotion assessment candidate is duplicated")
			}
			copyValue := value
			candidate = &copyValue
		}
	}
	if candidate == nil {
		return nil, fmt.Errorf("promotion assessment does not contain deployment version")
	}
	gateByName := make(map[string]robustness.Gate, len(candidate.Gates))
	for _, gate := range candidate.Gates {
		if _, exists := gateByName[gate.Name]; exists {
			return nil, fmt.Errorf("promotion assessment gate is duplicated")
		}
		gateByName[gate.Name] = gate
	}
	observed := make([]ObservedGate, len(input.Policy.canonical.RequiredGates))
	allPass := true
	for index, name := range input.Policy.canonical.RequiredGates {
		gate, ok := gateByName[name]
		if !ok {
			return nil, fmt.Errorf("promotion required gate %s is missing", name)
		}
		observed[index] = ObservedGate{Name: gate.Name, State: gate.State, Threshold: gate.Threshold, Observed: gate.Observed, Reason: gate.Reason, Description: gate.Description}
		allPass = allPass && gate.State == robustness.GatePass
	}
	priorState := input.Deployment.State()
	priorID, priorDigest := "", ""
	if input.PriorDecision != nil {
		if input.PriorDecision.DeploymentID() != input.Deployment.ID() {
			return nil, fmt.Errorf("promotion prior decision deployment diverges")
		}
		priorState, priorID, priorDigest = input.PriorDecision.NextState(), input.PriorDecision.ID().String(), input.PriorDecision.Digest()
	}
	outcome, nextState, reason := OutcomeHeld, priorState, "required_gate_failed_or_transition_not_available"
	if priorState == strategycatalog.DeploymentProposed && allPass {
		outcome, nextState, reason = OutcomeApproved, StateShadow, "all_required_robustness_gates_passed"
	} else if !allPass && input.Policy.FailureAction() == ActionRetire {
		outcome, nextState, reason = OutcomeRetired, StateRetired, "required_robustness_gate_failed"
	}
	canonical := decisionCanonical{
		Schema: DecisionSchemaV1, DeploymentID: input.Deployment.ID().String(), DeploymentSHA256: input.Deployment.Digest(), VersionID: input.Deployment.VersionID().String(),
		AssessmentID: input.Assessment.ID().String(), AssessmentSHA256: input.Assessment.Digest(), FamilyID: input.Assessment.FamilyID().String(),
		RobustnessPolicyID: input.Assessment.PolicyID().String(), Mode: input.Assessment.Mode(), PolicyID: input.Policy.ID().String(), PolicySHA256: input.Policy.Digest(),
		PriorDecisionID: priorID, PriorDecisionSHA256: priorDigest, PriorState: priorState, NextState: nextState, Outcome: outcome, Reason: reason, ObservedGates: observed,
	}
	encoded, _ := json.Marshal(canonical)
	digest := hash(encoded)
	return &Decision{canonical: canonical, bytes: encoded, digest: digest, id: economicid.DeterministicUUID("promotion-retirement-decision", DecisionSchemaV1+"@sha256:"+digest)}, nil
}

func DecisionFromCanonical(id uuid.UUID, digest string, raw []byte, deployment *strategycatalog.Deployment, assessment *robustness.Assessment, policy *Policy, prior *Decision) (*Decision, error) {
	var canonical decisionCanonical
	if id == uuid.Nil || !digestPattern.MatchString(digest) || hash(raw) != digest || decodeExact(raw, &canonical) != nil {
		return nil, fmt.Errorf("promotion decision envelope is invalid")
	}
	value, err := NewDecision(DecisionInput{Deployment: deployment, Assessment: assessment, Policy: policy, PriorDecision: prior})
	if err != nil || canonical.Schema != DecisionSchemaV1 || value.ID() != id || value.Digest() != digest || !bytes.Equal(value.bytes, raw) {
		return nil, fmt.Errorf("promotion decision identity does not reconstruct")
	}
	return value, nil
}

func (d *Decision) ID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return d.id
}

func (d *Decision) Digest() string {
	if d == nil {
		return ""
	}
	return d.digest
}

func (d *Decision) CanonicalBytes() json.RawMessage {
	if d == nil {
		return nil
	}
	return append(json.RawMessage(nil), d.bytes...)
}

func (d *Decision) DeploymentID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.DeploymentID)
}

func (d *Decision) DeploymentDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.DeploymentSHA256
}

func (d *Decision) AssessmentID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.AssessmentID)
}

func (d *Decision) AssessmentDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.AssessmentSHA256
}

func (d *Decision) FamilyID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.FamilyID)
}

func (d *Decision) RobustnessPolicyID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.RobustnessPolicyID)
}

func (d *Decision) PolicyID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.PolicyID)
}

func (d *Decision) PolicyDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.PolicySHA256
}

func (d *Decision) Mode() strategycatalog.ExperimentMode {
	if d == nil {
		return ""
	}
	return d.canonical.Mode
}

func (d *Decision) VersionID() uuid.UUID {
	if d == nil {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.VersionID)
}

func (d *Decision) PriorDecisionID() uuid.UUID {
	if d == nil || d.canonical.PriorDecisionID == "" {
		return uuid.Nil
	}
	return uuid.MustParse(d.canonical.PriorDecisionID)
}

func (d *Decision) PriorDecisionDigest() string {
	if d == nil {
		return ""
	}
	return d.canonical.PriorDecisionSHA256
}

func (d *Decision) PriorState() string {
	if d == nil {
		return ""
	}
	return d.canonical.PriorState
}

func (d *Decision) NextState() string {
	if d == nil {
		return ""
	}
	return d.canonical.NextState
}

func (d *Decision) Outcome() string {
	if d == nil {
		return ""
	}
	return d.canonical.Outcome
}

func (d *Decision) Reason() string {
	if d == nil {
		return ""
	}
	return d.canonical.Reason
}

func (d *Decision) ObservedGates() []ObservedGate {
	if d == nil {
		return nil
	}
	return append([]ObservedGate(nil), d.canonical.ObservedGates...)
}

func hash(value []byte) string { digest := sha256.Sum256(value); return hex.EncodeToString(digest[:]) }

func canonicalText(value string, maximum int) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= maximum
}

func canonicalToken(value string) bool {
	if !canonicalText(value, 128) {
		return false
	}
	for _, r := range value {
		if r != '_' && r != '-' && (r < 'a' || r > 'z') {
			return false
		}
	}
	return true
}

func decodeExact(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if decoder.Decode(&extra) == nil {
		return fmt.Errorf("extra json")
	}
	return nil
}
