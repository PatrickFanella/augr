package postgres

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/ledger"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestMarkObservationRepoConvergesAndRejectsChangedEvidence(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	fixture := newEconomicLedgerFixture(t, ctx, pool, "projection-mark")
	repo := NewProjectionRepo(pools.writer, pools.attestor)
	effectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	newMark := func(revision string, metadata json.RawMessage) *ledger.MarkObservation {
		value, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
			InstrumentID: fixture.instrument.ID, Price: decimal.RequireFromString("12.25"), PriceCurrency: "USD",
			Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "mark-1",
			SourceRevision: revision, EffectiveAt: effectiveAt, ObservedAt: effectiveAt.Add(time.Second), Metadata: metadata,
		})
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	created, err := repo.RecordMarkObservation(ctx, newMark("v1", json.RawMessage(`{"quality":"official","sequence":9007199254740993}`)))
	if err != nil {
		t.Fatalf("RecordMarkObservation() error = %v", err)
	}
	replayed, err := repo.RecordMarkObservation(ctx, newMark("v1", json.RawMessage(`{"sequence":9007199254740993,"quality":"official"}`)))
	if err != nil {
		t.Fatalf("RecordMarkObservation(retry) error = %v", err)
	}
	if replayed.ID != created.ID || !replayed.Price.Equal(created.Price) {
		t.Fatalf("replayed mark = %+v, want ID/price from %+v", replayed, created)
	}
	loaded, err := repo.GetMarkObservationByID(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetMarkObservationByID() error = %v", err)
	}
	if !ledger.SameMarkObservation(created, loaded) {
		t.Fatalf("loaded mark differs: created=%+v loaded=%+v", created, loaded)
	}
	if _, err := repo.RecordMarkObservation(ctx, newMark("v2", json.RawMessage(`{"quality":"official","sequence":9007199254740993}`))); !errors.Is(err, repository.ErrIdempotencyConflict) {
		t.Fatalf("changed revision error = %v, want ErrIdempotencyConflict", err)
	}
}

func TestMarkObservationRepoConcurrentIdenticalWritesConverge(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	fixture := newEconomicLedgerFixture(t, ctx, pool, "projection-mark-concurrent")
	repo := NewProjectionRepo(pools.writer, pools.attestor)
	effectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.Zero, PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "concurrent",
		SourceRevision: "v1", EffectiveAt: effectiveAt, ObservedAt: effectiveAt.Add(time.Second), Metadata: json.RawMessage(`{"expired":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	const writers = 8
	results := make(chan *ledger.MarkObservation, writers)
	errorsFound := make(chan error, writers)
	var wait sync.WaitGroup
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			value, writeErr := repo.RecordMarkObservation(ctx, mark)
			if writeErr != nil {
				errorsFound <- writeErr
				return
			}
			results <- value
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for writeErr := range errorsFound {
		t.Errorf("RecordMarkObservation(concurrent) error = %v", writeErr)
	}
	for value := range results {
		if value.ID != mark.ID {
			t.Errorf("concurrent mark ID = %s, want %s", value.ID, mark.ID)
		}
	}
}

func TestPortfolioProjectionRepoRebuildsAndPersistsExactCheckpoint(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	fixture := newEconomicLedgerFixture(t, ctx, pool, "projection-rebuild")
	ledgerRepo := NewLedgerRepo(pool)
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization); err != nil {
		t.Fatal(err)
	}
	repo := NewProjectionRepo(pools.writer, pools.attestor)
	markEffectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.NewFromInt(12), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "projection-mark",
		SourceRevision: "v1", EffectiveAt: markEffectiveAt, ObservedAt: markEffectiveAt.Add(time.Second), Metadata: json.RawMessage(`{"quality":"official"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	request := ledger.ProjectionRequest{
		AccountID: fixture.account.ID, AsOf: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond),
		MarkSource: "test-source", MarkNamespace: "marks/repository", MaxMarkAge: 48 * time.Hour,
	}
	futureMark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.NewFromInt(99), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "future-projection-mark",
		SourceRevision: "v1", EffectiveAt: request.AsOf.Add(time.Minute), ObservedAt: request.AsOf.Add(2 * time.Minute),
		Metadata: json.RawMessage(`{"future":true}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordMarkObservation(ctx, futureMark); err != nil {
		t.Fatal(err)
	}
	first, err := repo.RebuildPortfolioProjection(ctx, request)
	if err != nil {
		t.Fatalf("RebuildPortfolioProjection() error = %v", err)
	}
	if first.TransactionCount < 2 || len(first.Lots) != 1 || len(first.Positions) != 1 {
		t.Fatalf("projection boundary/lots/positions = %d/%d/%d", first.TransactionCount, len(first.Lots), len(first.Positions))
	}
	if !first.Totals.TotalPnL.Equal(decimal.RequireFromString("3.5")) {
		t.Fatalf("projection total P&L = %s, want 3.5", first.Totals.TotalPnL)
	}
	if len(first.Marks) != 1 || !first.Marks[0].Price.Equal(decimal.NewFromInt(12)) {
		t.Fatalf("point-in-time selected marks = %+v, want only the available $12 mark", first.Marks)
	}
	checkpoint, err := repo.GetProjectionCheckpointByID(ctx, first.CheckpointID)
	if err != nil {
		t.Fatalf("GetProjectionCheckpointByID() error = %v", err)
	}
	if err := checkpoint.Validate(); err != nil {
		t.Fatalf("loaded checkpoint Validate() error = %v", err)
	}
	if checkpoint.OutputChecksum != first.OutputChecksum || !bytes.Equal(checkpoint.PayloadBytes, first.PayloadBytes) {
		t.Fatal("stored checkpoint did not preserve exact projection bytes")
	}
	replayed, err := repo.RebuildPortfolioProjection(ctx, request)
	if err != nil {
		t.Fatalf("RebuildPortfolioProjection(retry) error = %v", err)
	}
	if replayed.CheckpointID != first.CheckpointID || replayed.InputChecksum != first.InputChecksum ||
		!bytes.Equal(replayed.PayloadBytes, first.PayloadBytes) {
		t.Fatal("identical rebuild did not converge")
	}
}

func TestPortfolioProjectionRepoConcurrentIdenticalRebuildsConverge(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	fixture := newEconomicLedgerFixture(t, ctx, pool, "projection-concurrent")
	ledgerRepo := NewLedgerRepo(pool)
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization); err != nil {
		t.Fatal(err)
	}
	repo := NewProjectionRepo(pools.writer, pools.attestor)
	markEffectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.NewFromInt(12), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "concurrent-projection-mark",
		EffectiveAt: markEffectiveAt, ObservedAt: markEffectiveAt.Add(time.Second), Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	request := ledger.ProjectionRequest{
		AccountID: fixture.account.ID, AsOf: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond),
		MarkSource: "test-source", MarkNamespace: "marks/repository", MaxMarkAge: 48 * time.Hour,
	}
	const workers = 6
	results := make(chan *ledger.PortfolioProjection, workers)
	errorsFound := make(chan error, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			projection, rebuildErr := repo.RebuildPortfolioProjection(ctx, request)
			if rebuildErr != nil {
				errorsFound <- rebuildErr
				return
			}
			results <- projection
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for rebuildErr := range errorsFound {
		t.Errorf("concurrent rebuild error = %v", rebuildErr)
	}
	var expectedID uuid.UUID
	var expectedPayload []byte
	for projection := range results {
		if expectedID == uuid.Nil {
			expectedID = projection.CheckpointID
			expectedPayload = append([]byte(nil), projection.PayloadBytes...)
			continue
		}
		if projection.CheckpointID != expectedID || !bytes.Equal(projection.PayloadBytes, expectedPayload) {
			t.Errorf("concurrent projection differs: %s/%s", projection.CheckpointID, expectedID)
		}
	}
	var checkpointCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM projection_checkpoints WHERE projection_version='ledger_fifo_v1'`).Scan(&checkpointCount); err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 1 {
		t.Fatalf("canonical checkpoint count = %d, want 1", checkpointCount)
	}
}

func TestPortfolioProjectionRepoFailureLeavesEvidenceUntouched(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	fixture := newEconomicLedgerFixture(t, ctx, pool, "projection-failure")
	ledgerRepo := NewLedgerRepo(pool)
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization); err != nil {
		t.Fatal(err)
	}
	repo := NewProjectionRepo(pools.writer, pools.attestor)
	markEffectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.NewFromInt(12), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "failure-mark",
		EffectiveAt: markEffectiveAt, ObservedAt: markEffectiveAt.Add(time.Second), Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	var beforeTransactions, beforePostings, beforeMarks int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM ledger_transactions),
		(SELECT COUNT(*) FROM ledger_postings),
		(SELECT COUNT(*) FROM mark_observations)`).Scan(&beforeTransactions, &beforePostings, &beforeMarks); err != nil {
		t.Fatal(err)
	}
	_, err = repo.RebuildPortfolioProjection(ctx, ledger.ProjectionRequest{
		AccountID: fixture.account.ID, AsOf: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond),
		MarkSource: "missing-source", MarkNamespace: "marks/repository", MaxMarkAge: 48 * time.Hour,
	})
	if err == nil {
		t.Fatal("rebuild without a selected mark unexpectedly succeeded")
	}
	var afterTransactions, afterPostings, afterMarks, checkpoints int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM ledger_transactions),
		(SELECT COUNT(*) FROM ledger_postings),
		(SELECT COUNT(*) FROM mark_observations),
		(SELECT COUNT(*) FROM projection_checkpoints)`).Scan(&afterTransactions, &afterPostings, &afterMarks, &checkpoints); err != nil {
		t.Fatal(err)
	}
	if beforeTransactions != afterTransactions || beforePostings != afterPostings || beforeMarks != afterMarks || checkpoints != 0 {
		t.Fatalf("failed rebuild mutated evidence: before %d/%d/%d after %d/%d/%d checkpoints %d",
			beforeTransactions, beforePostings, beforeMarks, afterTransactions, afterPostings, afterMarks, checkpoints)
	}
}

func TestPortfolioProjectionRepoLateBackdatedInputCreatesCorrectedCheckpoint(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	pool := pools.owner
	fixture := newEconomicLedgerFixture(t, ctx, pool, "projection-late")
	ledgerRepo := NewLedgerRepo(pool)
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization); err != nil {
		t.Fatal(err)
	}
	repo := NewProjectionRepo(pools.writer, pools.attestor)
	markEffectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.NewFromInt(12), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "late-mark",
		EffectiveAt: markEffectiveAt, ObservedAt: markEffectiveAt.Add(time.Second), Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	request := ledger.ProjectionRequest{
		AccountID: fixture.account.ID, AsOf: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond),
		MarkSource: "test-source", MarkNamespace: "marks/repository", MaxMarkAge: 48 * time.Hour,
	}
	first, err := repo.RebuildPortfolioProjection(ctx, request)
	if err != nil {
		t.Fatal(err)
	}

	lateEffectiveAt := fixture.normalization.EffectiveAt.Add(-time.Minute)
	source, err := ledger.NewEconomicSourceEvent(ledger.EconomicSourceEventInput{
		AccountID: fixture.account.ID, Source: "simulator", SourceNamespace: "costs/repository",
		SourceEventID: "late-cost", SourceRevision: "v1", ObservedAt: lateEffectiveAt.Add(time.Second),
		RawPayload: json.RawMessage(`{"fee":"late"}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	lateCost, err := ledger.NewCostEconomicNormalization(ledger.CostEconomicEventInput{
		Base: ledger.EconomicNormalizationBaseInput{
			SourceEvent: source, Account: fixture.account, NormalizerVersion: "economic_event_v1",
			ExecutionOriginType: ledger.ExecutionOriginReconciliation, ExecutionOriginID: "late-reconciliation",
			ReferenceType: "cost", ReferenceID: "late-cost", EffectiveAt: lateEffectiveAt,
		},
		Kind: ledger.CostKindFee, Currency: "USD", Amount: decimal.RequireFromString("0.25"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, lateCost); err != nil {
		t.Fatal(err)
	}
	corrected, err := repo.RebuildPortfolioProjection(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if corrected.CheckpointID == first.CheckpointID || corrected.InputChecksum == first.InputChecksum {
		t.Fatal("late/backdated input did not produce a corrected checkpoint identity")
	}
	if corrected.ThroughTransactionID != first.ThroughTransactionID {
		t.Fatalf("late/backdated through ID = %s, want unchanged %s", corrected.ThroughTransactionID, first.ThroughTransactionID)
	}
	if !corrected.Totals.TotalPnL.Equal(first.Totals.TotalPnL.Sub(decimal.RequireFromString("0.25"))) {
		t.Fatalf("corrected total P&L = %s, want %s", corrected.Totals.TotalPnL, first.Totals.TotalPnL.Sub(decimal.RequireFromString("0.25")))
	}
}

func TestPortfolioProjectionRepoRejectsUnsafeOwnerWriter(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	repo := NewProjectionRepo(pools.owner, pools.attestor)
	_, err := repo.RebuildPortfolioProjection(ctx, ledger.ProjectionRequest{
		AccountID: uuid.New(), AsOf: time.Now().UTC().Truncate(time.Microsecond),
		MarkSource: "test-source", MarkNamespace: "marks/repository", MaxMarkAge: time.Hour,
	})
	if err == nil || !strings.Contains(err.Error(), "unsafe checkpoint writer privileges") {
		t.Fatalf("owner projection writer error = %v, want unsafe privilege rejection", err)
	}
}

func TestPortfolioProjectionRepoRejectsMismatchedAttestationSecret(t *testing.T) {
	ctx := context.Background()
	pools := newProjectionIntegrationPool(t, ctx)
	fixture := newEconomicLedgerFixture(t, ctx, pools.owner, "projection-attestation")
	ledgerRepo := NewLedgerRepo(pools.owner)
	if _, err := ledgerRepo.RecordEconomicSourceEvent(ctx, fixture.source); err != nil {
		t.Fatal(err)
	}
	if _, err := ledgerRepo.ApplyEconomicNormalization(ctx, fixture.normalization); err != nil {
		t.Fatal(err)
	}
	goodRepo := NewProjectionRepo(pools.writer, pools.attestor)
	markEffectiveAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	mark, err := ledger.NewMarkObservation(ledger.MarkObservationInput{
		InstrumentID: fixture.instrument.ID, Price: decimal.NewFromInt(12), PriceCurrency: "USD",
		Source: "test-source", SourceNamespace: "marks/repository", SourceObservationID: "attestation-mark",
		EffectiveAt: markEffectiveAt, ObservedAt: markEffectiveAt.Add(time.Second), Metadata: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goodRepo.RecordMarkObservation(ctx, mark); err != nil {
		t.Fatal(err)
	}
	wrongAttestor := pools.attestor
	wrongAttestor.Secret = append([]byte(nil), wrongAttestor.Secret...)
	wrongAttestor.Secret[0] ^= 0xff
	badRepo := NewProjectionRepo(pools.writer, wrongAttestor)
	_, err = badRepo.RebuildPortfolioProjection(ctx, ledger.ProjectionRequest{
		AccountID: fixture.account.ID, AsOf: time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond),
		MarkSource: "test-source", MarkNamespace: "marks/repository", MaxMarkAge: 48 * time.Hour,
	})
	if err == nil || !strings.Contains(err.Error(), "attestation HMAC") {
		t.Fatalf("mismatched attestation secret error = %v, want HMAC rejection", err)
	}
	var checkpointCount int
	if err := pools.owner.QueryRow(ctx, `SELECT COUNT(*) FROM projection_checkpoints`).Scan(&checkpointCount); err != nil {
		t.Fatal(err)
	}
	if checkpointCount != 0 {
		t.Fatalf("mismatched attestation persisted %d checkpoints, want zero", checkpointCount)
	}
}

type projectionIntegrationPools struct {
	owner    *pgxpool.Pool
	writer   *pgxpool.Pool
	attestor ProjectionCheckpointAttestor
}

func newProjectionIntegrationPool(t *testing.T, ctx context.Context) projectionIntegrationPools {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping projection repository integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping projection repository integration test: DB_URL or DATABASE_URL is not set")
	}
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	schemaName := "integration_projection_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	ownerPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	migrationDirectory := filepath.Join(filepath.Dir(filename), "..", "..", "..", "migrations")
	entries, err := os.ReadDir(migrationDirectory)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if !entry.IsDir() && strings.HasSuffix(name, ".up.sql") && name <= "000069_ledger_projections.up.sql" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		contents, err := os.ReadFile(filepath.Join(migrationDirectory, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ownerPool.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply %s: %v", name, err)
		}
	}
	signingSecret := make([]byte, 32)
	if _, err := rand.Read(signingSecret); err != nil {
		t.Fatal(err)
	}
	attestor := ProjectionCheckpointAttestor{
		KeyID:  "repository-test-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		Secret: signingSecret,
	}
	if _, err := ownerPool.Exec(ctx, `INSERT INTO projection_checkpoint_signing_keys (
		key_id, signing_secret, created_by
	) VALUES ($1,$2,'repository-test')`, attestor.KeyID, attestor.Secret); err != nil {
		t.Fatal(err)
	}

	roleName := "projection_writer_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	password := strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE ROLE `+roleIdentifier+` LOGIN PASSWORD '`+password+`'`); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT USAGE ON SCHEMA `+identifier+` TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT SELECT ON
		accounts, ledger_transactions, ledger_postings, economic_event_normalizations,
		venue_contracts, option_contract_terms, instruments, mark_observations,
		projection_checkpoints TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT INSERT ON mark_observations TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT EXECUTE ON FUNCTION persist_canonical_projection_checkpoint(BYTEA,TEXT,BYTEA) TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}

	writerConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	writerConfig.ConnConfig.User = roleName
	writerConfig.ConnConfig.Password = password
	writerConfig.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	writerConfig.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	writerPool, err := pgxpool.NewWithConfig(ctx, writerConfig)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		writerPool.Close()
		ownerPool.Close()
		if _, cleanupErr := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); cleanupErr != nil {
			t.Errorf("drop projection integration schema: %v", cleanupErr)
		}
		if _, cleanupErr := adminPool.Exec(ctx, `DROP OWNED BY `+roleIdentifier); cleanupErr != nil {
			t.Errorf("drop projection writer grants: %v", cleanupErr)
		}
		if _, cleanupErr := adminPool.Exec(ctx, `DROP ROLE `+roleIdentifier); cleanupErr != nil {
			t.Errorf("drop projection writer role: %v", cleanupErr)
		}
		adminPool.Close()
	})
	return projectionIntegrationPools{owner: ownerPool, writer: writerPool, attestor: attestor}
}
