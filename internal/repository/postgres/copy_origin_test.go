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

	"github.com/PatrickFanella/get-rich-quick/internal/copyorigin"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestCopyOriginRetainedQualification(t *testing.T) {
	databaseURL := os.Getenv("COPY_ORIGIN_QUALIFICATION_DB_URL")
	if databaseURL == "" {
		t.Skip("set COPY_ORIGIN_QUALIFICATION_DB_URL to a dedicated empty schema-88 database")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var version, existing, strategiesBefore int
	if err = pool.QueryRow(ctx, `SELECT version FROM schema_migrations WHERE NOT dirty`).Scan(&version); err != nil || version != 88 {
		t.Fatalf("version=%d err=%v", version, err)
	}
	if err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM copy_subscriptions)+(SELECT count(*) FROM copy_origin_rebalance_runs),(SELECT count(*) FROM strategies)`).Scan(&existing, &strategiesBefore); err != nil || existing != 0 {
		t.Fatalf("existing=%d err=%v", existing, err)
	}
	leaderID, sourceID, observationID := uuid.New(), uuid.New(), uuid.New()
	if _, err = pool.Exec(ctx, `INSERT INTO copy_leaders(id,entity_type,display_name) VALUES($1,'institution','OVR501 fixture')`, leaderID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `INSERT INTO copy_leader_sources(id,leader_id,provider,source_type,external_key) VALUES($1,$2,'sec','sec_13f',$3)`, sourceID, leaderID, sourceID.String()); err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 20, 18, 0, 0, 0, time.UTC)
	if _, err = pool.Exec(ctx, `INSERT INTO copy_source_observations(id,source_id,provider_observation_id,observation_kind,effective_at,published_at,observed_at,content_hash,normalized_payload) VALUES($1,$2,'filing-1','portfolio_snapshot',$3,$4,$5,$6,'{}')`, observationID, sourceID, now.Add(-24*time.Hour), now.Add(-time.Hour), now, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	subscription := domain.DefaultCopySubscription()
	subscription.ID, subscription.LeaderID, subscription.SourceID = uuid.New(), leaderID, sourceID
	subscription.OriginType, subscription.OriginID, subscription.CreatedBy = "copy_subscription", subscription.ID, "ovr501"
	if err = subscription.Validate(); err != nil {
		t.Fatal(err)
	}
	copyRepo := NewCopyTradingRepo(pool)
	if err = copyRepo.CreateSubscription(ctx, &subscription); err != nil {
		t.Fatal(err)
	}
	intents := make([]domain.CopyTradeIntent, 2)
	for i, key := range []string{"AAPL", "MSFT"} {
		id := economicid.DeterministicUUID("copy-trade-intent", subscription.ID.String(), observationID.String(), key, "1")
		intents[i] = domain.CopyTradeIntent{ID: id, SubscriptionID: subscription.ID, OriginType: "copy_subscription", OriginID: subscription.ID, SourceObservationID: observationID, InstrumentKey: key, Ticker: key, Side: domain.OrderSideBuy, TargetWeight: 0.1, TargetValue: 1000, RequestedNotional: 1000, CalculationVersion: 1, Calculation: json.RawMessage(`{"fixture":true}`), PolicyStatus: "approved", RiskStatus: "pending", Status: "received"}
		if created, createErr := copyRepo.CreateIntent(ctx, &intents[i]); createErr != nil || !created {
			t.Fatalf("intent %s created=%t err=%v", key, created, createErr)
		}
	}
	run, err := copyorigin.NewRun(subscription, intents)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCopyOriginRepo(pool)
	for _, stage := range []string{"run", "intent"} {
		repo.afterStage = func(current string) error {
			if current == stage {
				return errors.New("injected")
			}
			return nil
		}
		if _, stageErr := repo.RegisterRun(ctx, run); stageErr == nil {
			t.Fatalf("stage %s accepted", stage)
		}
		var partial int
		if countErr := pool.QueryRow(ctx, `SELECT count(*) FROM copy_origin_rebalance_runs`).Scan(&partial); countErr != nil || partial != 0 {
			t.Fatalf("stage %s partial=%d/%v", stage, partial, countErr)
		}
	}
	repo.afterStage = nil
	errs := make(chan error, 8)
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			_, writeErr := repo.RegisterRun(ctx, run)
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
	loaded, err := NewCopyOriginRepo(pool).GetRun(ctx, run.ID())
	if err != nil || loaded.Digest() != run.Digest() {
		t.Fatalf("reload=%v/%v", loaded, err)
	}
	var subscriptions, runs, children, strategiesAfter int
	err = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM copy_subscriptions),(SELECT count(*) FROM copy_origin_rebalance_runs),(SELECT count(*) FROM copy_origin_rebalance_intents),(SELECT count(*) FROM strategies)`).Scan(&subscriptions, &runs, &children, &strategiesAfter)
	if err != nil || subscriptions != 1 || runs != 1 || children != 2 || strategiesAfter != strategiesBefore {
		t.Fatalf("counts=%d/%d/%d strategies=%d->%d err=%v", subscriptions, runs, children, strategiesBefore, strategiesAfter, err)
	}
	if _, err = pool.Exec(ctx, `UPDATE copy_origin_rebalance_runs SET state=state WHERE id=$1`, run.ID()); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("append-only=%v", err)
	}
	t.Logf("subscription=%s origin=copy_subscription/%s run=%s sha=%s intents=%d strategies=%d", subscription.ID, subscription.ID, run.ID(), run.Digest(), children, strategiesAfter)
}
