package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
	"github.com/PatrickFanella/get-rich-quick/internal/financialscheduler"
)

func TestFinancialSchedulerRetainedQualification(t *testing.T) {
	url := os.Getenv("FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL")
	if url == "" {
		t.Skip("set FINANCIAL_SCHEDULER_QUALIFICATION_DB_URL to a dedicated empty schema-98 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_job_occurrences`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("existing=%d/%v", count, err)
	}
	repo := NewFinancialSchedulerRepo(pool)
	repo.afterStage = func(stage string) error {
		if stage == "definitions" {
			return errors.New("injected")
		}
		return nil
	}
	if err := repo.RegisterCatalog(ctx, financialscheduler.Catalog()); err == nil {
		t.Fatal("definition stage rollback accepted")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_job_definitions`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("definition partial=%d/%v", count, err)
	}
	repo.afterStage = nil
	if err := repo.RegisterCatalog(ctx, financialscheduler.Catalog()); err != nil {
		t.Fatal(err)
	}

	due := time.Date(2026, 8, 20, 20, 4, 0, 0, time.UTC)
	occurrence, err := financialscheduler.NewOccurrence(financialscheduler.OccurrenceInput{JobKey: "strategy_execution", ScheduleRevision: "strategy-cron-v1@sha256:" + strings.Repeat("a", 64), Trigger: financialscheduler.TriggerScheduled, DueAt: due})
	if err != nil {
		t.Fatal(err)
	}
	for _, failedStage := range []string{"occurrence", "lease_event"} {
		rollbackRepo := NewFinancialSchedulerRepo(pool)
		rollbackRepo.afterStage = func(stage string) error {
			if stage == failedStage {
				return errors.New("injected")
			}
			return nil
		}
		if _, err := rollbackRepo.Acquire(ctx, occurrence, uuid.New(), 5*time.Second); err == nil {
			t.Fatalf("stage %s accepted", failedStage)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_job_occurrences WHERE id=$1`, occurrence.ID).Scan(&count); err != nil || count != 0 {
			t.Fatalf("stage %s partial=%d/%v", failedStage, count, err)
		}
	}
	type result struct {
		acquisition financialscheduler.Acquisition
		err         error
	}
	results := make(chan result, 8)
	var wait sync.WaitGroup
	for i := range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			owner := economicid.DeterministicUUID("ovr604-owner", string(rune('a'+i)))
			acquisition, acquireErr := NewFinancialSchedulerRepo(pool).Acquire(ctx, occurrence, owner, 5*time.Second)
			results <- result{acquisition, acquireErr}
		}()
	}
	wait.Wait()
	close(results)
	var winner financialscheduler.Lease
	wins := 0
	for result := range results {
		if result.err != nil {
			t.Error(result.err)
			continue
		}
		if result.acquisition.Acquired {
			winner = result.acquisition.Lease
			wins++
		}
	}
	if wins != 1 || winner.FenceToken != 1 {
		t.Fatalf("wins=%d lease=%+v", wins, winner)
	}

	payload := strings.Repeat("b", 64)
	effect, err := financialscheduler.NewEffect(financialscheduler.EffectInput{OccurrenceID: occurrence.ID, Kind: financialscheduler.EffectIntent, BusinessKey: "account/strategy/slot", PayloadSHA256: payload})
	if err != nil {
		t.Fatal(err)
	}
	repo.afterStage = func(stage string) error {
		if stage == "effect_claim" {
			return errors.New("injected")
		}
		return nil
	}
	if _, err := repo.ClaimEffect(ctx, winner, effect); err == nil {
		t.Fatal("effect stage rollback accepted")
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM financial_job_effect_claims WHERE id=$1`, effect.ID).Scan(&count); err != nil || count != 0 {
		t.Fatalf("effect partial=%d/%v", count, err)
	}
	repo.afterStage = nil
	if _, err := repo.ClaimEffect(ctx, winner, effect); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ClaimEffect(ctx, winner, effect); err != nil {
		t.Fatalf("exact effect replay: %v", err)
	}
	changed, _ := financialscheduler.NewEffect(financialscheduler.EffectInput{OccurrenceID: occurrence.ID, Kind: financialscheduler.EffectIntent, BusinessKey: effect.BusinessKey, PayloadSHA256: strings.Repeat("c", 64)})
	if _, err := repo.ClaimEffect(ctx, winner, changed); !errors.Is(err, ErrFinancialEffectConflict) {
		t.Fatalf("changed effect=%v", err)
	}
	stale := winner
	stale.OwnerID = uuid.MustParse("60400000-0000-4000-8000-000000000099")
	if _, err := repo.ClaimEffect(ctx, stale, effect); !errors.Is(err, ErrFinancialLeaseNotCurrent) {
		t.Fatalf("stale effect=%v", err)
	}
	winner, err = repo.Renew(ctx, winner, 5*time.Second)
	if err != nil || winner.Sequence != 2 {
		t.Fatalf("renew=%+v/%v", winner, err)
	}
	repo.afterStage = func(stage string) error {
		if stage == "terminal_event" {
			return errors.New("injected")
		}
		return nil
	}
	if err := repo.Complete(ctx, winner, true, strings.Repeat("d", 64)); err == nil {
		t.Fatal("terminal stage rollback accepted")
	}
	repo.afterStage = nil
	if err := repo.Complete(ctx, winner, true, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	terminal, err := NewFinancialSchedulerRepo(pool).Acquire(ctx, occurrence, uuid.New(), 5*time.Second)
	if err != nil || !terminal.Terminal || terminal.Acquired {
		t.Fatalf("terminal reacquire=%+v/%v", terminal, err)
	}

	takeoverOccurrence, err := financialscheduler.NewOccurrence(financialscheduler.OccurrenceInput{JobKey: "kalshi_settlement", ScheduleRevision: "settlement-v1", Trigger: financialscheduler.TriggerScheduled, DueAt: due.Add(time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	first, err := repo.Acquire(ctx, takeoverOccurrence, uuid.MustParse("60400000-0000-4000-8000-000000000011"), time.Second)
	if err != nil || !first.Acquired {
		t.Fatalf("first takeover lease=%+v/%v", first, err)
	}
	if _, err := pool.Exec(ctx, `SELECT pg_sleep(1.05)`); err != nil {
		t.Fatal(err)
	}
	second, err := NewFinancialSchedulerRepo(pool).Acquire(ctx, takeoverOccurrence, uuid.MustParse("60400000-0000-4000-8000-000000000012"), 5*time.Second)
	if err != nil || !second.Acquired || second.Lease.FenceToken != 2 {
		t.Fatalf("takeover=%+v/%v", second, err)
	}
	settlement, _ := financialscheduler.NewEffect(financialscheduler.EffectInput{OccurrenceID: takeoverOccurrence.ID, Kind: financialscheduler.EffectSettlement, BusinessKey: "kalshi/decision/one", PayloadSHA256: strings.Repeat("e", 64)})
	if _, err := repo.ClaimEffect(ctx, first.Lease, settlement); !errors.Is(err, ErrFinancialLeaseNotCurrent) {
		t.Fatalf("expired owner effect=%v", err)
	}
	if _, err := repo.ClaimEffect(ctx, second.Lease, settlement); err != nil {
		t.Fatal(err)
	}

	forgedID := economicid.DeterministicUUID("financial-job-lease-event", takeoverOccurrence.ID.String(), "4", "renewed", second.Lease.OwnerID.String(), "99")
	_, err = pool.Exec(ctx, `INSERT INTO financial_job_lease_events(id,occurrence_id,sequence,event_kind,owner_id,fence_token,occurred_at,lease_expires_at) VALUES($1,$2,4,'renewed',$3,99,date_trunc('microseconds',clock_timestamp()),date_trunc('microseconds',clock_timestamp())+interval '5 seconds')`, forgedID, takeoverOccurrence.ID, second.Lease.OwnerID)
	if err == nil || !strings.Contains(err.Error(), "renewal is not authorized") {
		t.Fatalf("direct forgery=%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE financial_job_effect_claims SET payload_sha256=payload_sha256 WHERE id=$1`, settlement.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	if _, err := pool.Exec(ctx, repositoryMigrationSQL(t, "000098_idempotent_financial_scheduler.down.sql")); err == nil || !strings.Contains(err.Error(), "rollback refused") {
		t.Fatalf("nonempty rollback=%v", err)
	}

	var definitions, occurrences, events, effects int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM financial_job_definitions),(SELECT count(*) FROM financial_job_occurrences),(SELECT count(*) FROM financial_job_lease_events),(SELECT count(*) FROM financial_job_effect_claims)`).Scan(&definitions, &occurrences, &events, &effects); err != nil || definitions != len(financialscheduler.Catalog()) || occurrences != 2 || events != 5 || effects != 2 {
		t.Fatalf("counts=%d/%d/%d/%d err=%v", definitions, occurrences, events, effects, err)
	}
	t.Logf("occurrence=%s sha=%s intent_effect=%s takeover=%s settlement_effect=%s", occurrence.ID, occurrence.SHA256, effect.ID, takeoverOccurrence.ID, settlement.ID)
}
