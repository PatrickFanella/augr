package postgres

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/accountingrecon"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestAccountingReconciliationRepoRoundTripsAndReplaysExactEvidence(t *testing.T) {
	ctx := context.Background()
	pool := newAccountingReconciliationTestPool(t, ctx)
	repo := NewAccountingReconciliationRepo(pool)
	run := accountingReconciliationTestRun(t, false)

	persisted, err := repo.RecordAccountingRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordAccountingRun() error = %v", err)
	}
	if !sameAccountingRun(persisted, run) {
		t.Fatalf("persisted run differs: got=%s want=%s", persisted.ID, run.ID)
	}
	replayed, err := repo.RecordAccountingRun(ctx, run)
	if err != nil {
		t.Fatalf("RecordAccountingRun(replay) error = %v", err)
	}
	if replayed.ID != run.ID {
		t.Fatalf("replayed ID = %s, want %s", replayed.ID, run.ID)
	}
	listed, err := repo.ListAccountingRuns(ctx, run.AccountID, 10, 0)
	if err != nil {
		t.Fatalf("ListAccountingRuns() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != run.ID {
		t.Fatalf("listed runs = %+v", listed)
	}

	changedAttestation := *run
	changedAttestation.AttestationType = "test-v1"
	changedAttestation.AttestationKeyID = "test-key"
	changedAttestation.Attestation = []byte("different opaque evidence")
	if _, err := repo.RecordAccountingRun(ctx, &changedAttestation); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed attestation replay error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestAccountingReconciliationRepoConcurrentIdenticalWritersConverge(t *testing.T) {
	ctx := context.Background()
	pool := newAccountingReconciliationTestPool(t, ctx)
	repo := NewAccountingReconciliationRepo(pool)
	run := accountingReconciliationTestRun(t, false)

	const writers = 8
	results := make(chan *accountingrecon.Run, writers)
	errorsSeen := make(chan error, writers)
	var group sync.WaitGroup
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			persisted, err := repo.RecordAccountingRun(ctx, run)
			if err != nil {
				errorsSeen <- err
				return
			}
			results <- persisted
		}()
	}
	group.Wait()
	close(results)
	close(errorsSeen)
	for err := range errorsSeen {
		t.Errorf("concurrent RecordAccountingRun() error = %v", err)
	}
	for persisted := range results {
		if persisted.ID != run.ID {
			t.Errorf("concurrent persisted ID = %s, want %s", persisted.ID, run.ID)
		}
	}
	var parentCount, resultCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_runs`).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_results`).Scan(&resultCount); err != nil {
		t.Fatal(err)
	}
	if parentCount != 1 || resultCount != len(run.Results) {
		t.Fatalf("stored parent/results = %d/%d, want 1/%d", parentCount, resultCount, len(run.Results))
	}
}

func TestAccountingReconciliationRepoPaginatesNewestFirst(t *testing.T) {
	ctx := context.Background()
	pool := newAccountingReconciliationTestPool(t, ctx)
	repo := NewAccountingReconciliationRepo(pool)
	base := time.Now().UTC().Add(-24 * time.Hour).Truncate(time.Microsecond)
	runs := []*accountingrecon.Run{
		accountingReconciliationTestRunAt(t, false, base),
		accountingReconciliationTestRunAt(t, false, base.Add(time.Hour)),
		accountingReconciliationTestRunAt(t, false, base.Add(2*time.Hour)),
	}
	for _, run := range runs {
		if _, err := repo.RecordAccountingRun(ctx, run); err != nil {
			t.Fatal(err)
		}
	}
	listed, err := repo.ListAccountingRuns(ctx, runs[0].AccountID, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != runs[1].ID {
		t.Fatalf("second newest page = %+v, want %s", listed, runs[1].ID)
	}
}

func TestAccountingReconciliationRepoChildFailureRollsBackParent(t *testing.T) {
	ctx := context.Background()
	pool := newAccountingReconciliationTestPool(t, ctx)
	repo := NewAccountingReconciliationRepo(pool)
	if _, err := pool.Exec(ctx, `ALTER TABLE accounting_reconciliation_results
		ADD CONSTRAINT test_reject_equity CHECK (fact_key <> 'metric:equity')`); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordAccountingRun(ctx, accountingReconciliationTestRun(t, false)); err == nil {
		t.Fatal("forced reconciliation child failure unexpectedly succeeded")
	}
	var parents, children int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_runs`).Scan(&parents); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_results`).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if parents != 0 || children != 0 {
		t.Fatalf("partial accounting evidence remained after child failure: parents=%d children=%d", parents, children)
	}
}

func newAccountingReconciliationTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	pools := newProjectionIntegrationPool(t, ctx)
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	path := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations", "000070_accounting_dual_run.up.sql")
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pools.owner.Exec(ctx, string(contents)); err != nil {
		t.Fatalf("apply migration 70: %v", err)
	}
	return pools.owner
}

func accountingReconciliationTestRun(t *testing.T, synthetic bool) *accountingrecon.Run {
	t.Helper()
	return accountingReconciliationTestRunAt(t, synthetic, time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond))
}

func accountingReconciliationTestRunAt(t *testing.T, synthetic bool, asOf time.Time) *accountingrecon.Run {
	t.Helper()
	accountID := uuid.MustParse("00000000-0000-4000-8000-000000000064")
	inputFor := func(source accountingrecon.SnapshotSource) accountingrecon.SnapshotInput {
		input := accountingrecon.SnapshotInput{
			Source: source, AccountID: accountID, AsOf: asOf, ObservedAt: asOf.Add(time.Second), Currency: "USD",
			ProjectionVersion: "ledger_fifo_v1", MarkSource: "test-source", MarkNamespace: "marks/test", MaxMarkAge: time.Minute,
			CaptureFenceID: "repository-fence", CaptureEpoch: 1,
			EvidenceID:       source.String() + ":repository-evidence",
			EvidenceChecksum: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			Synthetic:        synthetic, PositionCoverageComplete: true,
		}
		for _, kind := range accountingrecon.RequiredMetrics() {
			input.Metrics = append(input.Metrics, accountingrecon.MetricInput{Kind: kind, Value: decimal.NewFromInt(100), Provenance: accountingrecon.ProvenanceExactDecimal})
		}
		return input
	}
	legacy, err := accountingrecon.NewSnapshot(inputFor(accountingrecon.SourceLegacy))
	if err != nil {
		t.Fatal(err)
	}
	ledgerSnapshot, err := accountingrecon.NewSnapshot(inputFor(accountingrecon.SourceLedger))
	if err != nil {
		t.Fatal(err)
	}
	run, err := accountingrecon.Compare(accountingrecon.ComparisonInput{
		Legacy: legacy, Ledger: ledgerSnapshot, Generator: "repository-test", GeneratedAt: asOf.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}
