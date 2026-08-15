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

func TestQuoteDepthSnapshotMigrationDefinesImmutableExactContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000067_quote_depth_snapshots.up.sql"))
	for _, fragment := range []string{
		"create table quote_snapshots",
		"ingest_sequence bigint generated always as identity unique",
		"instrument_id uuid not null references instruments(id) on delete restrict",
		"venue_contract_id uuid references venue_contracts(id) on delete restrict",
		"observation_namespace text not null",
		"observation_id text not null",
		"source_revision text not null default ''",
		"available_at timestamptz",
		"bid numeric",
		"bid = round(bid, 12)",
		"bid_depth_count integer not null",
		"ask_depth_count integer not null",
		"unique (instrument_id, provider, venue, observation_namespace, observation_id, source_revision)",
		"create table quote_depth_levels",
		"unique (quote_snapshot_id, side, level_index)",
		"create trigger trg_quote_snapshots_venue_contract",
		"create constraint trigger trg_quote_snapshots_depth_consistent",
		"create trigger trg_quote_snapshots_immutable",
		"create trigger trg_quote_depth_levels_immutable",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration 67 to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000067_quote_depth_snapshots.down.sql"))
	for _, fragment := range []string{
		"drop table if exists quote_depth_levels",
		"drop table if exists quote_snapshots",
		"drop function if exists assert_quote_snapshot_depth(uuid)",
		"drop function if exists validate_quote_snapshot_venue_contract()",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected migration 67 down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestQuoteDepthSnapshotMigrationAcceptsIncompleteAttributableObservation(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	if _, err := pool.Exec(ctx, `INSERT INTO quote_snapshots (
		instrument_id, provider, venue, observation_namespace, observation_id,
		received_at, bid_depth_count, ask_depth_count
	) VALUES ($1, 'test-provider', 'test-venue', 'feed/incomplete', 'observation-1', $2, 0, 0)`,
		fixture.InstrumentID,
		fixture.ValidFrom,
	); err != nil {
		t.Fatalf("insert incomplete attributable observation: %v", err)
	}

	var source, availableAt, bid, ask *string
	if err := pool.QueryRow(ctx, `SELECT source, available_at::TEXT, bid::TEXT, ask::TEXT
		FROM quote_snapshots WHERE observation_id = 'observation-1'`).Scan(
		&source,
		&availableAt,
		&bid,
		&ask,
	); err != nil {
		t.Fatalf("load incomplete observation: %v", err)
	}
	if source != nil || availableAt != nil || bid != nil || ask != nil {
		t.Fatalf("incomplete fields = source:%v available:%v bid:%v ask:%v, want nil", source, availableAt, bid, ask)
	}
}

func TestQuoteDepthSnapshotMigrationAllowsSameObservationIdentityInDistinctScopes(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	rows := []struct {
		instrumentID uuid.UUID
		namespace    string
		revision     string
	}{
		{instrumentID: fixture.InstrumentID, namespace: "feed/a"},
		{instrumentID: fixture.InstrumentID, namespace: "feed/b"},
		{instrumentID: fixture.OtherInstrumentID, namespace: "feed/a"},
		{instrumentID: fixture.InstrumentID, namespace: "feed/a", revision: "correction-1"},
	}
	for _, row := range rows {
		if _, err := pool.Exec(ctx, `INSERT INTO quote_snapshots (
			instrument_id, provider, venue, observation_namespace, observation_id,
			source_revision, received_at, bid_depth_count, ask_depth_count
		) VALUES ($1, 'test-provider', 'test-venue', $2, 'shared-observation', $3, $4, 0, 0)`,
			row.instrumentID,
			row.namespace,
			row.revision,
			fixture.ValidFrom,
		); err != nil {
			t.Fatalf("insert scoped observation %+v: %v", row, err)
		}
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM quote_snapshots
		WHERE observation_id = 'shared-observation'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != len(rows) {
		t.Fatalf("scoped observation count = %d, want %d", count, len(rows))
	}
}

func TestQuoteDepthSnapshotMigrationValidatesVenueContractIdentityAndWindow(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	tests := []struct {
		name         string
		instrumentID uuid.UUID
		venue        string
		observedAt   time.Time
	}{
		{name: "wrong instrument", instrumentID: fixture.OtherInstrumentID, venue: "test-venue", observedAt: fixture.ValidFrom},
		{name: "wrong venue", instrumentID: fixture.InstrumentID, venue: "other-venue", observedAt: fixture.ValidFrom},
		{name: "before valid from", instrumentID: fixture.InstrumentID, venue: "test-venue", observedAt: fixture.ValidFrom.Add(-time.Microsecond)},
		{name: "at valid to", instrumentID: fixture.InstrumentID, venue: "test-venue", observedAt: fixture.ValidTo},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, `INSERT INTO quote_snapshots (
				instrument_id, venue_contract_id, provider, venue,
				observation_namespace, observation_id, exchange_at, received_at,
				bid_depth_count, ask_depth_count
			) VALUES ($1, $2, 'test-provider', $3, 'feed/contract', $4, $5, $5, 0, 0)`,
				test.instrumentID,
				fixture.VenueContractID,
				test.venue,
				strings.ReplaceAll(test.name, " ", "-"),
				test.observedAt,
			)
			if err == nil {
				t.Fatal("invalid venue contract reference unexpectedly succeeded")
			}
		})
	}
}

func TestQuoteDepthSnapshotMigrationUsesHalfOpenVenueContractWindow(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	for name, observedAt := range map[string]time.Time{
		"at-valid-from":   fixture.ValidFrom,
		"before-valid-to": fixture.ValidTo.Add(-time.Microsecond),
	} {
		if _, err := pool.Exec(ctx, `INSERT INTO quote_snapshots (
			instrument_id, venue_contract_id, provider, venue,
			observation_namespace, observation_id, exchange_at, received_at,
			bid_depth_count, ask_depth_count
		) VALUES ($1, $2, 'test-provider', 'test-venue', 'feed/window', $3, $4, $4, 0, 0)`,
			fixture.InstrumentID,
			fixture.VenueContractID,
			name,
			observedAt,
		); err != nil {
			t.Fatalf("insert quote %s: %v", name, err)
		}
	}
}

func TestQuoteDepthSnapshotMigrationRejectsCrossedQuote(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	if _, err := pool.Exec(ctx, `INSERT INTO quote_snapshots (
		instrument_id, provider, venue, observation_namespace, observation_id,
		received_at, bid, ask, bid_depth_count, ask_depth_count
	) VALUES ($1, 'test-provider', 'test-venue', 'feed/crossed', 'crossed', $2, 10.01, 10.00, 0, 0)`,
		fixture.InstrumentID,
		fixture.ValidFrom,
	); err == nil {
		t.Fatal("crossed top-of-book unexpectedly succeeded")
	}
}

func TestQuoteDepthSnapshotMigrationRejectsDecimalScaleAndMagnitudeOverflow(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	for name, value := range map[string]string{
		"scale":     "1.0000000000001",
		"magnitude": "100000000000000000000000000",
	} {
		t.Run(name, func(t *testing.T) {
			statement := `INSERT INTO quote_snapshots (
				instrument_id, provider, venue, observation_namespace, observation_id,
				received_at, bid, bid_depth_count, ask_depth_count
			) VALUES ($1, 'test-provider', 'test-venue', 'feed/numeric', $2, $3, ` + value + `, 0, 0)`
			if _, err := pool.Exec(ctx, statement, fixture.InstrumentID, name, fixture.ValidFrom); err == nil {
				t.Fatalf("raw SQL %s overflow unexpectedly succeeded", name)
			}
		})
	}
}

func TestQuoteDepthSnapshotMigrationAcceptsCompleteDepthAtCommit(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	snapshotID := uuid.New()
	if _, err := tx.Exec(ctx, `INSERT INTO quote_snapshots (
		id, instrument_id, provider, venue, observation_namespace, observation_id,
		received_at, bid, bid_size, ask, ask_size, bid_depth_count, ask_depth_count
	) VALUES ($1, $2, 'test-provider', 'test-venue', 'feed/depth', 'complete', $3,
		10.00, 2, 10.02, 3, 1, 1)`, snapshotID, fixture.InstrumentID, fixture.ValidFrom); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quote_depth_levels
		(quote_snapshot_id, side, level_index, price, size) VALUES
		($1, 'bid', 0, 10.00, 2), ($1, 'ask', 0, 10.02, 3)`, snapshotID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit complete depth: %v", err)
	}
}

func TestQuoteDepthSnapshotMigrationRejectsLateDepthInsertWithoutMutatingBook(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	snapshotID := uuid.New()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quote_snapshots (
		id, instrument_id, provider, venue, observation_namespace, observation_id,
		received_at, bid, bid_size, ask, ask_size, bid_depth_count, ask_depth_count
	) VALUES ($1, $2, 'test-provider', 'test-venue', 'feed/depth', 'late-insert', $3,
		10.00, 2, 10.02, 3, 1, 1)`, snapshotID, fixture.InstrumentID, fixture.ValidFrom); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quote_depth_levels
		(quote_snapshot_id, side, level_index, price, size) VALUES
		($1, 'bid', 0, 10.00, 2), ($1, 'ask', 0, 10.02, 3)`, snapshotID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit initial complete depth: %v", err)
	}

	noop, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := noop.Commit(ctx); err != nil {
		t.Fatalf("commit no-op control transaction: %v", err)
	}

	late, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := late.Exec(ctx, `INSERT INTO quote_depth_levels
		(quote_snapshot_id, side, level_index, price, size)
		VALUES ($1, 'bid', 1, 9.99, 1)`, snapshotID); err != nil {
		t.Fatalf("stage late depth insert: %v", err)
	}
	if err := late.Commit(ctx); err == nil {
		t.Fatal("late depth insert unexpectedly committed")
	}

	var bidCount, askCount int
	if err := pool.QueryRow(ctx, `SELECT
		COUNT(*) FILTER (WHERE side = 'bid'),
		COUNT(*) FILTER (WHERE side = 'ask')
		FROM quote_depth_levels WHERE quote_snapshot_id = $1`, snapshotID).Scan(&bidCount, &askCount); err != nil {
		t.Fatal(err)
	}
	if bidCount != 1 || askCount != 1 {
		t.Fatalf("depth after rejected late insert = bid:%d ask:%d, want 1/1", bidCount, askCount)
	}
}

func TestQuoteDepthSnapshotMigrationRejectsInvalidDepthAtCommit(t *testing.T) {
	tests := []struct {
		name     string
		bidCount int
		askCount int
		bid      string
		ask      string
		levelSQL string
	}{
		{name: "missing declared depth", bidCount: 1, askCount: 0},
		{name: "out of order bids", bidCount: 2, askCount: 0, levelSQL: `
			INSERT INTO quote_depth_levels (quote_snapshot_id, side, level_index, price, size)
			VALUES ($1, 'bid', 0, 10.00, 1), ($1, 'bid', 1, 10.01, 1)`},
		{name: "crossed depth without quote", bidCount: 1, askCount: 1, levelSQL: `
			INSERT INTO quote_depth_levels (quote_snapshot_id, side, level_index, price, size)
			VALUES ($1, 'bid', 0, 10.03, 1), ($1, 'ask', 0, 10.02, 1)`},
		{name: "top of book mismatch", bidCount: 1, askCount: 0, bid: "10.00", levelSQL: `
			INSERT INTO quote_depth_levels (quote_snapshot_id, side, level_index, price, size)
			VALUES ($1, 'bid', 0, 9.99, 1)`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
			tx, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			snapshotID := uuid.New()
			if _, err := tx.Exec(ctx, `INSERT INTO quote_snapshots (
				id, instrument_id, provider, venue, observation_namespace, observation_id,
				received_at, bid, ask, bid_depth_count, ask_depth_count
			) VALUES ($1, $2, 'test-provider', 'test-venue', 'feed/invalid-depth', $3,
				$4, NULLIF($5, '')::NUMERIC, NULLIF($6, '')::NUMERIC, $7, $8)`,
				snapshotID,
				fixture.InstrumentID,
				strings.ReplaceAll(test.name, " ", "-"),
				fixture.ValidFrom,
				test.bid,
				test.ask,
				test.bidCount,
				test.askCount,
			); err != nil {
				t.Fatal(err)
			}
			if test.levelSQL != "" {
				if _, err := tx.Exec(ctx, test.levelSQL, snapshotID); err != nil {
					t.Fatal(err)
				}
			}
			if err := tx.Commit(ctx); err == nil {
				t.Fatal("invalid depth unexpectedly committed")
			}
		})
	}
}

func TestQuoteDepthSnapshotMigrationRejectsMutation(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	snapshotID := uuid.New()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quote_snapshots (
		id, instrument_id, provider, venue, observation_namespace, observation_id,
		received_at, bid_depth_count, ask_depth_count
	) VALUES ($1, $2, 'test-provider', 'test-venue', 'feed/immutable', 'immutable', $3, 1, 0)`,
		snapshotID,
		fixture.InstrumentID,
		fixture.ValidFrom,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO quote_depth_levels
		(quote_snapshot_id, side, level_index, price, size) VALUES ($1, 'bid', 0, 10, 1)`, snapshotID); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	for name, statement := range map[string]string{
		"snapshot update": `UPDATE quote_snapshots SET source = 'mutated' WHERE id = '` + snapshotID.String() + `'`,
		"snapshot delete": `DELETE FROM quote_snapshots WHERE id = '` + snapshotID.String() + `'`,
		"depth update":    `UPDATE quote_depth_levels SET size = 2 WHERE quote_snapshot_id = '` + snapshotID.String() + `'`,
		"depth delete":    `DELETE FROM quote_depth_levels WHERE quote_snapshot_id = '` + snapshotID.String() + `'`,
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

func TestQuoteDepthSnapshotMigrationDoesNotBackfillLegacyFloatSnapshots(t *testing.T) {
	ctx, pool, _ := newQuoteDepthSnapshotMigrationPool(t)
	var legacyCount, canonicalCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM polymarket_book_snapshots
		WHERE slug = 'legacy-quote'`).Scan(&legacyCount); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM quote_snapshots`).Scan(&canonicalCount); err != nil {
		t.Fatal(err)
	}
	if legacyCount != 1 || canonicalCount != 0 {
		t.Fatalf("legacy/canonical snapshot counts = %d/%d, want 1/0", legacyCount, canonicalCount)
	}
}

func TestQuoteDepthSnapshotMigrationRollsBackWithoutTouchingSchema66(t *testing.T) {
	ctx, pool, fixture := newQuoteDepthSnapshotMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000067_quote_depth_snapshots.down.sql")); err != nil {
		t.Fatalf("apply quote-depth down migration: %v", err)
	}

	var quoteTable, depthTable, instrumentTable, legacyTable *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.quote_snapshots')::TEXT,
		to_regclass(current_schema() || '.quote_depth_levels')::TEXT,
		to_regclass(current_schema() || '.instruments')::TEXT,
		to_regclass(current_schema() || '.polymarket_book_snapshots')::TEXT
	`).Scan(&quoteTable, &depthTable, &instrumentTable, &legacyTable); err != nil {
		t.Fatal(err)
	}
	if quoteTable != nil || depthTable != nil || instrumentTable == nil || legacyTable == nil {
		t.Fatalf("rollback tables = quote:%v depth:%v instrument:%v legacy:%v", quoteTable, depthTable, instrumentTable, legacyTable)
	}
	var contractCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM venue_contracts WHERE id = $1`, fixture.VenueContractID).Scan(&contractCount); err != nil {
		t.Fatal(err)
	}
	if contractCount != 1 {
		t.Fatalf("venue contract count after rollback = %d, want 1", contractCount)
	}
}

type quoteDepthMigrationFixture struct {
	InstrumentID      uuid.UUID
	OtherInstrumentID uuid.UUID
	VenueContractID   uuid.UUID
	ValidFrom         time.Time
	ValidTo           time.Time
}

func newQuoteDepthSnapshotMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, quoteDepthMigrationFixture) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping quote-depth migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping quote-depth migration integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "migr_quote_depth_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create quote-depth migration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop quote-depth migration schema: %v", err)
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

	for _, filename := range sortedUpMigrationsThrough(t, "000066_canonical_instruments.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("apply %s: %v", filename, err)
		}
	}

	fixture := quoteDepthMigrationFixture{
		InstrumentID:      uuid.New(),
		OtherInstrumentID: uuid.New(),
		VenueContractID:   uuid.New(),
		ValidFrom:         time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC),
		ValidTo:           time.Date(2026, 8, 16, 12, 0, 0, 0, time.UTC),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO instruments (
			id, identity_key, asset_class, primary_venue, currency,
			tick_size, lot_size, multiplier, settlement_method, status
		) VALUES
			($1, $2, 'equity', 'test-venue', 'USD', 0.01, 1, 1, 'physical', 'active'),
			($3, $4, 'equity', 'test-venue', 'USD', 0.01, 1, 1, 'physical', 'active');
		INSERT INTO venue_contracts (
			id, instrument_id, venue, contract_id, currency, tick_size,
			lot_size, multiplier, settlement_method, valid_from, valid_to
		) VALUES ($5, $1, 'test-venue', 'TEST-CONTRACT', 'USD', 0.01, 1, 1, 'physical', $6, $7);
		INSERT INTO polymarket_book_snapshots (
			slug, best_bid, best_ask, bids, asks, received_at, conn_id
		) VALUES ('legacy-quote', 0.40, 0.60, '[{"price":0.40,"size":10}]'::jsonb,
			'[{"price":0.60,"size":10}]'::jsonb, $6, 1)
	`,
		fixture.InstrumentID,
		"figi:test:"+fixture.InstrumentID.String(),
		fixture.OtherInstrumentID,
		"figi:test:"+fixture.OtherInstrumentID.String(),
		fixture.VenueContractID,
		fixture.ValidFrom,
		fixture.ValidTo,
	); err != nil {
		t.Fatalf("seed quote-depth migration fixtures: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000067_quote_depth_snapshots.up.sql")); err != nil {
		t.Fatalf("apply 000067_quote_depth_snapshots.up.sql: %v", err)
	}
	return ctx, pool, fixture
}
