package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestKalshiSettlementGateRepoIntegration(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newKalshiIntegrationPool(t, ctx)
	defer cleanup()
	if _, err := pool.Exec(ctx, `CREATE TABLE kalshi_settlement_gate (job_name TEXT PRIMARY KEY, consecutive_successes INTEGER NOT NULL DEFAULT 0, threshold INTEGER NOT NULL DEFAULT 0, eligible BOOLEAN NOT NULL DEFAULT FALSE, projection_fingerprint TEXT NOT NULL DEFAULT '', last_outcome TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '', fetched INTEGER NOT NULL DEFAULT 0, resolved INTEGER NOT NULL DEFAULT 0, would_settle_markets INTEGER NOT NULL DEFAULT 0, would_settle_decisions INTEGER NOT NULL DEFAULT 0, last_run_at TIMESTAMPTZ, updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	repo := NewKalshiSettlementGateRepo(pool)
	job := "kalshi_settlement"
	now := time.Now().UTC().Truncate(time.Second)
	if _, err := repo.RecordSuccess(ctx, job, 20, 3, 2, 1, 4, "fp-a", now); err != nil {
		t.Fatalf("RecordSuccess() = %v", err)
	}
	state, err := repo.Get(ctx, job)
	if err != nil {
		t.Fatalf("Get() = %v", err)
	}
	if state.ConsecutiveSuccesses != 1 || state.Threshold != 20 || state.Eligible || state.ProjectionFingerprint != "fp-a" {
		t.Fatalf("state after success = %#v", state)
	}
	if _, err := repo.RecordFailure(ctx, job, 20, 5, 3, 2, 6, now.Add(time.Minute), "boom"); err != nil {
		t.Fatalf("RecordFailure() = %v", err)
	}
	state, err = repo.Get(ctx, job)
	if err != nil {
		t.Fatalf("Get() 2 = %v", err)
	}
	if state.ConsecutiveSuccesses != 0 || state.LastError != "boom" || state.Eligible {
		t.Fatalf("state after failure = %#v", state)
	}
	if state.Fetched != 5 || state.Resolved != 3 || state.WouldSettleMarkets != 2 || state.WouldSettleDecisions != 6 {
		t.Fatalf("counters not persisted: %#v", state)
	}
	for i := 0; i < 20; i++ {
		if _, err := repo.RecordSuccess(ctx, job, 20, 1, 1, 1, 1, "fp-a", now); err != nil {
			t.Fatalf("RecordSuccess(%d) = %v", i, err)
		}
	}
	state, err = repo.Get(ctx, job)
	if err != nil {
		t.Fatalf("Get() 3 = %v", err)
	}
	if !state.Eligible || state.ConsecutiveSuccesses != 21 {
		t.Fatalf("eligibility after threshold = %#v", state)
	}
	if _, err := repo.RecordSuccess(ctx, job, 20, 1, 1, 1, 1, "fp-b", now); err != nil {
		t.Fatalf("RecordSuccess drift = %v", err)
	}
	state, err = repo.Get(ctx, job)
	if err != nil {
		t.Fatalf("Get() 4 = %v", err)
	}
	if state.ConsecutiveSuccesses != 1 || state.Eligible || state.ProjectionFingerprint != "fp-b" {
		t.Fatalf("state after drift reset = %#v", state)
	}
	_ = repository.ErrNotFound
}
