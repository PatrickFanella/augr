package strategycatalog

import (
	"bytes"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestDeploymentAssignmentChangeCreatesNewProposedIdentity(t *testing.T) {
	input := validDeploymentInput()
	first, err := NewDeployment(input)
	if err != nil {
		t.Fatal(err)
	}
	if first.State() != DeploymentProposed || first.ActivationAuthority() != DeploymentActivationAuthority {
		t.Fatal("deployment constructor claimed activation authority")
	}
	input.Budget = "25000"
	changed, err := NewDeployment(input)
	if err != nil {
		t.Fatal(err)
	}
	if changed.ID() == first.ID() {
		t.Fatal("changed deployment assignment reused identity")
	}
	restored, err := DeploymentFromCanonical(first.ID(), first.Digest(), first.CanonicalBytes())
	if err != nil || restored.ID() != first.ID() || restored.AccountID() != validDeploymentInput().AccountID {
		t.Fatalf("restore deployment = %+v, %v", restored, err)
	}
}

func TestDeploymentRejectsInvalidAssignment(t *testing.T) {
	valid := validDeploymentInput()
	for name, mutate := range map[string]func(*DeploymentInput){
		"version":     func(value *DeploymentInput) { value.VersionID = uuid.Nil },
		"account":     func(value *DeploymentInput) { value.AccountID = uuid.Nil },
		"binding":     func(value *DeploymentInput) { value.CapitalBindingID = uuid.Nil },
		"zero budget": func(value *DeploymentInput) { value.Budget = "0" },
		"decimal":     func(value *DeploymentInput) { value.Budget = "100.00" },
		"cron":        func(value *DeploymentInput) { value.ScheduleCron = " daily" },
		"timezone":    func(value *DeploymentInput) { value.Timezone = "Not/AZone" },
		"risk":        func(value *DeploymentInput) { value.RiskPolicyVersion = "" },
		"mode":        func(value *DeploymentInput) { value.Mode = "live" },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			mutate(&input)
			if _, err := NewDeployment(input); err == nil {
				t.Fatal("invalid deployment succeeded")
			}
		})
	}
}

func TestDeploymentRestoreRejectsActiveState(t *testing.T) {
	deployment, err := NewDeployment(validDeploymentInput())
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(deployment.CanonicalBytes(), []byte(`"proposed"`), []byte(`"active"`), 1)
	if _, err := DeploymentFromCanonical(deployment.ID(), hashBytes(tampered), tampered); err == nil {
		t.Fatal("active deployment restored through proposed-only model")
	}
}

func validDeploymentInput() DeploymentInput {
	return DeploymentInput{
		VersionID:        uuid.MustParse("30200000-0000-4000-8000-000000000020"),
		AccountID:        uuid.MustParse("30200000-0000-4000-8000-000000000021"),
		CapitalBindingID: uuid.MustParse("30200000-0000-4000-8000-000000000022"),
		Budget:           "10000", ScheduleCron: "0 14 * * 1-5", Timezone: "America/Chicago",
		RiskPolicyVersion: "risk-policy-v1@sha256:" + strings.Repeat("a", 64), Mode: ExperimentPaperScored,
	}
}
