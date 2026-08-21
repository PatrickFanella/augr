package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	experimentqualification "github.com/PatrickFanella/get-rich-quick/internal/experimentrun/qualification"
	trendqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/trend/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestTrendRunnerScoredAndStressReplayThroughCommonBoundaries(t *testing.T) {
	for _, mode := range []strategycatalog.ExperimentMode{strategycatalog.ExperimentPaperScored, strategycatalog.ExperimentPaperStress} {
		t.Run(string(mode), func(t *testing.T) {
			pool := newExperimentRunnerGoldenPool(t)
			fixture, err := trendqualification.Build(mode)
			if err != nil {
				t.Fatal(err)
			}
			persistMomentumRunnerFixture(t, pool, fixture.Base)
			catalog := NewStrategyCatalogRepo(pool)
			ctx := context.Background()
			if _, err = catalog.RegisterStrategyFamily(ctx, fixture.Family); err != nil {
				t.Fatal(err)
			}
			if _, err = catalog.RegisterStrategyVersion(ctx, fixture.Version); err != nil {
				t.Fatal(err)
			}
			if _, err = catalog.DeclareResearchExperiment(ctx, fixture.Graph.Experiment); err != nil {
				t.Fatal(err)
			}
			runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
			first := runTrendExperiment(t, runner, fixture, uuid.New(), 0)
			second := runTrendExperiment(t, runner, fixture, uuid.New(), time.Second)
			if first.ID() != second.ID() || first.Digest() != second.Digest() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
				t.Fatal("trend runner replay diverged")
			}
			metrics := first.Metrics()
			if metrics.StepCount != 2 || metrics.IntentCount != 2 || metrics.OrderCount != 2 || metrics.FillCount != 2 || metrics.FeeTotal == "0" {
				t.Fatalf("trend metrics=%+v", metrics)
			}
		})
	}
}

func TestTrendRunnerRejectsUnknownContract(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	fixture, err := trendqualification.Build(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	persistMomentumRunnerFixture(t, pool, fixture.Base)
	catalog := NewStrategyCatalogRepo(pool)
	ctx := context.Background()
	_, _ = catalog.RegisterStrategyFamily(ctx, fixture.Family)
	_, _ = catalog.RegisterStrategyVersion(ctx, fixture.Version)
	_, _ = catalog.DeclareResearchExperiment(ctx, fixture.Graph.Experiment)
	delete(fixture.Graph.VenueContracts, fixture.Base.VenueContract.ID)
	runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
	_, err = runner.Run(ctx, trendRunRequest(fixture, uuid.New(), 0))
	if err == nil || !strings.Contains(err.Error(), "reference or no-lookahead evidence is invalid") {
		t.Fatalf("unknown contract=%v", err)
	}
}

func runTrendExperiment(t *testing.T, runner *experimentrun.Runner, fixture *trendqualification.Fixture, attemptID uuid.UUID, offset time.Duration) *experimentrun.Result {
	t.Helper()
	result, err := runner.Run(context.Background(), trendRunRequest(fixture, attemptID, offset))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func trendRunRequest(fixture *trendqualification.Fixture, attemptID uuid.UUID, offset time.Duration) experimentrun.RunRequest {
	return experimentrun.RunRequest{ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: attemptID, StartedAt: trendqualification.Start.Add(-time.Second).Add(offset), FinishedAt: trendqualification.End.Add(offset), Program: fixture.Program}
}
