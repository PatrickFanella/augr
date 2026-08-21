package strategycatalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

const (
	DeploymentSchemaV1            = "strategy-deployment-v1"
	DeploymentProposed            = "proposed"
	DeploymentActivationAuthority = "promotion-evaluator-v1"
	deploymentDomain              = "strategy-deployment"
)

type DeploymentInput struct {
	VersionID         uuid.UUID
	AccountID         uuid.UUID
	CapitalBindingID  uuid.UUID
	Budget            string
	ScheduleCron      string
	Timezone          string
	RiskPolicyVersion string
	Mode              ExperimentMode
}

type deploymentCanonical struct {
	Schema              string         `json:"schema"`
	State               string         `json:"state"`
	ActivationAuthority string         `json:"activation_authority"`
	VersionID           string         `json:"version_id"`
	AccountID           string         `json:"account_id"`
	CapitalBindingID    string         `json:"capital_binding_id"`
	Budget              string         `json:"budget"`
	ScheduleCron        string         `json:"schedule_cron"`
	Timezone            string         `json:"timezone"`
	RiskPolicyVersion   string         `json:"risk_policy_version"`
	Mode                ExperimentMode `json:"mode"`
}

type Deployment struct {
	canonical deploymentCanonical
	bytes     json.RawMessage
	digest    string
	id        uuid.UUID
}

func NewDeployment(input DeploymentInput) (*Deployment, error) {
	budget, err := canonicalPositiveDecimal(input.Budget)
	if input.VersionID == uuid.Nil || input.AccountID == uuid.Nil || input.CapitalBindingID == uuid.Nil || err != nil ||
		!canonicalText(input.ScheduleCron, 256) || !canonicalText(input.Timezone, 128) ||
		!canonicalText(input.RiskPolicyVersion, 256) ||
		(input.Mode != ExperimentPaperScored && input.Mode != ExperimentPaperStress) {
		return nil, fmt.Errorf("strategy deployment proposal is invalid")
	}
	location, err := time.LoadLocation(input.Timezone)
	if err != nil || location.String() != input.Timezone {
		return nil, fmt.Errorf("strategy deployment timezone is invalid")
	}
	canonical := deploymentCanonical{
		Schema: DeploymentSchemaV1, State: DeploymentProposed, ActivationAuthority: DeploymentActivationAuthority,
		VersionID: input.VersionID.String(), AccountID: input.AccountID.String(), CapitalBindingID: input.CapitalBindingID.String(),
		Budget: budget, ScheduleCron: input.ScheduleCron, Timezone: input.Timezone,
		RiskPolicyVersion: input.RiskPolicyVersion, Mode: input.Mode,
	}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("marshal strategy deployment: %w", err)
	}
	digest := hashBytes(encoded)
	return &Deployment{
		canonical: canonical, bytes: encoded, digest: digest,
		id: economicid.DeterministicUUID(deploymentDomain, DeploymentSchemaV1+"@sha256:"+digest),
	}, nil
}

func DeploymentFromCanonical(id uuid.UUID, digest string, raw []byte) (*Deployment, error) {
	if id == uuid.Nil || !sha256Pattern.MatchString(digest) || hashBytes(raw) != digest {
		return nil, fmt.Errorf("strategy deployment envelope is invalid")
	}
	var canonical deploymentCanonical
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&canonical); err != nil {
		return nil, err
	}
	if err := requireJSONEOF(decoder); err != nil {
		return nil, err
	}
	versionID, err := uuid.Parse(canonical.VersionID)
	if err != nil {
		return nil, err
	}
	accountID, err := uuid.Parse(canonical.AccountID)
	if err != nil {
		return nil, err
	}
	bindingID, err := uuid.Parse(canonical.CapitalBindingID)
	if err != nil {
		return nil, err
	}
	deployment, err := NewDeployment(DeploymentInput{
		VersionID: versionID, AccountID: accountID, CapitalBindingID: bindingID,
		Budget: canonical.Budget, ScheduleCron: canonical.ScheduleCron, Timezone: canonical.Timezone,
		RiskPolicyVersion: canonical.RiskPolicyVersion, Mode: canonical.Mode,
	})
	if err != nil {
		return nil, err
	}
	if canonical.Schema != DeploymentSchemaV1 || canonical.State != DeploymentProposed ||
		canonical.ActivationAuthority != DeploymentActivationAuthority || deployment.ID() != id ||
		deployment.Digest() != digest || !bytes.Equal(deployment.bytes, raw) {
		return nil, fmt.Errorf("strategy deployment canonical identity does not reconstruct")
	}
	return deployment, nil
}

func (deployment *Deployment) ID() uuid.UUID {
	if deployment == nil {
		return uuid.Nil
	}
	return deployment.id
}

func (deployment *Deployment) Digest() string {
	if deployment == nil {
		return ""
	}
	return deployment.digest
}

func (deployment *Deployment) CanonicalBytes() json.RawMessage {
	if deployment == nil {
		return nil
	}
	return append(json.RawMessage(nil), deployment.bytes...)
}

func (deployment *Deployment) VersionID() uuid.UUID {
	return deploymentUUID(deployment, func(value deploymentCanonical) string { return value.VersionID })
}

func (deployment *Deployment) AccountID() uuid.UUID {
	return deploymentUUID(deployment, func(value deploymentCanonical) string { return value.AccountID })
}

func (deployment *Deployment) CapitalBindingID() uuid.UUID {
	return deploymentUUID(deployment, func(value deploymentCanonical) string { return value.CapitalBindingID })
}

func (deployment *Deployment) Budget() string {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.Budget
}

func (deployment *Deployment) ScheduleCron() string {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.ScheduleCron
}

func (deployment *Deployment) Timezone() string {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.Timezone
}

func (deployment *Deployment) RiskPolicyVersion() string {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.RiskPolicyVersion
}

func (deployment *Deployment) Mode() ExperimentMode {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.Mode
}

func (deployment *Deployment) State() string {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.State
}

func (deployment *Deployment) ActivationAuthority() string {
	if deployment == nil {
		return ""
	}
	return deployment.canonical.ActivationAuthority
}

func deploymentUUID(deployment *Deployment, selectValue func(deploymentCanonical) string) uuid.UUID {
	if deployment == nil {
		return uuid.Nil
	}
	id, _ := uuid.Parse(selectValue(deployment.canonical))
	return id
}

func canonicalPositiveDecimal(value string) (string, error) {
	if value == "" || value != strings.TrimSpace(value) || strings.ContainsAny(value, "eE+") {
		return "", fmt.Errorf("decimal is not canonical")
	}
	parsed, err := decimal.NewFromString(value)
	if err != nil || !parsed.IsPositive() || parsed.String() != value {
		return "", fmt.Errorf("decimal is invalid")
	}
	return parsed.String(), nil
}
