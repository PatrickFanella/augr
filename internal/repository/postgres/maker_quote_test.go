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

	"github.com/PatrickFanella/get-rich-quick/internal/makerquote"
	makerqualification "github.com/PatrickFanella/get-rich-quick/internal/makerquote/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestMakerQuoteRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("MAKER_QUOTE_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set MAKER_QUOTE_QUALIFICATION_DB_URL to a dedicated empty schema-94 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 94 {
		t.Fatalf("version=%d/%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM maker_quote_candidates`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	fixture, err := makerqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	for i, outcome := range []string{fixture.RecorderInput.Books[0].OutcomeID.String(), fixture.RecorderInput.Books[2].OutcomeID.String()} {
		if _, err = pool.Exec(ctx, `INSERT INTO instruments(id,identity_key,asset_class,primary_venue,currency,tick_size,lot_size,multiplier,settlement_method,status) VALUES($1,$2,'prediction_contract','fixture','USD',0.01,1,1,'binary','active')`, outcome, fmt.Sprintf("maker-quote:outcome:%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = NewDatasetRepo(pool).RecordDatasetManifest(ctx, fixture.RecorderInput.Manifest, time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	recorder, err := predictionreplay.NewRecorder(fixture.RecorderInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewPredictionReplayRepo(pool).RegisterRecorder(ctx, recorder); err != nil {
		t.Fatal(err)
	}
	qualified, err := makerquote.NewCandidate(fixture.CandidateInput(recorder, "qualified", "0.01", "0.001"))
	if err != nil || !qualified.Qualified() {
		t.Fatalf("qualified=%v/%v", qualified, err)
	}
	repo := NewMakerQuoteRepo(pool)
	for _, failed := range []string{"parent", "scenario"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, stageErr := repo.RegisterCandidate(ctx, qualified); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM maker_quote_candidates`).Scan(&existing); err != nil || existing != 0 {
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
			_, writeErr := NewMakerQuoteRepo(pool).RegisterCandidate(ctx, qualified)
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
	loaded, err := NewMakerQuoteRepo(pool).GetCandidate(ctx, qualified.ID(), recorder)
	if err != nil || loaded.Digest() != qualified.Digest() {
		t.Fatalf("restart=%v/%v", loaded, err)
	}
	changed, err := makerquote.NewCandidate(fixture.CandidateInput(recorder, "qualified", "0.02", "0.001"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RegisterCandidate(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	adverse, err := makerquote.NewCandidate(fixture.CandidateInput(recorder, "adverse", "0", "0.1"))
	if err != nil || adverse.Reason() != "nonpositive_net_capture" {
		t.Fatalf("adverse=%v/%v", adverse, err)
	}
	if _, err = repo.RegisterCandidate(ctx, adverse); err != nil {
		t.Fatal(err)
	}
	noFillInput := fixture.CandidateInput(recorder, "no-fill", "0", "0.001")
	for i := range noFillInput.Scenarios {
		noFillInput.Scenarios[i].QueueOutflow = "10"
	}
	noFill, err := makerquote.NewCandidate(noFillInput)
	if err != nil || noFill.Reason() != "no_fill" {
		t.Fatalf("no-fill=%v/%v", noFill, err)
	}
	if _, err = repo.RegisterCandidate(ctx, noFill); err != nil {
		t.Fatal(err)
	}
	boundary, err := makerquote.NewCandidate(fixture.CandidateInput(recorder, "strict-boundary", "0.02985", "0.001"))
	if err != nil || boundary.Reason() != "nonpositive_net_capture" {
		t.Fatalf("boundary=%v/%v", boundary, err)
	}
	if _, err = repo.RegisterCandidate(ctx, boundary); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `UPDATE maker_quote_candidates SET reason='no_fill' WHERE id=$1`, qualified.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO maker_quote_scenarios(candidate_id,sequence,scenario_key,weight,horizon_at,queue_outflow,mark_book_source_key,mark_price,filled_quantity,residual_quantity,post_fill_inventory,gross_capture,maker_fee,inventory_cost,net_capture,canonical_row) VALUES($1,99,'forged',1,NOW(),0,'forged',0.5,0,0,0,0,0,0,0,'{}')`, qualified.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var candidates, scenarios int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM maker_quote_candidates),(SELECT count(*) FROM maker_quote_scenarios)`).Scan(&candidates, &scenarios); err != nil || candidates != 4 || scenarios != 8 {
		t.Fatalf("counts=%d/%d err=%v", candidates, scenarios, err)
	}
	t.Logf("recorder=%s qualified=%s sha=%s expected_net=%s adverse=%s no_fill=%s boundary=%s candidates=%d scenarios=%d", recorder.ID(), qualified.ID(), qualified.Digest(), qualified.ExpectedNetCapture(), adverse.ID(), noFill.ID(), boundary.ID(), candidates, scenarios)
}
