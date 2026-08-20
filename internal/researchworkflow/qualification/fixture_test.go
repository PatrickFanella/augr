package qualification

import "testing"

func TestBuild(t *testing.T) {
	fixture, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Hypothesis == nil || fixture.ReadyCritic.Recommendation() != "ready_for_experiment_review" || fixture.RejectCritic.Recommendation() != "reject" {
		t.Fatalf("fixture=%v/%v/%v", fixture.Hypothesis, fixture.ReadyCritic, fixture.RejectCritic)
	}
}
