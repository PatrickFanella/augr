package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCanonicalInstrumentMigrationDefinesImmutableReferenceContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000066_canonical_instruments.up.sql"))
	for _, fragment := range []string{
		"create table instruments",
		"identity_key text not null unique",
		"status text not null check",
		"create table instrument_alias_events",
		"action text not null check (action in ('assigned', 'retired'))",
		"unique (provider, alias_type, alias_value, effective_at)",
		"create table venue_contracts",
		"create table corporate_actions",
		"create table instrument_identity_quarantine",
		"create trigger trg_instruments_immutable",
		"create trigger trg_instrument_alias_events_immutable",
		"create trigger trg_instrument_alias_events_transition",
		"legacy_augr_stock",
		"migration_000066_incomplete_reference_terms",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration 66 to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000066_canonical_instruments.down.sql"))
	for _, fragment := range []string{
		"drop table if exists instrument_identity_quarantine",
		"drop table if exists corporate_actions",
		"drop table if exists venue_contracts",
		"drop table if exists instrument_alias_events",
		"drop table if exists instruments",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected migration 66 down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestCanonicalInstrumentMigrationQuarantinesLegacySymbols(t *testing.T) {
	ctx, pool := newCanonicalInstrumentMigrationPool(t)

	var instrumentCount, legacyAliasCount, incompleteFindingCount int
	var allQuarantined, allTermsUnknown bool
	if err := pool.QueryRow(ctx, `SELECT
		COUNT(*),
		COALESCE(bool_and(status = 'quarantined'), FALSE),
		COALESCE(bool_and(currency IS NULL AND tick_size IS NULL AND lot_size IS NULL AND multiplier IS NULL), FALSE)
		FROM instruments
		WHERE metadata->>'backfill' = 'migration_000066'`).Scan(
		&instrumentCount,
		&allQuarantined,
		&allTermsUnknown,
	); err != nil {
		t.Fatalf("query canonical legacy instruments: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM instrument_alias_events
		WHERE provider LIKE 'legacy_augr_%' AND action = 'assigned'`).Scan(&legacyAliasCount); err != nil {
		t.Fatalf("count legacy aliases: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM instrument_identity_quarantine
		WHERE finding_code = 'migration_000066_incomplete_reference_terms'`).Scan(&incompleteFindingCount); err != nil {
		t.Fatalf("count incomplete-reference findings: %v", err)
	}
	if instrumentCount != 4 || legacyAliasCount != 4 || incompleteFindingCount != 4 || !allQuarantined || !allTermsUnknown {
		t.Fatalf(
			"backfill = instruments:%d aliases:%d findings:%d quarantined:%t unknown:%t, want 4/4/4/true/true",
			instrumentCount,
			legacyAliasCount,
			incompleteFindingCount,
			allQuarantined,
			allTermsUnknown,
		)
	}

	var identityKey, slug string
	if err := pool.QueryRow(ctx, `SELECT identity_key, metadata->>'legacy_symbol'
		FROM instruments WHERE identity_key = 'legacy:polymarket:Will-Fed-Cut'`).Scan(&identityKey, &slug); err != nil {
		t.Fatalf("load case-sensitive polymarket identity: %v", err)
	}
	if identityKey != "legacy:polymarket:Will-Fed-Cut" || slug != "Will-Fed-Cut" {
		t.Fatalf("polymarket identity = %q/%q", identityKey, slug)
	}

	var deterministic bool
	if err := pool.QueryRow(ctx, `SELECT id = md5('legacy-instrument:stock:AAPL')::UUID
		FROM instruments WHERE identity_key = 'legacy:stock:AAPL'`).Scan(&deterministic); err != nil {
		t.Fatalf("check deterministic legacy ID: %v", err)
	}
	if !deterministic {
		t.Fatal("legacy AAPL instrument ID is not deterministic")
	}
}

func TestCanonicalInstrumentMigrationPreservesVerifiedCUSIPAlias(t *testing.T) {
	ctx, pool := newCanonicalInstrumentMigrationPool(t)

	var identityKey, action string
	if err := pool.QueryRow(ctx, `SELECT instrument.identity_key, alias.action
		FROM instrument_alias_events AS alias
		JOIN instruments AS instrument ON instrument.id = alias.instrument_id
		WHERE alias.provider = 'sec'
		  AND alias.alias_type = 'cusip'
		  AND alias.alias_value = '037833100'`).Scan(&identityKey, &action); err != nil {
		t.Fatalf("load verified CUSIP alias: %v", err)
	}
	if identityKey != "legacy:stock:AAPL" || action != "assigned" {
		t.Fatalf("verified CUSIP alias = %q/%q", identityKey, action)
	}

	var ambiguousAliasCount, ambiguousFindingCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM instrument_alias_events
		WHERE provider = 'openfigi' AND alias_value = 'BBG000B9XRY4'`).Scan(&ambiguousAliasCount); err != nil {
		t.Fatalf("count ambiguous FIGI aliases: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM instrument_identity_quarantine
		WHERE finding_code = 'migration_000066_copy_mapping_ambiguous'`).Scan(&ambiguousFindingCount); err != nil {
		t.Fatalf("count ambiguous mapping findings: %v", err)
	}
	if ambiguousAliasCount != 0 || ambiguousFindingCount != 1 {
		t.Fatalf("ambiguous mapping = aliases:%d findings:%d, want 0/1", ambiguousAliasCount, ambiguousFindingCount)
	}
}

func TestCanonicalInstrumentMigrationRejectsMutation(t *testing.T) {
	ctx, pool := newCanonicalInstrumentMigrationPool(t)
	instrumentID := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO instruments (
			id, identity_key, asset_class, primary_venue, currency,
			tick_size, lot_size, multiplier, settlement_method, status
		) VALUES ($1, $2, 'equity', 'nasdaq', 'USD', 0.01, 1, 1, 'physical', 'active');
		INSERT INTO venue_contracts (
			instrument_id, venue, contract_id, currency, tick_size, lot_size,
			multiplier, settlement_method, valid_from
		) VALUES ($1, 'alpaca', 'AAPL', 'USD', 0.01, 1, 1, 'physical', NOW());
		INSERT INTO corporate_actions (
			instrument_id, action_type, effective_at, source, source_event_id
		) VALUES ($1, 'delisting', NOW(), 'migration-test', $3)
	`, instrumentID, "figi:test:"+instrumentID.String(), "delisting:"+instrumentID.String()); err != nil {
		t.Fatalf("seed immutable canonical reference rows: %v", err)
	}

	for name, statement := range map[string]string{
		"instrument update": `UPDATE instruments SET status = 'inactive' WHERE id = '` + instrumentID.String() + `'`,
		"alias delete": `DELETE FROM instrument_alias_events
			WHERE id = (SELECT id FROM instrument_alias_events ORDER BY id LIMIT 1)`,
		"venue update": `UPDATE venue_contracts SET currency = 'EUR'
			WHERE instrument_id = '` + instrumentID.String() + `'`,
		"corporate action delete": `DELETE FROM corporate_actions
			WHERE instrument_id = '` + instrumentID.String() + `'`,
		"quarantine update": `UPDATE instrument_identity_quarantine SET source = 'mutated'
			WHERE id = (SELECT id FROM instrument_identity_quarantine ORDER BY id LIMIT 1)`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, statement); err == nil {
				t.Fatal("append-only mutation unexpectedly succeeded")
			} else if !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("mutation error = %v, want append-only rejection", err)
			}
		})
	}
}

func TestCanonicalInstrumentMigrationRejectsIncompleteActiveInstrument(t *testing.T) {
	ctx, pool := newCanonicalInstrumentMigrationPool(t)

	if _, err := pool.Exec(ctx, `INSERT INTO instruments (
		identity_key, asset_class, primary_venue, status
	) VALUES ('test:incomplete-active', 'equity', 'nasdaq', 'active')`); err == nil {
		t.Fatal("incomplete active instrument unexpectedly succeeded")
	}

	if _, err := pool.Exec(ctx, `INSERT INTO instruments (
		identity_key, asset_class, primary_venue, status, metadata
	) VALUES ('test:unproven-quarantine', 'unknown', 'legacy_unknown', 'quarantined', '{}')`); err == nil {
		t.Fatal("quarantined instrument without provenance unexpectedly succeeded")
	}

	if _, err := pool.Exec(ctx, `INSERT INTO instruments (
		identity_key, asset_class, primary_venue, currency,
		tick_size, lot_size, multiplier, settlement_method, status
	) VALUES (
		'test:incomplete-option', 'option', 'cboe', 'USD',
		0.01, 1, 100, 'physical', 'active'
	)`); err == nil {
		t.Fatal("active option without expiration, exercise style, or underlying unexpectedly succeeded")
	}
}

func TestCanonicalInstrumentDownPreservesLegacyTables(t *testing.T) {
	ctx, pool := newCanonicalInstrumentMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000066_canonical_instruments.down.sql")); err != nil {
		t.Fatalf("apply canonical-instrument down migration: %v", err)
	}

	var instrumentsTable, strategiesTable, ledgerTable, copyMappingsTable *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.instruments')::TEXT,
		to_regclass(current_schema() || '.strategies')::TEXT,
		to_regclass(current_schema() || '.ledger_transactions')::TEXT,
		to_regclass(current_schema() || '.copy_instrument_mappings')::TEXT
	`).Scan(&instrumentsTable, &strategiesTable, &ledgerTable, &copyMappingsTable); err != nil {
		t.Fatalf("inspect rollback tables: %v", err)
	}
	if instrumentsTable != nil {
		t.Fatalf("instruments remains after rollback: %q", *instrumentsTable)
	}
	if strategiesTable == nil || ledgerTable == nil || copyMappingsTable == nil {
		t.Fatalf(
			"legacy tables after rollback = strategies:%v ledger:%v copy mappings:%v",
			strategiesTable,
			ledgerTable,
			copyMappingsTable,
		)
	}

	var seededTickerCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM universe_tickers WHERE ticker = 'aapl'`).Scan(&seededTickerCount); err != nil {
		t.Fatalf("query preserved universe ticker: %v", err)
	}
	if seededTickerCount != 1 {
		t.Fatalf("preserved universe ticker count = %d, want 1", seededTickerCount)
	}
}

func newCanonicalInstrumentMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping canonical-instrument migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping canonical-instrument migration integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "migr_instrument_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create canonical-instrument migration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop canonical-instrument migration schema: %v", err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.ParseConfig() error = %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("pgxpool.NewWithConfig() error = %v", err)
	}
	t.Cleanup(pool.Close)

	for _, filename := range sortedUpMigrationsThrough(t, "000065_immutable_ledger.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("apply %s: %v", filename, err)
		}
	}
	seedCanonicalInstrumentLegacyFixtures(t, ctx, pool)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000066_canonical_instruments.up.sql")); err != nil {
		t.Fatalf("apply 000066_canonical_instruments.up.sql: %v", err)
	}
	return ctx, pool
}

func seedCanonicalInstrumentLegacyFixtures(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	observedAt := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	if _, err := pool.Exec(ctx, `
		INSERT INTO universe_tickers (ticker, name, exchange, created_at, updated_at)
		VALUES ('aapl', 'Apple Inc.', 'NASDAQ', $1, $1);
		INSERT INTO option_contracts (occ_symbol, underlying, option_type, strike, expiry, fetched_at)
		VALUES ('aapl270115c00150000', 'AAPL', 'call', 150, '2027-01-15', $1);
		INSERT INTO kalshi_watched_markets (ticker, title, added_at, updated_at)
		VALUES ('kx-fed-26', 'Federal funds target', $1, $1);
		INSERT INTO polymarket_watched_markets (slug, added_at)
		VALUES ('Will-Fed-Cut', $1);
		INSERT INTO copy_instrument_mappings (
			provider, identifier_type, identifier_value, instrument_key, ticker,
			confidence, mapping_method, valid_from, created_at, updated_at
		) VALUES
			('sec', 'cusip', '037833100', 'stock:AAPL', 'AAPL',
			 'manual_verified', 'operator', $1, $1, $1),
			('openfigi', 'figi', 'BBG000B9XRY4', 'stock:AAPL', 'AAPL',
			 'ambiguous', 'candidate', $1, $1, $1)
	`, observedAt); err != nil {
		t.Fatalf("seed canonical-instrument legacy fixtures: %v", err)
	}
}
