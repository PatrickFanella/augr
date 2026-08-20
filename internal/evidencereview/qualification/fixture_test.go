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
}
