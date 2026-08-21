package migrations_test

import (
	"context"
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

func TestEconomicEventMigrationDefinesRawFirstExactContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000068_economic_events.up.sql"))
	for _, fragment := range []string{
		"create function economic_deterministic_uuid",
		"digest(convert_to(encoded, 'utf8'), 'sha256')",
		"create table economic_source_events",
		"raw_payload bytea not null",
		"payload_sha256 text not null",
		"payload jsonb not null",
		"unique (account_id, source, source_namespace, source_event_id)",
		"economic_deterministic_uuid( 'economic-source-event'",
		"create table option_contract_terms",
		"contract_type text not null check (contract_type in ('call', 'put'))",
		"unique (option_instrument_id, effective_at)",
		"create table economic_event_normalizations",
		"source_event_id uuid not null unique",
		"ledger_transaction_id uuid not null unique",
		"deferrable initially deferred",
		"create constraint trigger trg_economic_normalizations_semantic",
		"pg_advisory_xact_lock",
		"create trigger trg_option_contract_terms_validate",
		"create trigger trg_economic_source_events_immutable",
		"create trigger trg_option_contract_terms_immutable",
		"create trigger trg_economic_normalizations_immutable",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration 68 to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000068_economic_events.down.sql"))
	for _, fragment := range []string{
		"lock table economic_event_normalizations, economic_source_events, option_contract_terms in access exclusive mode",
		"origin_type = 'economic_source_event'",
		"cannot roll back migration 68",
		"drop table economic_event_normalizations",
		"drop table option_contract_terms",
		"drop table economic_source_events",
		"drop function economic_deterministic_uuid",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected migration 68 down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestEconomicEventMigrationDeterministicUUIDMatchesGo(t *testing.T) {
	ctx, pool, _ := newEconomicEventMigrationPool(t)
	components := []string{"account", "fëed", "event-1"}
	var databaseID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT economic_deterministic_uuid(
		'economic-source-event', $1::TEXT, $2::TEXT, $3::TEXT
	)`, components[0], components[1], components[2]).Scan(&databaseID); err != nil {
		t.Fatal(err)
	}
	want := economicid.DeterministicUUID("economic-source-event", components...)
	if databaseID != want {
		t.Fatalf("database deterministic UUID = %s, want Go UUID %s", databaseID, want)
	}
}

func TestEconomicEventMigrationCommitsRawBeforeNormalization(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "raw-first")

	var rawCount, normalizationCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM economic_source_events WHERE id = $1),
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id = $1)`, sourceID).Scan(
		&rawCount,
		&normalizationCount,
	); err != nil {
		t.Fatal(err)
	}
	if rawCount != 1 || normalizationCount != 0 {
		t.Fatalf("raw/normalization counts = %d/%d, want 1/0", rawCount, normalizationCount)
	}
}

func TestEconomicEventMigrationRejectsForgedSourceUUIDAndRevisionReuse(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	raw := []byte(`{"id":"forged"}`)
	hash := sha256.Sum256(raw)
	_, err := pool.Exec(ctx, `INSERT INTO economic_source_events (
		id, account_id, source, source_namespace, source_event_id, observed_at,
		raw_payload, payload_sha256, payload
	) VALUES ($1,$2,'simulator','fills/run-1','forged',$3,$4,$5,$6)`,
		uuid.New(),
		fixture.AccountID,
		fixture.ObservedAt,
		raw,
		hex.EncodeToString(hash[:]),
		json.RawMessage(raw),
	)
	if err == nil {
		t.Fatal("direct SQL arbitrary source UUID unexpectedly succeeded")
	}

	sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "revision-conflict")
	raw = []byte(`{"id":"revision-conflict","revision":2}`)
	hash = sha256.Sum256(raw)
	_, err = pool.Exec(ctx, `INSERT INTO economic_source_events (
		id, account_id, source, source_namespace, source_event_id, source_revision,
		observed_at, raw_payload, payload_sha256, payload
	) VALUES ($1,$2,'simulator','fills/run-1','revision-conflict','v2',$3,$4,$5,$6)`,
		sourceID,
		fixture.AccountID,
		fixture.ObservedAt,
		raw,
		hex.EncodeToString(hash[:]),
		json.RawMessage(raw),
	)
	if err == nil {
		t.Fatal("source revision reused durable economic identity")
	}
}

func TestEconomicEventMigrationAcceptsNormalizationBeforeLedgerAtCommit(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "valid-fill")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	insertMigrationFillAggregate(t, ctx, tx, fixture, sourceID, "")
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit valid normalization aggregate: %v", err)
	}
}

func TestEconomicEventMigrationRejectsWrongPostingAndRetainsRaw(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "wrong-posting")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	insertMigrationFillAggregate(t, ctx, tx, fixture, sourceID, "wrong-inventory-unit")
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("normalization with arbitrary instrument unit unexpectedly committed")
	}

	var rawCount, normalizationCount, transactionCount int
	if err := pool.QueryRow(ctx, `SELECT
		(SELECT COUNT(*) FROM economic_source_events WHERE id = $1),
		(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id = $1),
		(SELECT COUNT(*) FROM ledger_transactions WHERE origin_id = $1::TEXT)`, sourceID).Scan(
		&rawCount,
		&normalizationCount,
		&transactionCount,
	); err != nil {
		t.Fatal(err)
	}
	if rawCount != 1 || normalizationCount != 0 || transactionCount != 0 {
		t.Fatalf("raw/normalization/transaction counts = %d/%d/%d, want 1/0/0", rawCount, normalizationCount, transactionCount)
	}
}

func TestEconomicEventMigrationRejectsDirectSQLSemanticCorruption(t *testing.T) {
	for _, corruption := range []string{
		"wrong-posting-id",
		"wrong-inventory-amount",
		"wrong-currency",
		"missing-posting-pair",
		"extra-posting-pair",
		"invalid-contract-window",
		"wrong-ledger-reference",
	} {
		t.Run(corruption, func(t *testing.T) {
			ctx, pool, fixture := newEconomicEventMigrationPool(t)
			sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "direct-"+corruption)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(ctx) }()
			insertMigrationFillAggregate(t, ctx, tx, fixture, sourceID, corruption)
			if err := tx.Commit(ctx); err == nil {
				t.Fatalf("direct-SQL aggregate with %s unexpectedly committed", corruption)
			}

			var rawCount, normalizationCount, transactionCount int
			if err := pool.QueryRow(ctx, `SELECT
				(SELECT COUNT(*) FROM economic_source_events WHERE id = $1),
				(SELECT COUNT(*) FROM economic_event_normalizations WHERE source_event_id = $1),
				(SELECT COUNT(*) FROM ledger_transactions WHERE origin_id = $1::TEXT)`, sourceID).Scan(
				&rawCount,
				&normalizationCount,
				&transactionCount,
			); err != nil {
				t.Fatal(err)
			}
			if rawCount != 1 || normalizationCount != 0 || transactionCount != 0 {
				t.Fatalf("raw/normalization/transaction counts = %d/%d/%d, want 1/0/0", rawCount, normalizationCount, transactionCount)
			}
		})
	}
}

func TestEconomicEventMigrationRejectsForgedNormalizationIdentifiers(t *testing.T) {
	for _, forgedField := range []string{"normalization", "ledger-transaction"} {
		t.Run(forgedField, func(t *testing.T) {
			ctx, pool, fixture := newEconomicEventMigrationPool(t)
			sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "forged-"+forgedField)
			version := "economic_event_v1"
			normalizationID := economicid.DeterministicUUID("economic-normalization", sourceID.String(), version)
			transactionID := economicid.DeterministicUUID("economic-ledger-transaction", sourceID.String(), version)
			if forgedField == "normalization" {
				normalizationID = uuid.New()
			} else {
				transactionID = uuid.New()
			}
			_, err := pool.Exec(ctx, `INSERT INTO economic_event_normalizations (
				id, source_event_id, event_type, normalizer_version,
				execution_origin_type, execution_origin_id, reference_type, reference_id,
				venue, instrument_id, venue_contract_id, effective_at, cash_currency,
				quantity, price, ledger_transaction_id
			) VALUES ($1,$2,'fill.buy',$3,'strategy_version','strategy-version-1',
				'fill','fill-1','test-venue',$4,$5,$6,'USD',2,10.25,$7)`,
				normalizationID,
				sourceID,
				version,
				fixture.InstrumentID,
				fixture.VenueContractID,
				fixture.EffectiveAt,
				transactionID,
			)
			if err == nil {
				t.Fatalf("forged %s UUID unexpectedly passed row checks", forgedField)
			}
		})
	}
}

func TestEconomicEventMigrationRejectsMutation(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	sourceID := insertMigrationSourceEvent(t, ctx, pool, fixture, "immutable")
	for name, statement := range map[string]string{
		"update": `UPDATE economic_source_events SET source_revision = 'changed' WHERE id = '` + sourceID.String() + `'`,
		"delete": `DELETE FROM economic_source_events WHERE id = '` + sourceID.String() + `'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, statement); err == nil {
				t.Fatal("raw economic event mutation unexpectedly succeeded")
			} else if !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("mutation error = %v, want append-only rejection", err)
			}
		})
	}
}

func TestEconomicEventMigrationEmptyRollbackPreservesSchema67(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000068_economic_events.down.sql")); err != nil {
		t.Fatalf("apply empty economic-event down migration: %v", err)
	}
	var sourceTable, termsTable, normalizationTable, instrumentTable *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.economic_source_events')::TEXT,
		to_regclass(current_schema() || '.option_contract_terms')::TEXT,
		to_regclass(current_schema() || '.economic_event_normalizations')::TEXT,
		to_regclass(current_schema() || '.instruments')::TEXT`).Scan(
		&sourceTable,
		&termsTable,
		&normalizationTable,
		&instrumentTable,
	); err != nil {
		t.Fatal(err)
	}
	if sourceTable != nil || termsTable != nil || normalizationTable != nil || instrumentTable == nil {
		t.Fatalf("rollback tables = source:%v terms:%v normalization:%v instrument:%v", sourceTable, termsTable, normalizationTable, instrumentTable)
	}
	var instrumentCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM instruments WHERE id = $1`, fixture.InstrumentID).Scan(&instrumentCount); err != nil {
		t.Fatal(err)
	}
	if instrumentCount != 1 {
		t.Fatalf("schema-67 instrument count after rollback = %d, want 1", instrumentCount)
	}
}

func TestEconomicEventMigrationRefusesNonemptyRollback(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	insertMigrationSourceEvent(t, ctx, pool, fixture, "rollback-guard")
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000068_economic_events.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 68") {
		t.Fatalf("nonempty down migration error = %v, want guarded refusal", err)
	}
	var sourceTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.economic_source_events')::TEXT`).Scan(&sourceTable); err != nil {
		t.Fatal(err)
	}
	if sourceTable == nil {
		t.Fatal("guarded rollback nevertheless dropped economic_source_events")
	}
}

func TestEconomicEventMigrationExclusiveRollbackLockBlocksWriter(t *testing.T) {
	ctx, pool, fixture := newEconomicEventMigrationPool(t)
	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rollbackTx.Rollback(ctx) }()
	if _, err := rollbackTx.Exec(ctx, `LOCK TABLE
		economic_event_normalizations, economic_source_events, option_contract_terms
		IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}

	writerDone := make(chan error, 1)
	go func() {
		id := economicid.DeterministicUUID(
			"economic-source-event",
			fixture.AccountID.String(),
			"simulator",
			"fills/run-1",
			"blocked-writer",
		)
		raw := []byte(`{"id":"blocked-writer"}`)
		hash := sha256.Sum256(raw)
		_, err := pool.Exec(ctx, `INSERT INTO economic_source_events (
			id, account_id, source, source_namespace, source_event_id, observed_at,
			raw_payload, payload_sha256, payload
		) VALUES ($1,$2,'simulator','fills/run-1','blocked-writer',$3,$4,$5,$6)`,
			id,
			fixture.AccountID,
			fixture.ObservedAt,
			raw,
			hex.EncodeToString(hash[:]),
			json.RawMessage(raw),
		)
		writerDone <- err
	}()

	select {
	case err := <-writerDone:
		t.Fatalf("writer completed while rollback lock held: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("writer failed after rollback lock released: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("writer remained blocked after rollback lock released")
	}
}

type economicEventMigrationFixture struct {
	AccountID       uuid.UUID
	InstrumentID    uuid.UUID
	VenueContractID uuid.UUID
	EffectiveAt     time.Time
	ObservedAt      time.Time
}

func newEconomicEventMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, economicEventMigrationFixture) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping economic-event migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping economic-event migration integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "migr_economic_events_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create economic-event migration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop economic-event migration schema: %v", err)
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

	for _, filename := range sortedUpMigrationsThrough(t, "000068_economic_events.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("apply %s: %v", filename, err)
		}
	}

	fixture := economicEventMigrationFixture{
		AccountID:       uuid.MustParse("00000000-0000-4000-8000-000000000064"),
		InstrumentID:    uuid.New(),
		VenueContractID: uuid.New(),
		EffectiveAt:     time.Date(2026, time.August, 15, 15, 0, 0, 0, time.UTC),
		ObservedAt:      time.Date(2026, time.August, 15, 15, 0, 1, 0, time.UTC),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO instruments (
			id, identity_key, asset_class, primary_venue, currency,
			tick_size, lot_size, multiplier, settlement_method, status
		) VALUES ($1, $2, 'equity', 'test-venue', 'USD', 0.01, 1, 1, 'physical', 'active');
		INSERT INTO venue_contracts (
			id, instrument_id, venue, contract_id, currency, tick_size,
			lot_size, multiplier, settlement_method, valid_from, valid_to
		) VALUES ($3, $1, 'test-venue', 'TEST-CONTRACT', 'USD', 0.01, 1, 1,
			'physical', $4::TIMESTAMPTZ - interval '1 hour', $4::TIMESTAMPTZ + interval '1 hour')`,
		fixture.InstrumentID,
		"figi:economic:"+fixture.InstrumentID.String(),
		fixture.VenueContractID,
		fixture.EffectiveAt,
	); err != nil {
		t.Fatalf("seed economic-event fixtures: %v", err)
	}
	return ctx, pool, fixture
}

func insertMigrationSourceEvent(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture economicEventMigrationFixture,
	sourceEventID string,
) uuid.UUID {
	t.Helper()
	id := economicid.DeterministicUUID(
		"economic-source-event",
		fixture.AccountID.String(),
		"simulator",
		"fills/run-1",
		sourceEventID,
	)
	raw := []byte(`{"id":"` + sourceEventID + `"}`)
	hash := sha256.Sum256(raw)
	if _, err := pool.Exec(ctx, `INSERT INTO economic_source_events (
		id, account_id, source, source_namespace, source_event_id, source_revision,
		observed_at, raw_payload, payload_sha256, payload
	) VALUES ($1,$2,'simulator','fills/run-1',$3,'v1',$4,$5,$6,$7)`,
		id,
		fixture.AccountID,
		sourceEventID,
		fixture.ObservedAt,
		raw,
		hex.EncodeToString(hash[:]),
		json.RawMessage(raw),
	); err != nil {
		t.Fatalf("insert raw economic source event: %v", err)
	}
	return id
}

func insertMigrationFillAggregate(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	fixture economicEventMigrationFixture,
	sourceID uuid.UUID,
	corruption string,
) {
	t.Helper()
	version := "economic_event_v1"
	normalizationID := economicid.DeterministicUUID("economic-normalization", sourceID.String(), version)
	transactionID := economicid.DeterministicUUID("economic-ledger-transaction", sourceID.String(), version)
	effectiveAt := fixture.EffectiveAt
	cashCurrency := "USD"
	ledgerReferenceID := "fill-1"
	if corruption == "invalid-contract-window" {
		effectiveAt = fixture.EffectiveAt.Add(-2 * time.Hour)
	}
	if corruption == "wrong-currency" {
		cashCurrency = "EUR"
	}
	if corruption == "wrong-ledger-reference" {
		ledgerReferenceID = "fill-forged"
	}
	if _, err := tx.Exec(ctx, `INSERT INTO economic_event_normalizations (
		id, source_event_id, event_type, normalizer_version,
		execution_origin_type, execution_origin_id, reference_type, reference_id,
		venue, instrument_id, venue_contract_id, effective_at, cash_currency,
		quantity, price, ledger_transaction_id
	) VALUES ($1,$2,'fill.buy',$3,'strategy_version','strategy-version-1',
		'fill','fill-1','test-venue',$4,$5,$6,$7,2,10.25,$8)`,
		normalizationID,
		sourceID,
		version,
		fixture.InstrumentID,
		fixture.VenueContractID,
		effectiveAt,
		cashCurrency,
		transactionID,
	); err != nil {
		t.Fatalf("insert economic normalization first: %v", err)
	}
	var rawPayloadSHA256 string
	if err := tx.QueryRow(ctx, `SELECT payload_sha256 FROM economic_source_events WHERE id = $1`, sourceID).Scan(&rawPayloadSHA256); err != nil {
		t.Fatal(err)
	}
	metadata := map[string]string{
		"economic_normalization_id": normalizationID.String(),
		"execution_origin_id":       "strategy-version-1",
		"execution_origin_type":     "strategy_version",
		"normalizer_version":        version,
		"raw_payload_sha256":        rawPayloadSHA256,
		"source_event_id":           sourceID.String(),
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	instrumentUnit := fixture.InstrumentID.String()
	if corruption == "wrong-inventory-unit" {
		instrumentUnit = uuid.NewString()
	}
	postings := []struct {
		key     string
		account string
		kind    string
		unit    string
		amount  string
	}{
		{"inventory", "asset:security_inventory", "instrument", instrumentUnit, "2"},
		{"clearing-inventory", "clearing:execution", "instrument", fixture.InstrumentID.String(), "-2"},
		{"gross-cash", "asset:cash", "currency", "USD", "-20.5"},
		{"clearing-gross-cash", "clearing:execution", "currency", "USD", "20.5"},
	}
	if corruption == "wrong-inventory-amount" {
		postings[0].amount = "3"
		postings[1].amount = "-3"
	}
	if corruption == "wrong-currency" {
		postings[2].unit = cashCurrency
		postings[3].unit = cashCurrency
	}
	if corruption == "missing-posting-pair" {
		postings = postings[:2]
	}
	if corruption == "extra-posting-pair" {
		postings = append(postings,
			struct {
				key     string
				account string
				kind    string
				unit    string
				amount  string
			}{"extra-cash", "asset:cash", "currency", cashCurrency, "1"},
			struct {
				key     string
				account string
				kind    string
				unit    string
				amount  string
			}{"extra-clearing", "clearing:execution", "currency", cashCurrency, "-1"},
		)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		reference_type, reference_id, effective_at, observed_at, metadata,
		posting_count
	) VALUES ($1,$2,'fill.buy',$3,'economic_source_event',$4,'fill',$5,$6,$7,$8::JSONB,$9)`,
		transactionID,
		fixture.AccountID,
		"economic-source-event:"+sourceID.String(),
		sourceID.String(),
		ledgerReferenceID,
		effectiveAt,
		fixture.ObservedAt,
		string(metadataJSON),
		len(postings),
	); err != nil {
		t.Fatalf("insert economic ledger parent: %v", err)
	}
	for _, posting := range postings {
		postingID := economicid.DeterministicUUID("economic-ledger-posting", sourceID.String(), version, posting.key)
		if corruption == "wrong-posting-id" && posting.key == "inventory" {
			postingID = uuid.New()
		}
		if _, err := tx.Exec(ctx, `INSERT INTO ledger_postings (
			id, transaction_id, idempotency_key, ledger_account, unit_kind, unit, amount
		) VALUES ($1,$2,$3,$4,$5,$6,$7::NUMERIC)`,
			postingID,
			transactionID,
			posting.key,
			posting.account,
			posting.kind,
			posting.unit,
			posting.amount,
		); err != nil {
			t.Fatalf("insert economic ledger posting %s: %v", posting.key, err)
		}
	}
}
