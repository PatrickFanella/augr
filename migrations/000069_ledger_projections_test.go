package migrations_test

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestLedgerProjectionMigrationDefinesCanonicalContracts(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000069_ledger_projections.up.sql"))
	for _, fragment := range []string{
		"alter table mark_observations alter column price type numeric",
		"price >= 0",
		"add column instrument_id uuid references instruments(id)",
		"add column source_namespace text",
		"add column source_revision text",
		"create unique index idx_mark_observations_canonical_identity",
		"where instrument_id is not null",
		"create function validate_canonical_mark_observation",
		"unit_kind <> 'instrument'",
		"unit <> new.instrument_id::text",
		"source_observation_id is null",
		"economic_deterministic_uuid( 'canonical-mark-observation'",
		"add column payload_bytes bytea",
		"add column input_checksum text",
		"add column projection_version text",
		"add column as_of timestamptz",
		"add column transaction_count integer",
		"create table projection_checkpoint_signing_keys",
		"signing_secret bytea not null",
		"create table projection_checkpoint_signing_key_revocations",
		"add column attestation_key_id text",
		"add column attestation_hmac bytea",
		"create unique index idx_projection_checkpoints_canonical_identity",
		"create function validate_canonical_projection_checkpoint",
		"convert_from(new.payload_bytes, 'utf8')::jsonb",
		"digest(new.payload_bytes, 'sha256')",
		"order by effective_at desc, observed_at desc, id desc",
		"portfolio-projection-checkpoint",
		"create trigger trg_projection_checkpoints_canonical",
		"create function persist_canonical_projection_checkpoint",
		"hmac(",
		"security definer",
		"set search_path to pg_catalog",
		"pg_temp",
		"revoke all on function persist_canonical_projection_checkpoint(bytea, text, bytea) from public",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration 69 to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000069_ledger_projections.down.sql"))
	for _, fragment := range []string{
		"lock table mark_observations, projection_checkpoints, projection_checkpoint_signing_keys, projection_checkpoint_signing_key_revocations in access exclusive mode",
		"cannot roll back migration 69",
		"drop trigger if exists trg_projection_checkpoints_canonical",
		"drop function if exists persist_canonical_projection_checkpoint(bytea, text, bytea)",
		"drop table if exists projection_checkpoint_signing_key_revocations",
		"drop table if exists projection_checkpoint_signing_keys",
		"drop trigger if exists trg_mark_observations_canonical",
		"alter column price type numeric(38, 12)",
		"add constraint mark_observations_price_check check (price > 0)",
		"unique (account_id, projection_type, through_transaction_id)",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected migration 69 down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestLedgerProjectionMigrationEnforcesCheckpointWriterBoundary(t *testing.T) {
	ctx, ownerPool, fixture := newLedgerProjectionMigrationPool(t)
	input := projectionMigrationCheckpoint(t, ctx, ownerPool, fixture)
	writerPool := newLedgerProjectionWriterPool(t, ctx, ownerPool)

	if err := insertProjectionMigrationCheckpointInput(ctx, writerPool, input); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("projection writer direct INSERT error = %v, want permission denied", err)
	}
	var exposedSecret []byte
	if err := writerPool.QueryRow(ctx,
		`SELECT signing_secret FROM projection_checkpoint_signing_keys WHERE key_id=$1`,
		input.AttestationKeyID,
	).Scan(&exposedSecret); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("projection writer signing-key read error = %v, want permission denied", err)
	}

	var forgedPayload map[string]any
	if err := json.Unmarshal(input.PayloadBytes, &forgedPayload); err != nil {
		t.Fatal(err)
	}
	forgedPayload["totals"] = map[string]any{"cash": "999999"}
	forgedBytes, err := json.Marshal(forgedPayload)
	if err != nil {
		t.Fatal(err)
	}
	var forgedID uuid.UUID
	if err := writerPool.QueryRow(ctx,
		`SELECT persisted_id FROM persist_canonical_projection_checkpoint($1,$2,$3)`,
		forgedBytes,
		input.AttestationKeyID,
		input.AttestationHMAC,
	).Scan(&forgedID); err == nil || !strings.Contains(err.Error(), "attestation HMAC") {
		t.Fatalf("forged controlled checkpoint error = %v, want attestation rejection", err)
	}

	var persistedID uuid.UUID
	if err := writerPool.QueryRow(ctx,
		`SELECT persisted_id FROM persist_canonical_projection_checkpoint($1,$2,$3)`,
		input.PayloadBytes,
		input.AttestationKeyID,
		input.AttestationHMAC,
	).Scan(&persistedID); err != nil {
		t.Fatalf("controlled checkpoint persistence: %v", err)
	}
	if persistedID != input.ID {
		t.Fatalf("controlled checkpoint ID = %s, want %s", persistedID, input.ID)
	}

	if _, err := ownerPool.Exec(ctx, `INSERT INTO projection_checkpoint_signing_key_revocations (
		key_id, reason, revoked_by
	) VALUES ($1,'migration-test rotation','migration-test')`, input.AttestationKeyID); err != nil {
		t.Fatal(err)
	}
	if err := writerPool.QueryRow(ctx,
		`SELECT persisted_id FROM persist_canonical_projection_checkpoint($1,$2,$3)`,
		input.PayloadBytes,
		input.AttestationKeyID,
		input.AttestationHMAC,
	).Scan(&persistedID); err == nil || !strings.Contains(err.Error(), "unknown or revoked") {
		t.Fatalf("revoked checkpoint signer error = %v, want revocation rejection", err)
	}
}

func TestLedgerProjectionMigrationAcceptsCanonicalZeroMarkAndCheckpoint(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	markID := insertProjectionMigrationMark(t, ctx, pool, fixture, "mark-1", "0")
	if markID != economicid.DeterministicUUID(
		"canonical-mark-observation", fixture.InstrumentID.String(), "USD", "test-source", "marks/test", "mark-1",
	) {
		t.Fatalf("canonical mark ID = %s", markID)
	}

	checkpointID := insertProjectionMigrationCheckpoint(t, ctx, pool, fixture)
	var payloadBytes []byte
	var checksum string
	if err := pool.QueryRow(ctx, `SELECT payload_bytes, checksum FROM projection_checkpoints WHERE id = $1`, checkpointID).Scan(&payloadBytes, &checksum); err != nil {
		t.Fatal(err)
	}
	if checksum != projectionMigrationSHA(payloadBytes) {
		t.Fatalf("checkpoint checksum = %s, want payload SHA", checksum)
	}
}

func TestLedgerProjectionMigrationRejectsInvalidCanonicalMarks(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	tests := map[string]func() (uuid.UUID, string, string, string, string, string, string){
		"forged ID": func() (uuid.UUID, string, string, string, string, string, string) {
			return uuid.New(), "instrument", fixture.InstrumentID.String(), "1", "USD", "test-source", "mark-forged"
		},
		"negative": func() (uuid.UUID, string, string, string, string, string, string) {
			return projectionMigrationMarkID(fixture, "mark-negative"), "instrument", fixture.InstrumentID.String(), "-1", "USD", "test-source", "mark-negative"
		},
		"over precision": func() (uuid.UUID, string, string, string, string, string, string) {
			return projectionMigrationMarkID(fixture, "mark-precision"), "instrument", fixture.InstrumentID.String(), "1.0000000000001", "USD", "test-source", "mark-precision"
		},
		"wrong currency": func() (uuid.UUID, string, string, string, string, string, string) {
			return economicid.DeterministicUUID("canonical-mark-observation", fixture.InstrumentID.String(), "EUR", "test-source", "marks/test", "mark-currency"), "instrument", fixture.InstrumentID.String(), "1", "EUR", "test-source", "mark-currency"
		},
		"wrong unit kind": func() (uuid.UUID, string, string, string, string, string, string) {
			return projectionMigrationMarkID(fixture, "mark-kind"), "currency", fixture.InstrumentID.String(), "1", "USD", "test-source", "mark-kind"
		},
		"wrong unit": func() (uuid.UUID, string, string, string, string, string, string) {
			return projectionMigrationMarkID(fixture, "mark-unit"), "instrument", uuid.NewString(), "1", "USD", "test-source", "mark-unit"
		},
		"uppercase source": func() (uuid.UUID, string, string, string, string, string, string) {
			return economicid.DeterministicUUID("canonical-mark-observation", fixture.InstrumentID.String(), "USD", "TEST-SOURCE", "marks/test", "mark-source"), "instrument", fixture.InstrumentID.String(), "1", "USD", "TEST-SOURCE", "mark-source"
		},
		"missing observation": func() (uuid.UUID, string, string, string, string, string, string) {
			return projectionMigrationMarkID(fixture, "mark-missing"), "instrument", fixture.InstrumentID.String(), "1", "USD", "test-source", ""
		},
	}
	for name, values := range tests {
		name, values := name, values
		t.Run(name, func(t *testing.T) {
			id, unitKind, unit, price, currency, source, observationID := values()
			_, err := pool.Exec(ctx, `INSERT INTO mark_observations (
				id, unit_kind, unit, price, price_currency, source,
				source_observation_id, effective_at, observed_at, metadata,
				instrument_id, source_namespace, source_revision
			) VALUES ($1,$2,$3,$4::NUMERIC,$5,$6,NULLIF($7,''),$8,$9,'{}',$10,'marks/test','v1')`,
				id, unitKind, unit, price, currency, source, observationID,
				fixture.MarkEffectiveAt, fixture.MarkEffectiveAt.Add(time.Second), fixture.InstrumentID,
			)
			if err == nil {
				t.Fatal("invalid canonical mark unexpectedly succeeded")
			}
		})
	}
}

func TestLedgerProjectionMigrationCheckpointRecomputesBoundaryAndBytes(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	flowID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO capital_flows (
		id, account_id, flow_type, amount, currency, idempotency_key, source,
		metadata, effective_at, observed_at
	) VALUES ($1,$2,'deposit',1,'USD',$3,'operator','{}',$4,$5)`,
		flowID, fixture.AccountID, "projection-boundary:"+flowID.String(), fixture.AsOf.Add(-10*time.Second), fixture.AsOf.Add(-9*time.Second),
	); err != nil {
		t.Fatal(err)
	}
	var earlierThrough uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM ledger_transactions
		WHERE account_id=$1 AND effective_at <= $2 AND observed_at <= $2
		ORDER BY effective_at, observed_at, id LIMIT 1`, fixture.AccountID, fixture.AsOf).Scan(&earlierThrough); err != nil {
		t.Fatal(err)
	}
	valid := projectionMigrationCheckpoint(t, ctx, pool, fixture)
	tests := map[string]func(*projectionMigrationCheckpointInput){
		"earlier through": func(input *projectionMigrationCheckpointInput) { input.ThroughTransactionID = earlierThrough },
		"wrong count":     func(input *projectionMigrationCheckpointInput) { input.TransactionCount++ },
		"forged ID":       func(input *projectionMigrationCheckpointInput) { input.ID = uuid.New() },
		"wrong checksum":  func(input *projectionMigrationCheckpointInput) { input.Checksum = strings.Repeat("f", 64) },
		"wrong bytes":     func(input *projectionMigrationCheckpointInput) { input.PayloadBytes = []byte(`{"changed":true}`) },
		"coherent forged payload": func(input *projectionMigrationCheckpointInput) {
			input.PayloadBytes = []byte(`{}`)
			input.Checksum = projectionMigrationSHA(input.PayloadBytes)
		},
	}
	for name, mutate := range tests {
		name, mutate := name, mutate
		t.Run(name, func(t *testing.T) {
			input := valid
			input.PayloadBytes = append([]byte(nil), valid.PayloadBytes...)
			mutate(&input)
			if err := insertProjectionMigrationCheckpointInput(ctx, pool, input); err == nil {
				t.Fatal("invalid checkpoint unexpectedly succeeded")
			}
		})
	}
}

func TestLedgerProjectionMigrationRejectsZeroEventCheckpoint(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	accountID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO accounts (
		id, name, environment, venue, base_currency, storage_namespace,
		evidence_class, starting_capital, buying_power_multiplier,
		margin_profile, status, created_by, creation_metadata
	) VALUES ($1,'Empty projection account','paper_scored','internal','USD',$2,
		'promotion_evidence',100,1,'cash','active','migration-test','{}')`,
		accountID, "paper_scored/empty-projection-"+accountID.String(),
	); err != nil {
		t.Fatal(err)
	}
	input := projectionMigrationCheckpoint(t, ctx, pool, fixture)
	input.AccountID = accountID
	input.TransactionCount = 1
	input.ID = economicid.DeterministicUUID(
		"portfolio-projection-checkpoint", accountID.String(), "portfolio", "ledger_fifo_v1",
		input.AsOf.Format("2006-01-02T15:04:05.000000Z"), input.InputChecksum,
	)
	payload := map[string]any{
		"checkpoint_id": input.ID.String(), "projection_type": "portfolio", "version": "ledger_fifo_v1", "fifo": "fifo",
		"account_id": accountID.String(), "base_currency": "USD", "as_of": input.AsOf.Format("2006-01-02T15:04:05.000000Z"),
		"mark_source": "test-source", "mark_namespace": "marks/test", "max_mark_age_microseconds": int64(time.Hour / time.Microsecond),
		"through_transaction_id": input.ThroughTransactionID.String(), "transaction_count": 1, "input_checksum": input.InputChecksum,
		"marks": []any{}, "lots": []any{}, "matches": []any{}, "positions": []any{}, "totals": map[string]any{},
	}
	var err error
	input.PayloadBytes, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	input.Checksum = projectionMigrationSHA(input.PayloadBytes)
	input.AttestationHMAC = projectionMigrationHMAC(input.AttestationKeyID, fixture.SigningSecret, input.PayloadBytes)
	if err := insertProjectionMigrationCheckpointInput(ctx, pool, input); err == nil || !strings.Contains(err.Error(), "requires at least one eligible") {
		t.Fatalf("zero-event checkpoint error = %v", err)
	}
}

func TestLedgerProjectionMigrationEmptyRollbackAndCanonicalDataGuard(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.down.sql")); err != nil {
		t.Fatalf("empty migration 69 rollback: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.up.sql")); err != nil {
		t.Fatalf("reapply migration 69: %v", err)
	}
	insertProjectionMigrationMark(t, ctx, pool, fixture, "guard", "1")
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.down.sql")); err == nil || !strings.Contains(err.Error(), "cannot roll back migration 69") {
		t.Fatalf("canonical-data rollback error = %v", err)
	}
}

func TestLedgerProjectionMigrationPreservesLegacyNullRowsAcrossUpAndDown(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.down.sql")); err != nil {
		t.Fatal(err)
	}
	legacyID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO mark_observations (
		id, unit_kind, unit, price, price_currency, source,
		source_observation_id, effective_at, observed_at, metadata
	) VALUES ($1,'instrument',$2,1,'USD','Legacy Source','legacy-1',$3,$4,'{}')`,
		legacyID, fixture.InstrumentID.String(), fixture.MarkEffectiveAt, fixture.MarkEffectiveAt.Add(time.Second),
	); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.up.sql")); err != nil {
		t.Fatal(err)
	}
	var instrumentID, namespace, revision *string
	if err := pool.QueryRow(ctx, `SELECT instrument_id::TEXT, source_namespace, source_revision
		FROM mark_observations WHERE id=$1`, legacyID).Scan(&instrumentID, &namespace, &revision); err != nil {
		t.Fatal(err)
	}
	if instrumentID != nil || namespace != nil || revision != nil {
		t.Fatalf("legacy canonical fields = %v/%v/%v, want all null", instrumentID, namespace, revision)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.down.sql")); err != nil {
		t.Fatalf("rollback with legacy-only rows: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM mark_observations WHERE id=$1`, legacyID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("legacy mark count after down = %d, want 1", count)
	}
}

func TestLedgerProjectionMigrationRollbackLockBlocksCanonicalWriter(t *testing.T) {
	ctx, pool, fixture := newLedgerProjectionMigrationPool(t)
	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rollbackTx.Rollback(ctx) }()
	if _, err := rollbackTx.Exec(ctx, readMigrationFile(t, "000069_ledger_projections.down.sql")); err != nil {
		t.Fatalf("execute migration 69 down under held transaction: %v", err)
	}
	writerDone := make(chan error, 1)
	go func() {
		id := projectionMigrationMarkID(fixture, "blocked-writer")
		_, writeErr := pool.Exec(ctx, `INSERT INTO mark_observations (
			id, unit_kind, unit, price, price_currency, source,
			source_observation_id, effective_at, observed_at, metadata,
			instrument_id, source_namespace, source_revision
		) VALUES ($1,'instrument',$2,1,'USD','test-source','blocked-writer',$3,$4,'{}',$5,'marks/test','v1')`,
			id, fixture.InstrumentID.String(), fixture.MarkEffectiveAt, fixture.MarkEffectiveAt.Add(time.Second), fixture.InstrumentID,
		)
		writerDone <- writeErr
	}()
	select {
	case writeErr := <-writerDone:
		t.Fatalf("canonical writer completed while rollback lock held: %v", writeErr)
	case <-time.After(100 * time.Millisecond):
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case writeErr := <-writerDone:
		if writeErr != nil {
			t.Fatalf("canonical writer failed after rollback lock release: %v", writeErr)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("canonical writer remained blocked after rollback lock release")
	}
}

type ledgerProjectionMigrationFixture struct {
	AccountID       uuid.UUID
	InstrumentID    uuid.UUID
	MarkEffectiveAt time.Time
	AsOf            time.Time
	SigningKeyID    string
	SigningSecret   []byte
}

func newLedgerProjectionMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, ledgerProjectionMigrationFixture) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping ledger-projection migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping ledger-projection migration integration test: DB_URL or DATABASE_URL is not set")
	}
	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)
	schemaName := "migr_ledger_projection_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop ledger-projection schema: %v", err)
		}
	})
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	for _, filename := range sortedUpMigrationsThrough(t, "000069_ledger_projections.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("apply %s: %v", filename, err)
		}
	}
	signingSecret := make([]byte, 32)
	if _, err := rand.Read(signingSecret); err != nil {
		t.Fatal(err)
	}
	fixture := ledgerProjectionMigrationFixture{
		AccountID: uuid.MustParse("00000000-0000-4000-8000-000000000064"), InstrumentID: uuid.New(),
		MarkEffectiveAt: time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond),
		AsOf:            time.Now().UTC().Add(time.Minute).Truncate(time.Microsecond),
		SigningKeyID:    "migration-test-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		SigningSecret:   signingSecret,
	}
	if _, err := pool.Exec(ctx, `INSERT INTO instruments (
		id, identity_key, asset_class, primary_venue, currency,
		tick_size, lot_size, multiplier, settlement_method, status
	) VALUES ($1,$2,'equity','test-venue','USD',0.01,1,1,'physical','active')`,
		fixture.InstrumentID, "figi:projection:"+fixture.InstrumentID.String(),
	); err != nil {
		t.Fatal(err)
	}
	return ctx, pool, fixture
}

func insertProjectionMigrationMark(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ledgerProjectionMigrationFixture, observationID, price string) uuid.UUID {
	t.Helper()
	id := projectionMigrationMarkID(fixture, observationID)
	if _, err := pool.Exec(ctx, `INSERT INTO mark_observations (
		id, unit_kind, unit, price, price_currency, source,
		source_observation_id, effective_at, observed_at, metadata,
		instrument_id, source_namespace, source_revision
	) VALUES ($1,'instrument',$2,$3::NUMERIC,'USD','test-source',$4,$5,$6,'{}',$7,'marks/test','v1')`,
		id, fixture.InstrumentID.String(), price, observationID,
		fixture.MarkEffectiveAt, fixture.MarkEffectiveAt.Add(time.Second), fixture.InstrumentID,
	); err != nil {
		t.Fatalf("insert canonical mark: %v", err)
	}
	return id
}

func projectionMigrationMarkID(fixture ledgerProjectionMigrationFixture, observationID string) uuid.UUID {
	return economicid.DeterministicUUID(
		"canonical-mark-observation", fixture.InstrumentID.String(), "USD", "test-source", "marks/test", observationID,
	)
}

type projectionMigrationCheckpointInput struct {
	ID                   uuid.UUID
	AccountID            uuid.UUID
	ThroughTransactionID uuid.UUID
	AsOf                 time.Time
	TransactionCount     int
	InputChecksum        string
	PayloadBytes         []byte
	Checksum             string
	AttestationKeyID     string
	AttestationHMAC      []byte
}

func projectionMigrationCheckpoint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ledgerProjectionMigrationFixture) projectionMigrationCheckpointInput {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO projection_checkpoint_signing_keys (
		key_id, signing_secret, created_by
	) VALUES ($1,$2,'migration-test') ON CONFLICT (key_id) DO NOTHING`,
		fixture.SigningKeyID, fixture.SigningSecret,
	); err != nil {
		t.Fatal(err)
	}
	var transactionCount int
	var through uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions
		WHERE account_id=$1 AND effective_at <= $2 AND observed_at <= $2`, fixture.AccountID, fixture.AsOf).Scan(&transactionCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT id FROM ledger_transactions
		WHERE account_id=$1 AND effective_at <= $2 AND observed_at <= $2
		ORDER BY effective_at DESC, observed_at DESC, id DESC LIMIT 1`, fixture.AccountID, fixture.AsOf).Scan(&through); err != nil {
		t.Fatal(err)
	}
	inputChecksum := strings.Repeat("a", 64)
	id := economicid.DeterministicUUID(
		"portfolio-projection-checkpoint", fixture.AccountID.String(), "portfolio", "ledger_fifo_v1",
		fixture.AsOf.Format("2006-01-02T15:04:05.000000Z"), inputChecksum,
	)
	payload, err := json.Marshal(map[string]any{
		"checkpoint_id": id.String(), "projection_type": "portfolio", "version": "ledger_fifo_v1", "fifo": "fifo",
		"account_id": fixture.AccountID.String(), "base_currency": "USD", "as_of": fixture.AsOf.Format("2006-01-02T15:04:05.000000Z"),
		"mark_source": "test-source", "mark_namespace": "marks/test", "max_mark_age_microseconds": int64(time.Hour / time.Microsecond),
		"through_transaction_id": through.String(), "transaction_count": transactionCount, "input_checksum": inputChecksum,
		"marks": []any{}, "lots": []any{}, "matches": []any{}, "positions": []any{}, "totals": map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	return projectionMigrationCheckpointInput{
		ID: id, AccountID: fixture.AccountID, ThroughTransactionID: through, AsOf: fixture.AsOf,
		TransactionCount: transactionCount, InputChecksum: inputChecksum, PayloadBytes: payload, Checksum: projectionMigrationSHA(payload),
		AttestationKeyID: fixture.SigningKeyID,
		AttestationHMAC:  projectionMigrationHMAC(fixture.SigningKeyID, fixture.SigningSecret, payload),
	}
}

func insertProjectionMigrationCheckpoint(t *testing.T, ctx context.Context, pool *pgxpool.Pool, fixture ledgerProjectionMigrationFixture) uuid.UUID {
	t.Helper()
	input := projectionMigrationCheckpoint(t, ctx, pool, fixture)
	if err := insertProjectionMigrationCheckpointInput(ctx, pool, input); err != nil {
		t.Fatalf("insert canonical checkpoint: %v", err)
	}
	return input.ID
}

func insertProjectionMigrationCheckpointInput(ctx context.Context, pool *pgxpool.Pool, input projectionMigrationCheckpointInput) error {
	_, err := pool.Exec(ctx, `INSERT INTO projection_checkpoints (
		id, account_id, projection_type, through_transaction_id, payload, checksum,
		projection_version, as_of, fifo_method, base_currency,
		mark_source, mark_namespace, max_mark_age_microseconds,
		transaction_count, mark_count, lot_count, match_count, position_count,
		input_checksum, payload_bytes, attestation_key_id, attestation_hmac
	) VALUES ($1,$2,'portfolio',$3,$4::JSONB,$5,'ledger_fifo_v1',$6,'fifo','USD',
		'test-source','marks/test',$7,$8,0,0,0,0,$9,$10,$11,$12)`,
		input.ID, input.AccountID, input.ThroughTransactionID, string(input.PayloadBytes), input.Checksum,
		input.AsOf, int64(time.Hour/time.Microsecond), input.TransactionCount, input.InputChecksum, input.PayloadBytes,
		input.AttestationKeyID, input.AttestationHMAC,
	)
	return err
}

func projectionMigrationSHA(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}

func projectionMigrationHMAC(keyID string, secret, payload []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte("augr-projection-checkpoint-hmac-v1"))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(keyID))
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write(payload)
	return mac.Sum(nil)
}

func newLedgerProjectionWriterPool(t *testing.T, ctx context.Context, ownerPool *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()
	var schemaName string
	if err := ownerPool.QueryRow(ctx, `SELECT current_schema()`).Scan(&schemaName); err != nil {
		t.Fatal(err)
	}
	roleName := "projection_writer_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	roleIdentifier := pgx.Identifier{roleName}.Sanitize()
	password := strings.ReplaceAll(uuid.NewString(), "-", "")
	if _, err := ownerPool.Exec(ctx, `CREATE ROLE `+roleIdentifier+` LOGIN PASSWORD '`+password+`'`); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT USAGE ON SCHEMA `+pgx.Identifier{schemaName}.Sanitize()+` TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT SELECT ON projection_checkpoints TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}
	if _, err := ownerPool.Exec(ctx, `GRANT EXECUTE ON FUNCTION persist_canonical_projection_checkpoint(BYTEA,TEXT,BYTEA) TO `+roleIdentifier); err != nil {
		t.Fatal(err)
	}

	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.User = roleName
	config.ConnConfig.Password = password
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	writerPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		writerPool.Close()
		if _, cleanupErr := ownerPool.Exec(ctx, `DROP OWNED BY `+roleIdentifier); cleanupErr != nil {
			t.Errorf("drop projection writer grants: %v", cleanupErr)
		}
		if _, cleanupErr := ownerPool.Exec(ctx, `DROP ROLE `+roleIdentifier); cleanupErr != nil {
			t.Errorf("drop projection writer role: %v", cleanupErr)
		}
	})
	return writerPool
}
