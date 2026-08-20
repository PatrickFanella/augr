package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	experimentqualification "github.com/PatrickFanella/get-rich-quick/internal/experimentrun/qualification"
	wheelqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/wheel/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestWheelRunnerScoredAndStressReplayThroughCommonBoundaries(t *testing.T) {
	for _, mode := range []strategycatalog.ExperimentMode{strategycatalog.ExperimentPaperScored, strategycatalog.ExperimentPaperStress} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			pool := newExperimentRunnerGoldenPool(t)
			fixture, err := wheelqualification.Build(mode)
			if err != nil {
				t.Fatal(err)
			}
			persistWheelRunnerFixture(t, pool, fixture)
			runner, err := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
			if err != nil {
				t.Fatal(err)
			}
			first := runWheelExperiment(t, runner, fixture, uuid.New(), 0)
			second := runWheelExperiment(t, runner, fixture, uuid.New(), time.Second)
			if first.ID() != second.ID() || first.Digest() != second.Digest() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
				t.Fatal("wheel runner replay diverged")
			}
			metrics := first.Metrics()
			if metrics.StepCount != 1 || metrics.IntentCount != 1 || metrics.OrderCount != 1 || metrics.FillCount != 1 || metrics.FilledQuantity != "1" || metrics.FeeTotal != "1.01" {
				t.Fatalf("wheel runner metrics=%+v", metrics)
			}
		})
	}
}

func TestWheelRunnerRejectsUnknownContractAndCapitalExcess(t *testing.T) {
	t.Run("unknown contract", func(t *testing.T) {
		pool := newExperimentRunnerGoldenPool(t)
		fixture, err := wheelqualification.Build(strategycatalog.ExperimentPaperScored)
		if err != nil {
			t.Fatal(err)
		}
		persistWheelRunnerFixture(t, pool, fixture)
		delete(fixture.Graph.VenueContracts, fixture.VenueContract.ID)
		runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
		_, err = runner.Run(context.Background(), wheelRunRequest(fixture, uuid.New(), 0))
		if err == nil || !strings.Contains(err.Error(), "reference or no-lookahead evidence is invalid") {
			t.Fatalf("unknown contract error=%v", err)
		}
	})
	t.Run("capital excess", func(t *testing.T) {
		pool := newExperimentRunnerGoldenPool(t)
		fixture, err := wheelqualification.BuildCapitalRejected()
		if err != nil {
			t.Fatal(err)
		}
		persistWheelRunnerFixture(t, pool, fixture)
		runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
		_, err = runner.Run(context.Background(), wheelRunRequest(fixture, uuid.New(), 0))
		if err == nil || !strings.Contains(err.Error(), "capital rejected") {
			t.Fatalf("capital excess error=%v", err)
		}
	})
}

func persistWheelRunnerFixture(t *testing.T, pool *pgxpool.Pool, fixture *wheelqualification.Fixture) {
	t.Helper()
	ctx := context.Background()
	if err := NewAccountRepo(pool).Create(ctx, fixture.Graph.Account); err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err := instruments.CreateInstrument(ctx, fixture.Underlying); err != nil {
		t.Fatal(err)
	}
	if _, err := instruments.CreateInstrument(ctx, fixture.Option); err != nil {
		t.Fatal(err)
	}
	if _, err := instruments.RegisterVenueContract(ctx, fixture.VenueContract); err != nil {
		t.Fatal(err)
	}
	if _, err := NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, fixture.QuoteSnapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCapitalPolicyRepo(pool).RegisterCapitalPolicy(ctx, fixture.CapitalArtifact); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCapitalPolicyRepo(pool).BindCapitalPolicy(ctx, fixture.Graph.CapitalBinding); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSimulationPolicyRepo(pool).RegisterSimulationPolicy(ctx, fixture.SimulationArtifact); err != nil {
		t.Fatal(err)
	}
	datasets := NewDatasetRepo(pool)
	if _, err := datasets.RegisterDatasetPolicy(ctx, fixture.DatasetPolicy); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetManifest(ctx, fixture.Graph.Manifest, wheelqualification.Start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetQualityResult(ctx, fixture.Graph.Quality, wheelqualification.Start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	catalog := NewStrategyCatalogRepo(pool)
	if _, err := catalog.RegisterStrategyFamily(ctx, fixture.Family); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.RegisterStrategyVersion(ctx, fixture.Version); err != nil {
		t.Fatal(err)
	}
	if _, err := catalog.DeclareResearchExperiment(ctx, fixture.Graph.Experiment); err != nil {
		t.Fatal(err)
	}
}

func runWheelExperiment(t *testing.T, runner *experimentrun.Runner, fixture *wheelqualification.Fixture, attemptID uuid.UUID, offset time.Duration) *experimentrun.Result {
	t.Helper()
	result, err := runner.Run(context.Background(), wheelRunRequest(fixture, attemptID, offset))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func wheelRunRequest(fixture *wheelqualification.Fixture, attemptID uuid.UUID, offset time.Duration) experimentrun.RunRequest {
	return experimentrun.RunRequest{ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: attemptID, StartedAt: wheelqualification.Start.Add(-time.Second).Add(offset), FinishedAt: wheelqualification.End.Add(offset), Program: fixture.Program}
}
