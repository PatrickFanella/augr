package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	experimentqualification "github.com/PatrickFanella/get-rich-quick/internal/experimentrun/qualification"
	momentumqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/momentum/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestMomentumRunnerScoredAndStressReplayThroughCommonBoundaries(t *testing.T) {
	for _, mode := range []strategycatalog.ExperimentMode{strategycatalog.ExperimentPaperScored, strategycatalog.ExperimentPaperStress} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			pool := newExperimentRunnerGoldenPool(t)
			fixture, err := momentumqualification.Build(mode)
			if err != nil {
				t.Fatal(err)
			}
			persistMomentumRunnerFixture(t, pool, fixture)
			runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
			first := runMomentumExperiment(t, runner, fixture, uuid.New(), 0)
			second := runMomentumExperiment(t, runner, fixture, uuid.New(), time.Second)
			if first.ID() != second.ID() || first.Digest() != second.Digest() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
				t.Fatal("momentum runner replay diverged")
			}
			metrics := first.Metrics()
			if metrics.StepCount != 2 || metrics.IntentCount != 2 || metrics.OrderCount != 2 || metrics.FillCount != 2 || metrics.FeeTotal == "0" {
				t.Fatalf("momentum metrics=%+v", metrics)
			}
		})
	}
}

func TestMomentumRunnerRejectsUnknownContractAndCapitalExcess(t *testing.T) {
	t.Run("unknown contract", func(t *testing.T) {
		pool := newExperimentRunnerGoldenPool(t)
		fixture, err := momentumqualification.Build(strategycatalog.ExperimentPaperScored)
		if err != nil {
			t.Fatal(err)
		}
		persistMomentumRunnerFixture(t, pool, fixture)
		delete(fixture.Graph.VenueContracts, fixture.VenueContract.ID)
		runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
		_, err = runner.Run(context.Background(), momentumRunRequest(fixture, uuid.New(), 0))
		if err == nil || !strings.Contains(err.Error(), "reference or no-lookahead evidence is invalid") {
			t.Fatalf("unknown contract=%v", err)
		}
	})
	t.Run("capital excess", func(t *testing.T) {
		pool := newExperimentRunnerGoldenPool(t)
		fixture, err := momentumqualification.Build(strategycatalog.ExperimentPaperScored)
		if err != nil {
			t.Fatal(err)
		}
		fixture.VenueContract.Multiplier = decimal.NewFromInt(1_000_000)
		persistMomentumRunnerFixture(t, pool, fixture)
		runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
		_, err = runner.Run(context.Background(), momentumRunRequest(fixture, uuid.New(), 0))
		if err == nil || !strings.Contains(err.Error(), "capital rejected") {
			t.Fatalf("capital excess=%v", err)
		}
	})
}

func persistMomentumRunnerFixture(t *testing.T, pool *pgxpool.Pool, fixture *momentumqualification.Fixture) {
	t.Helper()
	ctx := context.Background()
	if err := NewAccountRepo(pool).Create(ctx, fixture.Graph.Account); err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err := instruments.CreateInstrument(ctx, fixture.Instrument); err != nil {
		t.Fatal(err)
	}
	if _, err := instruments.RegisterVenueContract(ctx, fixture.VenueContract); err != nil {
		t.Fatal(err)
	}
	quotes := NewQuoteSnapshotRepo(pool)
	for _, snapshot := range fixture.Snapshots {
		if _, err := quotes.RecordQuoteSnapshot(ctx, snapshot); err != nil {
			t.Fatal(err)
		}
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
	if _, err := datasets.RecordDatasetManifest(ctx, fixture.Graph.Manifest, momentumqualification.Start.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetQualityResult(ctx, fixture.Graph.Quality, momentumqualification.Start.Add(-time.Hour)); err != nil {
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

func runMomentumExperiment(t *testing.T, runner *experimentrun.Runner, fixture *momentumqualification.Fixture, attemptID uuid.UUID, offset time.Duration) *experimentrun.Result {
	t.Helper()
	result, err := runner.Run(context.Background(), momentumRunRequest(fixture, attemptID, offset))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func momentumRunRequest(fixture *momentumqualification.Fixture, attemptID uuid.UUID, offset time.Duration) experimentrun.RunRequest {
	return experimentrun.RunRequest{ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: attemptID, StartedAt: momentumqualification.Start.Add(-time.Second).Add(offset), FinishedAt: momentumqualification.End.Add(offset), Program: fixture.Program}
}
