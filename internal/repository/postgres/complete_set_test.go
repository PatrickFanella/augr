package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/completeset"
	completequalification "github.com/PatrickFanella/get-rich-quick/internal/completeset/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestCompleteSetRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("COMPLETE_SET_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set COMPLETE_SET_QUALIFICATION_DB_URL to a dedicated empty schema-93 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 93 {
		t.Fatalf("version=%d/%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM complete_set_candidates`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	fixture, err := completequalification.Build()
	if err != nil {
		t.Fatal(err)
	}
	for i, outcome := range fixture.Outcomes {
		if _, err = pool.Exec(ctx, `INSERT INTO instruments(id,identity_key,asset_class,primary_venue,currency,tick_size,lot_size,multiplier,settlement_method,status) VALUES($1,$2,'prediction_contract','fixture','USD',0.01,1,1,'binary','active')`, outcome, fmt.Sprintf("complete-set:outcome:%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = NewDatasetRepo(pool).RecordDatasetManifest(ctx, fixture.RecorderInput.Manifest, time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	recorder, err := predictionreplay.NewRecorder(fixture.RecorderInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewPredictionReplayRepo(pool).RegisterRecorder(ctx, recorder); err != nil {
		t.Fatal(err)
	}
	qualified, err := completeset.NewCandidate(fixture.CandidateInput(recorder, "qualified", "10", "0.5"))
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCompleteSetRepo(pool)
	for _, failed := range []string{"parent", "binding", "leg", "scenario", "scenario_leg"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, stageErr := repo.RegisterCandidate(ctx, qualified); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM complete_set_candidates`).Scan(&existing); err != nil || existing != 0 {
			t.Fatalf("stage %s partial=%d/%v", failed, existing, err)
		}
	}
	repo.afterStage = nil
	var wait sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, writeErr := NewCompleteSetRepo(pool).RegisterCandidate(ctx, qualified)
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
	loaded, err := NewCompleteSetRepo(pool).GetCandidate(ctx, qualified.ID(), recorder)
	if err != nil || loaded.Digest() != qualified.Digest() {
		t.Fatalf("restart=%v/%v", loaded, err)
	}
	changedInput := fixture.CandidateInput(recorder, "qualified", "11", "0.5")
	changed, err := completeset.NewCandidate(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RegisterCandidate(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	rejected, err := completeset.NewCandidate(fixture.CandidateInput(recorder, "low-capital", "9.19", "0.5"))
	if err != nil || rejected.Reason() != "insufficient_capital" {
		t.Fatalf("rejected=%v/%v", rejected, err)
	}
	if _, err = repo.RegisterCandidate(ctx, rejected); err != nil {
		t.Fatal(err)
	}
	boundary, err := completeset.NewCandidate(fixture.CandidateInput(recorder, "strict-boundary", "10", "0.8"))
	if err != nil || boundary.Reason() != "orphan_guard_failure" {
		t.Fatalf("boundary=%v/%v", boundary, err)
	}
	if _, err = repo.RegisterCandidate(ctx, boundary); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE complete_set_candidates SET reason='orphan_guard_failure' WHERE id=$1`, qualified.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO complete_set_orphan_scenario_legs(candidate_id,scenario_sequence,sequence,outcome_id,entry_cost,unwind_proceeds,loss,canonical_row) VALUES($1,0,99,$2,1,1,0,'{}')`, qualified.ID(), fixture.Outcomes[0]); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var candidates, bindings, legs, scenarios, scenarioLegs int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM complete_set_candidates),(SELECT count(*) FROM complete_set_bindings),(SELECT count(*) FROM complete_set_legs),(SELECT count(*) FROM complete_set_orphan_scenarios),(SELECT count(*) FROM complete_set_orphan_scenario_legs)`).Scan(&candidates, &bindings, &legs, &scenarios, &scenarioLegs); err != nil || candidates != 3 || bindings != 9 || legs != 9 || scenarios != 18 || scenarioLegs != 27 {
		t.Fatalf("counts=%d/%d/%d/%d/%d err=%v", candidates, bindings, legs, scenarios, scenarioLegs, err)
	}
	t.Logf("recorder=%s qualified=%s sha=%s rejected=%s boundary=%s books=3 candidates=%d bindings=%d legs=%d scenarios=%d scenario_legs=%d", recorder.ID(), qualified.ID(), qualified.Digest(), rejected.ID(), boundary.ID(), candidates, bindings, legs, scenarios, scenarioLegs)
}
