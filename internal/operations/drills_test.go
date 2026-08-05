package operations

import (
	"encoding/json"
	"os"
	"testing"
)

func TestValidateRecoveryDrillsRequiresEveryScenarioAndEvidence(t *testing.T) {
	results := make([]DrillResult, 0, len(RequiredRecoveryDrills))
	for _, name := range RequiredRecoveryDrills {
		results = append(results, DrillResult{Name: name, Passed: true, Evidence: "automated test and runbook"})
	}
	if err := ValidateRecoveryDrills(results); err != nil {
		t.Fatalf("complete drills rejected: %v", err)
	}
	results[0].Evidence = ""
	if err := ValidateRecoveryDrills(results); err == nil {
		t.Fatal("missing evidence accepted")
	}
}

func TestRepositoryRecoveryDrillManifestIsComplete(t *testing.T) {
	raw, err := os.ReadFile("testdata/recovery_drills.json")
	if err != nil {
		t.Fatal(err)
	}
	var results []DrillResult
	if err := json.Unmarshal(raw, &results); err != nil {
		t.Fatal(err)
	}
	if err := ValidateRecoveryDrills(results); err != nil {
		t.Fatal(err)
	}
}
