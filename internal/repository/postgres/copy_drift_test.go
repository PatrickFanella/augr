package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/copydrift"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestCopyDriftRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("COPY_DRIFT_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set COPY_DRIFT_QUALIFICATION_DB_URL to a dedicated empty schema-90 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 90 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM copy_target_drift_runs`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	leaderID, sourceID, observationID := uuid.New(), uuid.New(), uuid.New()
	now := time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)
	if _, err = pool.Exec(ctx, `INSERT INTO copy_leaders(id,entity_type,display_name) VALUES($1,'institution','OVR503 fixture')`, leaderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO copy_leader_sources(id,leader_id,provider,source_type,external_key) VALUES($1,$2,'sec','sec_13f',$3)`, sourceID, leaderID, sourceID.String()); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO copy_source_observations(id,source_id,provider_observation_id,observation_kind,effective_at,published_at,observed_at,content_hash,normalized_payload) VALUES($1,$2,'same-filing','portfolio_snapshot',$3,$4,$5,$6,'{}')`, observationID, sourceID, now.Add(-24*time.Hour), now.Add(-time.Hour), now, strings.Repeat("c", 64)); err != nil {
		t.Fatal(err)
	}
	subscription := domain.DefaultCopySubscription()
	subscription.ID, subscription.LeaderID, subscription.SourceID = uuid.New(), leaderID, sourceID
	subscription.OriginType, subscription.OriginID, subscription.CreatedBy = "copy_subscription", subscription.ID, "ovr503"
	if err = subscription.Validate(); err != nil {
		t.Fatal(err)
	}
	if err = NewCopyTradingRepo(pool).CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	targets := []copydrift.Value{{InstrumentKey: "MSFT", Amount: decimal.NewFromInt(3000)}, {InstrumentKey: "AAPL", Amount: decimal.NewFromInt(6000)}}
	probe, err := copydrift.NewRun(copydrift.Input{Subscription: subscription, SourceObservationID: observationID, SessionKey: "2026-08-19/regular", SessionBudget: decimal.NewFromInt(2500), Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCopyDriftRepo(pool)
	for _, failed := range []string{"run", "leg"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, stageErr := repo.RegisterRun(ctx, probe); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM copy_target_drift_runs`).Scan(&existing); err != nil || existing != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, existing, err)
		}
	}
	repo.afterStage = nil
	currents := []copydrift.Value{}
	sessions := []string{"2026-08-20/regular", "2026-08-21/regular", "2026-08-24/regular", "2026-08-25/regular"}
	runs := make([]*copydrift.Run, 0, len(sessions))
	for index, session := range sessions {
		run, runErr := copydrift.NewRun(copydrift.Input{Subscription: subscription, SourceObservationID: observationID, SessionKey: session, SessionBudget: decimal.NewFromInt(2500), Targets: targets, Currents: currents})
		if runErr != nil {
			t.Fatal(runErr)
		}
		if index == 0 {
			var wait sync.WaitGroup
			errs := make(chan error, 8)
			for range 8 {
				wait.Add(1)
				go func() {
					defer wait.Done()
					_, writeErr := NewCopyDriftRepo(pool).RegisterRun(ctx, run)
					errs <- writeErr
				}()
			}
			wait.Wait()
			close(errs)
			for writeErr := range errs {
				if writeErr != nil {
					t.Error(writeErr)
				}
			}
		} else if _, runErr = repo.RegisterRun(ctx, run); runErr != nil {
			t.Fatal(runErr)
		}
		runs = append(runs, run)
		currents = copyDriftProjectedCurrents(t, currents, run)
		t.Logf("session=%s run=%s sha=%s turnover=%s residual=%s", session, run.ID(), run.Digest(), run.PreparedTurnover(), run.ResidualDrift())
	}
	if !runs[len(runs)-1].Converged() {
		t.Fatal("final session did not converge")
	}
	changed, err := copydrift.NewRun(copydrift.Input{Subscription: subscription, SourceObservationID: observationID, SessionKey: sessions[0], SessionBudget: decimal.NewFromInt(2000), Targets: targets})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RegisterRun(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	loaded, err := NewCopyDriftRepo(pool).GetRun(ctx, runs[2].ID())
	if err != nil || loaded.Digest() != runs[2].Digest() {
		t.Fatalf("restart=%v/%v", loaded, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE copy_target_drift_legs SET requested_notional=requested_notional+1 WHERE run_id=$1 AND sequence=0`, runs[0].ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO copy_target_drift_legs(run_id,sequence,instrument_key,side,current_value,target_value,starting_drift,requested_notional,projected_value,residual_drift,canonical_leg) VALUES($1,99,'FORGED','buy',0,1,1,1,1,0,'{}')`, runs[0].ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var observations, runCount, legCount int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM copy_source_observations WHERE source_id=$1),(SELECT count(*) FROM copy_target_drift_runs),(SELECT count(*) FROM copy_target_drift_legs)`, sourceID).Scan(&observations, &runCount, &legCount); err != nil || observations != 1 || runCount != 4 || legCount != 5 {
		t.Fatalf("counts observations/runs/legs=%d/%d/%d err=%v", observations, runCount, legCount, err)
	}
	t.Logf("subscription=%s observation=%s runs=%d legs=%d", subscription.ID, observationID, runCount, legCount)
}

func copyDriftProjectedCurrents(t *testing.T, prior []copydrift.Value, run *copydrift.Run) []copydrift.Value {
	t.Helper()
	var envelope struct {
		Legs []struct {
			InstrumentKey  string `json:"instrument_key"`
			ProjectedValue string `json:"projected_value"`
		} `json:"legs"`
	}
	if json.Unmarshal(run.CanonicalBytes(), &envelope) != nil {
		t.Fatal("decode run")
	}
	values := map[string]decimal.Decimal{}
	for _, value := range prior {
		values[value.InstrumentKey] = value.Amount
	}
	for _, leg := range envelope.Legs {
		values[leg.InstrumentKey] = decimal.RequireFromString(leg.ProjectedValue)
	}
	result := make([]copydrift.Value, 0, len(values))
	for key, value := range values {
		result = append(result, copydrift.Value{InstrumentKey: key, Amount: value})
	}
	return result
}
