package options

import "testing"

func TestRecordOptionsDeploymentOutcomeSeparatesCreateReuseAndDryRun(t *testing.T) {
	result := &OptionsDiscoveryResult{}
	recordOptionsDeploymentOutcome(result, false, true)
	recordOptionsDeploymentOutcome(result, false, false)
	recordOptionsDeploymentOutcome(result, true, false)
	if result.Proposed != 3 || result.Created != 1 || result.Reused != 1 || result.Deployed != 1 {
		t.Fatalf("deployment outcome = %+v", result)
	}
}
