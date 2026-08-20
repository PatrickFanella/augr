package qualification

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestBuildMomentumRunnerFixtureScoredAndStress(t *testing.T) {
	t.Parallel()
	for _, mode := range []strategycatalog.ExperimentMode{strategycatalog.ExperimentPaperScored, strategycatalog.ExperimentPaperStress} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			t.Parallel()
			first, err := Build(mode)
			if err != nil {
				t.Fatal(err)
			}
			second, err := Build(mode)
			if err != nil || first.Program.Identity().ID() != second.Program.Identity().ID() || first.Scenario.ID() != second.Scenario.ID() {
				t.Fatalf("fixture replay=%v", err)
			}
		})
	}
}

func TestBuildRetainedScenarios(t *testing.T) {
	fixture, err := BuildRetainedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Scenarios) != 4 || len(fixture.Reports) != 4 || len(fixture.Reports["regime_transitions"].Rebalances()) != 4 {
		t.Fatalf("retained scenarios=%d reports=%d", len(fixture.Scenarios), len(fixture.Reports))
	}
}
