package qualification

import "testing"

func TestBuild(t *testing.T) {
	fixture, err := Build()
	if err != nil {
		t.Fatal(err)
	}
	if fixture.Case == nil || len(fixture.Reviews) != 2 || fixture.Summary == nil || !fixture.Summary.EscalationRequired() {
		t.Fatalf("fixture=%v", fixture)
	}
	t.Logf("case=%s sha=%s summary=%s sha=%s held_case=%s sha=%s held_summary=%s sha=%s", fixture.Case.ID(), fixture.Case.Digest(), fixture.Summary.ID(), fixture.Summary.Digest(), fixture.HeldCase.ID(), fixture.HeldCase.Digest(), fixture.HeldSummary.ID(), fixture.HeldSummary.Digest())
}
