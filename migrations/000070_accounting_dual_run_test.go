package migrations_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/accountingrecon"
	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestAccountingDualRunMigrationDefinesStructuralEvidenceContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000070_accounting_dual_run.up.sql"))
	for _, fragment := range []string{
		"create table accounting_reconciliation_runs",
		"legacy_snapshot_bytes bytea not null",
		"ledger_snapshot_bytes bytea not null",
		"payload_bytes bytea not null",
		"capture_fence_id text not null",
		"capture_epoch bigint not null",
		"attestation_type text",
		"attestation_key_id text",
		"attestation bytea",
		"create table accounting_reconciliation_results",
		"status text not null check (status in ('equal', 'explained', 'unexplained', 'not_comparable'))",
		"create function validate_accounting_reconciliation_run",
		"digest(new.legacy_snapshot_bytes, 'sha256')",
		"digest(new.ledger_snapshot_bytes, 'sha256')",
		"digest(new.payload_bytes, 'sha256')",
		"economic_deterministic_uuid( 'accounting-reconciliation-run'",
		"create function validate_accounting_reconciliation_result",
		"accounting-reconciliation-result",
		"create function validate_accounting_reconciliation_parent_complete",
		"create function validate_accounting_reconciliation_result_set_complete",
		"create constraint trigger trg_accounting_reconciliation_run_complete",
		"deferrable initially deferred",
		"create trigger trg_accounting_reconciliation_runs_immutable",
		"create trigger trg_accounting_reconciliation_results_immutable",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration 70 to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000070_accounting_dual_run.down.sql"))
	for _, fragment := range []string{
		"lock table accounting_reconciliation_results, accounting_reconciliation_runs in access exclusive mode",
		"cannot roll back migration 70 while accounting reconciliation evidence exists",
		"drop table accounting_reconciliation_results",
		"drop table accounting_reconciliation_runs",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected migration 70 down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestAccountingDualRunMigrationRejectsForgedOrIncompleteEvidence(t *testing.T) {
	ctx, pool, accountID := newAccountingDualRunMigrationPool(t)
	run := accountingDualRunMigrationFixture(t, accountID)

	for name, options := range map[string]accountingDualRunInsertOptions{
		"forged run ID":             {runID: uuid.New()},
		"forged legacy snapshot ID": {legacySnapshotID: uuid.New()},
		"forged legacy checksum":    {legacyChecksum: strings.Repeat("0", 64)},
		"forged result ID":          {forgedResultID: true},
		"forged result status":      {forgedResultStatus: true},
		"missing payload header":    {missingPayloadGenerator: true},
		"incomplete result set":     {omitLastResult: true},
	} {
		name, options := name, options
		t.Run(name, func(t *testing.T) {
			if err := insertAccountingDualRun(ctx, pool, run, options); err == nil {
				t.Fatal("forged or incomplete accounting evidence unexpectedly committed")
			}
		})
	}

	if err := insertAccountingDualRun(ctx, pool, run, accountingDualRunInsertOptions{}); err != nil {
		t.Fatalf("canonical accounting evidence failed after rejected forgeries: %v", err)
	}
	var parentCount, resultCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_runs`).Scan(&parentCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_results`).Scan(&resultCount); err != nil {
		t.Fatal(err)
	}
	if parentCount != 1 || resultCount != len(run.Results) {
		t.Fatalf("canonical evidence counts = %d/%d, want 1/%d", parentCount, resultCount, len(run.Results))
	}
}

func TestAccountingDualRunMigrationEvidenceIsAppendOnlyAndBlocksDataRollback(t *testing.T) {
	ctx, pool, accountID := newAccountingDualRunMigrationPool(t)
	run := accountingDualRunMigrationFixture(t, accountID)
	if err := insertAccountingDualRun(ctx, pool, run, accountingDualRunInsertOptions{}); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE accounting_reconciliation_runs SET generator='forged' WHERE id=$1`, run.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("parent UPDATE error = %v, want append-only rejection", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM accounting_reconciliation_results WHERE run_id=$1`, run.ID); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("child DELETE error = %v, want append-only rejection", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000070_accounting_dual_run.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 70") {
		t.Fatalf("data-bearing rollback error = %v, want explicit refusal", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM accounting_reconciliation_runs`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("evidence count after refused rollback = %d, want 1", count)
	}
}

func TestAccountingDualRunMigrationEmptyRollbackReapplyAndLockFirst(t *testing.T) {
	ctx, pool, accountID := newAccountingDualRunMigrationPool(t)
	downSQL := readMigrationFile(t, "000070_accounting_dual_run.down.sql")
	upSQL := readMigrationFile(t, "000070_accounting_dual_run.up.sql")
	if _, err := pool.Exec(ctx, downSQL); err != nil {
		t.Fatalf("empty migration 70 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, upSQL); err != nil {
		t.Fatalf("reapply migration 70: %v", err)
	}

	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rollbackTx.Rollback(ctx) }()
	if _, err := rollbackTx.Exec(ctx, downSQL); err != nil {
		t.Fatalf("execute migration 70 down under held transaction: %v", err)
	}
	run := accountingDualRunMigrationFixture(t, accountID)
	writerDone := make(chan error, 1)
	go func() {
		writerDone <- insertAccountingDualRun(ctx, pool, run, accountingDualRunInsertOptions{})
	}()
	select {
	case writeErr := <-writerDone:
		t.Fatalf("accounting writer completed while rollback lock held: %v", writeErr)
	case <-time.After(100 * time.Millisecond):
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case writeErr := <-writerDone:
		if writeErr != nil {
			t.Fatalf("accounting writer failed after rollback lock release: %v", writeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("accounting writer remained blocked after rollback lock release")
	}
}

type accountingDualRunInsertOptions struct {
	runID                   uuid.UUID
	legacySnapshotID        uuid.UUID
	legacyChecksum          string
	forgedResultID          bool
	forgedResultStatus      bool
	missingPayloadGenerator bool
	omitLastResult          bool
}

func newAccountingDualRunMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, uuid.UUID) {
	t.Helper()
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000070_accounting_dual_run.up.sql")); err != nil {
		t.Fatalf("apply migration 70: %v", err)
	}
	return ctx, pool, fixture.AccountID
}

func accountingDualRunMigrationFixture(t *testing.T, accountID uuid.UUID) *accountingrecon.Run {
	t.Helper()
	asOf := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	inputFor := func(source accountingrecon.SnapshotSource) accountingrecon.SnapshotInput {
		input := accountingrecon.SnapshotInput{
			Source: source, AccountID: accountID, AsOf: asOf, ObservedAt: asOf.Add(time.Second), Currency: "USD",
			ProjectionVersion: "ledger_fifo_v1", MarkSource: "test-source", MarkNamespace: "marks/test", MaxMarkAge: time.Minute,
			CaptureFenceID: "migration-fence", CaptureEpoch: 1,
			EvidenceID: source.String() + ":migration-evidence", EvidenceChecksum: strings.Repeat("a", 64),
			PositionCoverageComplete: true,
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
		Legacy: legacy, Ledger: ledgerSnapshot, Generator: "migration-test", GeneratedAt: asOf.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func insertAccountingDualRun(ctx context.Context, pool *pgxpool.Pool, run *accountingrecon.Run, options accountingDualRunInsertOptions) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	runID := run.ID
	payloadBytes := run.PayloadBytes
	checksum := run.Checksum
	if options.missingPayloadGenerator {
		var payload map[string]any
		if err := json.Unmarshal(run.PayloadBytes, &payload); err != nil {
			return err
		}
		delete(payload, "generator")
		payloadBytes, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		checksum = projectionMigrationSHA(payloadBytes)
		runID = economicid.DeterministicUUID(
			"accounting-reconciliation-run", run.Version, run.PolicyVersion,
			run.AccountID.String(), run.Legacy.Checksum, run.Ledger.Checksum, checksum,
		)
	}
	if options.runID != uuid.Nil {
		runID = options.runID
	}
	legacyID := run.Legacy.ID
	if options.legacySnapshotID != uuid.Nil {
		legacyID = options.legacySnapshotID
	}
	legacyChecksum := run.Legacy.Checksum
	if options.legacyChecksum != "" {
		legacyChecksum = options.legacyChecksum
	}
	if _, err := tx.Exec(ctx, `INSERT INTO accounting_reconciliation_runs (
		id, account_id, comparison_version, policy_version, as_of, generated_at,
		generator, projection_version, mark_source, mark_namespace,
		max_mark_age_microseconds, capture_fence_id, capture_epoch,
		legacy_snapshot_id, legacy_snapshot_checksum, legacy_snapshot_bytes,
		ledger_snapshot_id, ledger_snapshot_checksum, ledger_snapshot_bytes,
		payload, payload_bytes, checksum, result_count, equal_count,
		explained_count, unexplained_count, not_comparable_count, synthetic
	) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20::JSONB,$21,$22,$23,$24,$25,$26,$27,$28)`,
		runID, run.AccountID, run.Version, run.PolicyVersion, run.AsOf, run.GeneratedAt,
		run.Generator, run.ProjectionVersion, run.MarkSource, run.MarkNamespace,
		run.MaxMarkAge.Microseconds(), run.CaptureFenceID, run.CaptureEpoch,
		legacyID, legacyChecksum, run.Legacy.PayloadBytes,
		run.Ledger.ID, run.Ledger.Checksum, run.Ledger.PayloadBytes,
		json.RawMessage(payloadBytes), payloadBytes, checksum, len(run.Results),
		run.EqualCount, run.ExplainedCount, run.UnexplainedCount, run.NotComparableCount, run.Synthetic,
	); err != nil {
		return err
	}
	results := run.Results
	if options.omitLastResult {
		results = results[:len(results)-1]
	}
	for index, result := range results {
		resultID := result.ID
		if options.forgedResultID && index == 0 {
			resultID = uuid.New()
		}
		status := string(result.Status)
		if options.forgedResultStatus && index == 0 {
			status = string(accountingrecon.StatusUnexplained)
		}
		var explanation any
		if result.Explanation != nil {
			encoded, marshalErr := json.Marshal(result.Explanation)
			if marshalErr != nil {
				return marshalErr
			}
			explanation = json.RawMessage(encoded)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO accounting_reconciliation_results (
			id, run_id, fact_key, legacy_value, ledger_value, delta, status, reason_code, explanation
		) VALUES ($1,$2,$3,$4::NUMERIC,$5::NUMERIC,$6::NUMERIC,$7,$8,$9::JSONB)`,
			resultID, runID, result.FactKey, accountingDualRunDecimal(result.LegacyValue),
			accountingDualRunDecimal(result.LedgerValue), accountingDualRunDecimal(result.Delta),
			status, result.ReasonCode, explanation,
		); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func accountingDualRunDecimal(value *decimal.Decimal) any {
	if value == nil {
		return nil
	}
	return value.String()
}
