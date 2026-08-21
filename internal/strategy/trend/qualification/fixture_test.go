package qualification

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestBuildTrendRunnerFixtureScoredAndStress(t *testing.T) {
	for _, mode := range []strategycatalog.ExperimentMode{strategycatalog.ExperimentPaperScored, strategycatalog.ExperimentPaperStress} {
		first, err := Build(mode)
		if err != nil {
			t.Fatal(err)
		}
		second, err := Build(mode)
		if err != nil || first.Program.Identity().ID() != second.Program.Identity().ID() || first.Scenario.ID() != second.Scenario.ID() {
			t.Fatalf("fixture %s replay=%v", mode, err)
		}
	}
}

func TestBuildRetainedScenarios(t *testing.T) {
	fixture, err := BuildRetainedScenarios()
	if err != nil {
		t.Fatal(err)
	}
	if len(fixture.Scenarios) != 5 || len(fixture.Reports) != 5 || len(fixture.Reports["turnover_multi"].Rebalances()) != 3 {
		t.Fatalf("retained scenarios=%d reports=%d", len(fixture.Scenarios), len(fixture.Reports))
	}
}
