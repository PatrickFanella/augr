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
