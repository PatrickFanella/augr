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

	"github.com/PatrickFanella/get-rich-quick/internal/copyreplay"
	copyreplayqualification "github.com/PatrickFanella/get-rich-quick/internal/copyreplay/qualification"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestCopyReplayRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("COPY_REPLAY_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set COPY_REPLAY_QUALIFICATION_DB_URL to a dedicated empty schema-91 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 91 {
		t.Fatalf("version=%d/%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT count(*) FROM copy_13f_replays`).Scan(&existing); err != nil || existing != 0 {
		t.Fatalf("existing=%d/%v", existing, err)
	}
	input, err := copyreplayqualification.Build()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = NewDatasetRepo(pool).RecordDatasetManifest(ctx, input.Manifest, time.Date(2026, 8, 20, 15, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	replay, err := copyreplay.NewReplay(input)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCopyReplayRepo(pool)
	for _, failed := range []string{"parent", "candidate", "filing", "manager", "decision", "step"} {
		repo.afterStage = func(stage string) error {
			if stage == failed {
				return errors.New("injected")
			}
			return nil
		}
		if _, stageErr := repo.RegisterReplay(ctx, replay); stageErr == nil {
			t.Fatalf("stage %s accepted", failed)
		}
		if err = pool.QueryRow(ctx, `SELECT count(*) FROM copy_13f_replays`).Scan(&existing); err != nil || existing != 0 {
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
			_, writeErr := NewCopyReplayRepo(pool).RegisterReplay(ctx, replay)
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
	loaded, err := NewCopyReplayRepo(pool).GetReplay(ctx, replay.ID(), input.Manifest)
	if err != nil || loaded.Digest() != replay.Digest() {
		t.Fatalf("restart=%v/%v", loaded, err)
	}
	changedInput := input
	changedInput.DecisionTimes = append([]time.Time(nil), input.DecisionTimes...)
	changedInput.DecisionTimes[len(changedInput.DecisionTimes)-1] = changedInput.DecisionTimes[len(changedInput.DecisionTimes)-1].Add(time.Hour)
	changed, err := copyreplay.NewReplay(changedInput)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repo.RegisterReplay(ctx, changed); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed retry=%v", err)
	}
	if _, err = pool.Exec(ctx, `UPDATE copy_13f_replay_decisions SET status='no_filing' WHERE replay_id=$1 AND sequence=1`, replay.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO copy_13f_replay_steps(replay_id,sequence,decision_sequence,partition_content_sha256,observation_source_key,observation_content_sha256,available_at,decision,canonical_row) VALUES($1,99,0,$2,'forged',$3,NOW(),'{}','{}')`, replay.ID(), strings.Repeat("e", 64), strings.Repeat("e", 64)); err == nil || !strings.Contains(err.Error(), "does not reconstruct") {
		t.Fatalf("forgery=%v", err)
	}
	var replays, candidates, filings, managers, decisions, steps int
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM copy_13f_replays),(SELECT count(*) FROM copy_13f_replay_candidates),(SELECT count(*) FROM copy_13f_replay_filings),(SELECT count(*) FROM copy_13f_replay_managers),(SELECT count(*) FROM copy_13f_replay_decisions),(SELECT count(*) FROM copy_13f_replay_steps)`).Scan(&replays, &candidates, &filings, &managers, &decisions, &steps); err != nil || replays != 1 || candidates != 3 || filings != 4 || managers != 2 || decisions != 10 || steps != 4 {
		t.Fatalf("counts=%d/%d/%d/%d/%d/%d err=%v", replays, candidates, filings, managers, decisions, steps, err)
	}
	t.Logf("manifest=%s replay=%s sha=%s candidates=%d filings=%d managers=%d decisions=%d steps=%d", input.Manifest.ID(), replay.ID(), replay.Digest(), candidates, filings, managers, decisions, steps)
}
