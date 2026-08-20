package postgres

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/experimentrun"
	experimentqualification "github.com/PatrickFanella/get-rich-quick/internal/experimentrun/qualification"
	definedriskqualification "github.com/PatrickFanella/get-rich-quick/internal/strategy/definedrisk/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/strategycatalog"
)

func TestDefinedRiskRunnerScoredAndStressReplayThroughCommonBoundaries(t *testing.T) {
	for _, mode := range []strategycatalog.ExperimentMode{strategycatalog.ExperimentPaperScored, strategycatalog.ExperimentPaperStress} {
		t.Run(string(mode), func(t *testing.T) {
			pool := newExperimentRunnerGoldenPool(t)
			fixture, err := definedriskqualification.BuildRunner(mode)
			if err != nil {
				t.Fatal(err)
			}
			persistDefinedRiskRunnerFixture(t, pool, fixture)
			runner, err := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
			if err != nil {
				t.Fatal(err)
			}
			first := runDefinedRiskExperiment(t, runner, fixture, uuid.New(), 0)
			second := runDefinedRiskExperiment(t, runner, fixture, uuid.New(), time.Second)
			if first.ID() != second.ID() || first.Digest() != second.Digest() || string(first.CanonicalBytes()) != string(second.CanonicalBytes()) {
				t.Fatal("defined-risk runner replay diverged")
			}
			metrics := first.Metrics()
			if metrics.StepCount != 2 || metrics.IntentCount != 2 || metrics.OrderCount != 2 || metrics.FillCount != 2 || metrics.FilledQuantity != "4" || metrics.FeeTotal == "0" {
				t.Fatalf("defined-risk metrics=%+v", metrics)
			}
		})
	}
}

func TestDefinedRiskRunnerRejectsUnknownContract(t *testing.T) {
	pool := newExperimentRunnerGoldenPool(t)
	fixture, err := definedriskqualification.BuildRunner(strategycatalog.ExperimentPaperScored)
	if err != nil {
		t.Fatal(err)
	}
	persistDefinedRiskRunnerFixture(t, pool, fixture)
	delete(fixture.Graph.VenueContracts, fixture.Contracts[1].ID)
	runner, _ := experimentrun.NewRunner(experimentqualification.Loader{Graph: fixture.Graph}, NewExperimentRunRepo(pool))
	_, err = runner.Run(context.Background(), definedRiskRunRequest(fixture, uuid.New(), 0))
	if err == nil || !strings.Contains(err.Error(), "reference or no-lookahead evidence is invalid") {
		t.Fatalf("unknown contract=%v", err)
	}
}

func persistDefinedRiskRunnerFixture(t *testing.T, pool *pgxpool.Pool, fixture *definedriskqualification.RunnerFixture) {
	t.Helper()
	ctx := context.Background()
	if err := NewAccountRepo(pool).Create(ctx, fixture.Graph.Account); err != nil {
		t.Fatal(err)
	}
	instruments := NewInstrumentRepo(pool)
	if _, err := instruments.CreateInstrument(ctx, fixture.Underlying); err != nil {
		t.Fatal(err)
	}
	for i := range fixture.Options {
		if _, err := instruments.CreateInstrument(ctx, fixture.Options[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := instruments.RegisterVenueContract(ctx, fixture.Contracts[i]); err != nil {
			t.Fatal(err)
		}
		if _, err := NewQuoteSnapshotRepo(pool).RecordQuoteSnapshot(ctx, fixture.Snapshots[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := NewCapitalPolicyRepo(pool).RegisterCapitalPolicy(ctx, fixture.Base.CapitalArtifact); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCapitalPolicyRepo(pool).BindCapitalPolicy(ctx, fixture.Graph.CapitalBinding); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSimulationPolicyRepo(pool).RegisterSimulationPolicy(ctx, fixture.Base.SimulationArtifact); err != nil {
		t.Fatal(err)
	}
	datasets := NewDatasetRepo(pool)
	if _, err := datasets.RegisterDatasetPolicy(ctx, fixture.Base.DatasetPolicy); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetManifest(ctx, fixture.Graph.Manifest, definedriskqualification.DecisionAt.Add(-time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := datasets.RecordDatasetQualityResult(ctx, fixture.Graph.Quality, definedriskqualification.DecisionAt.Add(-time.Hour)); err != nil {
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

func runDefinedRiskExperiment(t *testing.T, runner *experimentrun.Runner, fixture *definedriskqualification.RunnerFixture, attemptID uuid.UUID, offset time.Duration) *experimentrun.Result {
	t.Helper()
	result, err := runner.Run(context.Background(), definedRiskRunRequest(fixture, attemptID, offset))
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func definedRiskRunRequest(fixture *definedriskqualification.RunnerFixture, attemptID uuid.UUID, offset time.Duration) experimentrun.RunRequest {
	return experimentrun.RunRequest{ExperimentID: fixture.Graph.Experiment.ID(), AttemptID: attemptID, StartedAt: definedriskqualification.DecisionAt.Add(-time.Second).Add(offset), FinishedAt: definedriskqualification.ExpiryAt.Add(offset), Program: fixture.Program}
}
