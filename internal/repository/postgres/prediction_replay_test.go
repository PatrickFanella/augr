package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/predictionreplay"
	predictionqualification "github.com/PatrickFanella/get-rich-quick/internal/predictionreplay/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestPredictionReplayRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("PREDICTION_REPLAY_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set PREDICTION_REPLAY_QUALIFICATION_DB_URL to a dedicated empty schema-92 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 92 {
		t.Fatalf("version=%d/%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM prediction_book_fee_recorders`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	input, err := predictionqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO instruments(id,identity_key,asset_class,primary_venue,currency,tick_size,lot_size,multiplier,settlement_method,status) VALUES($1,$2,'prediction_contract','kalshi','USD',0.01,1,1,'binary','active'),($3,$4,'prediction_contract','kalshi','USD',0.01,1,1,'binary','active')`, predictionqualification.OutcomeYes, "prediction:qualification:yes", predictionqualification.OutcomeNo, "prediction:qualification:no"); err != nil {
		t.Fatal(err)
	}
	if _, err = NewDatasetRepo(pool).RecordDatasetManifest(ctx, input.Manifest, time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	recorder, err := predictionreplay.NewRecorder(input)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewPredictionReplayRepo(pool)
	for _, failed := range []string{"parent", "book", "level", "fee", "replay", "fill"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, stageErr := repo.RegisterRecorder(ctx, recorder); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM prediction_book_fee_recorders`).Scan(&existing); err != nil || existing != 0 {
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
			_, writeErr := NewPredictionReplayRepo(pool).RegisterRecorder(ctx, recorder)
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
	loaded, err := NewPredictionReplayRepo(pool).GetRecorder(ctx, recorder.ID(), input.Manifest)
	if err != nil || loaded.Digest() != recorder.Digest() {
		t.Fatalf("restart=%v/%v", loaded, err)
	}
	changedInput := input
	changedInput.Replays = append([]predictionreplay.ReplayInput(nil), input.Replays...)
	changedInput.Replays[0].LimitPrice = "0.47"
	changed, err := predictionreplay.NewRecorder(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RegisterRecorder(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE prediction_recorded_replays SET status='partial' WHERE recorder_id=$1 AND sequence=0`, recorder.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO prediction_recorded_fills(recorder_id,replay_sequence,sequence,book_level,price,quantity,gross,canonical_row) VALUES($1,0,99,0,0.5,1,0.5,'{}')`, recorder.ID()); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var recorders, books, levels, fees, replays, fills int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM prediction_book_fee_recorders),(SELECT count(*) FROM prediction_recorded_books),(SELECT count(*) FROM prediction_recorded_book_levels),(SELECT count(*) FROM prediction_recorded_fee_policies),(SELECT count(*) FROM prediction_recorded_replays),(SELECT count(*) FROM prediction_recorded_fills)`).Scan(&recorders, &books, &levels, &fees, &replays, &fills); err != nil || recorders != 1 || books != 3 || levels != 12 || fees != 3 || replays != 3 || fills != 5 {
		t.Fatalf("counts=%d/%d/%d/%d/%d/%d err=%v", recorders, books, levels, fees, replays, fills, err)
	}
	t.Logf("manifest=%s recorder=%s sha=%s books=%d levels=%d fees=%d replays=%d fills=%d", input.Manifest.ID(), recorder.ID(), recorder.Digest(), books, levels, fees, replays, fills)
}
