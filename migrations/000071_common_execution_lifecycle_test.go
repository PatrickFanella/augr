package migrations_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/economicid"
)

func TestCommonExecutionLifecycleMigrationDefinesImmutableGraph(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000071_common_execution_lifecycle.up.sql"))
	for _, fragment := range []string{
		"create table execution_intents",
		"create table execution_orders",
		"create table execution_order_bindings",
		"create table execution_fills",
		"create table execution_lifecycle_events",
		"ingest_sequence bigint generated always as identity unique",
		"unique (account_id, idempotency_key)",
		"unique (intent_id)",
		"unique (account_id, venue, external_order_id)",
		"economic_source_event_id uuid not null unique",
		"normalization_id uuid not null unique",
		"ledger_transaction_id uuid not null unique",
		"evidence_bytes bytea not null",
		"evidence_sha256 text not null",
		"observation_class text not null check (observation_class in ('ordinary', 'correction', 'bust'))",
		"check ((kind in ('fill_acknowledged', 'fill_recorded')) = (cumulative_fill_quantity is not null))",
		"create unique index idx_execution_lifecycle_events_ordinary_source",
		"create unique index idx_execution_lifecycle_events_revision_source",
		"case when observation_class = 'ordinary' then source_event_id else original_source_event_id end",
		"source_namespace, original_source_event_id, observation_discriminator",
		"create index idx_execution_lifecycle_recovery",
		"economic_deterministic_uuid( 'execution-intent'",
		"economic_deterministic_uuid( 'execution-order'",
		"economic_deterministic_uuid( 'execution-order-binding'",
		"economic_deterministic_uuid( 'execution-fill'",
		"economic_deterministic_uuid( 'execution-lifecycle-event'",
		"create function execution_lifecycle_transition_is_valid",
		"create function validate_execution_intent",
		"create function validate_execution_order",
		"create function validate_execution_order_binding",
		"create function validate_execution_fill",
		"create function validate_execution_lifecycle_event",
		"for update",
		"create function assert_execution_lifecycle_graph",
		"create constraint trigger trg_execution_intents_complete",
		"create constraint trigger trg_execution_orders_complete",
		"create constraint trigger trg_execution_order_bindings_complete",
		"create constraint trigger trg_execution_fills_complete",
		"create constraint trigger trg_execution_lifecycle_events_complete",
		"create constraint trigger trg_economic_normalizations_execution_fill_complete",
		"deferrable initially deferred",
		"create function reject_execution_lifecycle_mutation",
		"create trigger trg_execution_intents_immutable",
		"create trigger trg_execution_orders_immutable",
		"create trigger trg_execution_order_bindings_immutable",
		"create trigger trg_execution_fills_immutable",
		"create trigger trg_execution_lifecycle_events_immutable",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Errorf("migration 71 is missing %q", fragment)
		}
	}
	for _, forbidden := range []string{
		"insert into orders",
		"insert into trades",
		"insert into positions",
		"grant insert",
		"grant update",
		"grant delete",
	} {
		if strings.Contains(upSQL, forbidden) {
			t.Errorf("migration 71 contains forbidden activation or legacy mutation %q", forbidden)
		}
	}
}

func TestCommonExecutionLifecycleMigrationDefinesEmptyOnlyRollback(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000071_common_execution_lifecycle.down.sql"))
	for _, fragment := range []string{
		"lock table economic_event_normalizations, execution_lifecycle_events, execution_fills, execution_order_bindings, execution_orders, execution_intents in access exclusive mode",
		"cannot roll back migration 71 while common execution lifecycle evidence exists",
		"drop trigger if exists trg_economic_normalizations_execution_fill_complete on economic_event_normalizations",
		"drop table execution_lifecycle_events",
		"drop table execution_fills",
		"drop table execution_order_bindings",
		"drop table execution_orders",
		"drop table execution_intents",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Errorf("migration 71 rollback is missing %q", fragment)
		}
	}
	if strings.Contains(downSQL, "drop table economic_event_normalizations") {
		t.Error("migration 71 rollback must preserve schema-68 normalizations")
	}
}

func TestCommonExecutionLifecycleMigrationAppliesAndEmptyRollbackPreservesSchema70(t *testing.T) {
	ctx, pool, _ := newAccountingDualRunMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000071_common_execution_lifecycle.up.sql")); err != nil {
		t.Fatalf("apply migration 71: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000071_common_execution_lifecycle.down.sql")); err != nil {
		t.Fatalf("empty rollback migration 71: %v", err)
	}

	var intentTable, normalizationTable, reconciliationTable *string
	if err := pool.QueryRow(ctx, `SELECT
		to_regclass(current_schema() || '.execution_intents')::TEXT,
		to_regclass(current_schema() || '.economic_event_normalizations')::TEXT,
		to_regclass(current_schema() || '.accounting_reconciliation_runs')::TEXT
	`).Scan(&intentTable, &normalizationTable, &reconciliationTable); err != nil {
		t.Fatal(err)
	}
	if intentTable != nil || normalizationTable == nil || reconciliationTable == nil {
		t.Fatalf("rollback tables = intent:%v normalization:%v reconciliation:%v", intentTable, normalizationTable, reconciliationTable)
	}
}

func TestCommonExecutionLifecycleMigrationMatchesGoIdentityAndRequiresCompleteProposal(t *testing.T) {
	ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
	for _, identity := range []struct {
		domain     string
		components []string
	}{
		{domain: "execution-intent", components: []string{fixture.AccountID.String(), "intent-key"}},
		{domain: "execution-order", components: []string{uuid.NewString(), "order-key"}},
		{domain: "execution-order-binding", components: []string{uuid.NewString()}},
		{domain: "execution-fill", components: []string{uuid.NewString(), uuid.NewString()}},
		{domain: "execution-lifecycle-event", components: []string{uuid.NewString(), "ordinary", "test", "lifecycle/test", "event-1", ""}},
	} {
		var databaseID uuid.UUID
		if err := pool.QueryRow(ctx, `SELECT economic_deterministic_uuid($1, VARIADIC $2::TEXT[])`, identity.domain, identity.components).Scan(&databaseID); err != nil {
			t.Fatal(err)
		}
		if want := economicid.DeterministicUUID(identity.domain, identity.components...); databaseID != want {
			t.Fatalf("database %s ID = %s, want Go %s", identity.domain, databaseID, want)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	intentID := insertMigrationLifecycleIntent(t, ctx, tx, fixture, "incomplete")
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("intent without proposed event unexpectedly committed")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM execution_intents WHERE id = $1`, intentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("incomplete intent count = %d, want rollback", count)
	}
}

func TestCommonExecutionLifecycleMigrationSerializesTransitionsAndRejectsMutation(t *testing.T) {
	ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
	intentID, proposalID := insertMigrationLifecycleProposal(t, ctx, pool, fixture, "serialized")

	for name, statement := range map[string]string{
		"intent update": `UPDATE execution_intents SET metadata = '{"changed":true}' WHERE id = '` + intentID.String() + `'`,
		"intent delete": `DELETE FROM execution_intents WHERE id = '` + intentID.String() + `'`,
		"event update":  `UPDATE execution_lifecycle_events SET reason = 'changed' WHERE id = '` + proposalID.String() + `'`,
		"event delete":  `DELETE FROM execution_lifecycle_events WHERE id = '` + proposalID.String() + `'`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, statement); err == nil || !strings.Contains(err.Error(), "append-only") {
				t.Fatalf("mutation error = %v, want append-only rejection", err)
			}
		})
	}

	allocationEvidence := []byte(`{"quantity":"5"}`)
	allocationID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "allocator",
		"lifecycle/test", "allocation-stale", "",
	)
	if _, err := pool.Exec(ctx, migrationLifecycleEventSQL,
		allocationID, intentID, "intent_allocated", "allocated", "allocated",
		fixture.AccountID, "paper_scored", "strategy_version", "strategy-version-1", "strategy-version-1",
		"5", fixture.DecisionAt.Add(time.Second), "allocator", "lifecycle/test", "allocation-stale",
		"allocator", "allocated", allocationEvidence, migrationLifecycleSHA(allocationEvidence),
	); err == nil || !strings.Contains(err.Error(), "stale or illegal") {
		t.Fatalf("stale transition error = %v, want serialized rejection", err)
	}

	wrongContextID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "allocator",
		"lifecycle/test", "allocation-context", "",
	)
	if _, err := pool.Exec(ctx, migrationLifecycleEventSQL,
		wrongContextID, intentID, "intent_allocated", "proposed", "allocated",
		fixture.AccountID, "shadow", "strategy_version", "strategy-version-1", "strategy-version-1",
		"5", fixture.DecisionAt.Add(time.Second), "allocator", "lifecycle/test", "allocation-context",
		"allocator", "allocated", allocationEvidence, migrationLifecycleSHA(allocationEvidence),
	); err == nil || !strings.Contains(err.Error(), "context differs") {
		t.Fatalf("context transition error = %v, want context rejection", err)
	}
}

func TestCommonExecutionLifecycleMigrationConcurrentDuplicateEventConverges(t *testing.T) {
	ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
	intentID, _ := insertMigrationLifecycleProposal(t, ctx, pool, fixture, "concurrent-event")
	evidence := []byte(`{"quantity":"5"}`)
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "allocator",
		"lifecycle/test", "allocation-concurrent", "",
	)

	const writers = 8
	errorsFound := make(chan error, writers)
	var wait sync.WaitGroup
	var ready sync.WaitGroup
	ready.Add(writers)
	start := make(chan struct{})
	for range writers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			ready.Done()
			<-start
			_, err := pool.Exec(ctx, migrationLifecycleEventSQL+` ON CONFLICT (id) DO NOTHING`,
				eventID, intentID, "intent_allocated", "proposed", "allocated",
				fixture.AccountID, "paper_scored", "strategy_version", "strategy-version-1", "strategy-version-1",
				"5", fixture.DecisionAt.Add(time.Second), "allocator", "lifecycle/test", "allocation-concurrent",
				"allocator", "allocated", evidence, migrationLifecycleSHA(evidence),
			)
			if err != nil {
				errorsFound <- err
			}
		}()
	}
	ready.Wait()
	close(start)
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent duplicate event error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM execution_lifecycle_events
		WHERE intent_id = $1 AND kind = 'intent_allocated'`, intentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("concurrent allocation count = %d, want 1", count)
	}
}

func TestCommonExecutionLifecycleMigrationRejectsCumulativeQuantityOnNonFillEvent(t *testing.T) {
	ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
	intentID, _ := insertMigrationLifecycleProposal(t, ctx, pool, fixture, "forged-cumulative")
	evidence := []byte(`{"quantity":"5"}`)
	eventID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "allocator",
		"lifecycle/test", "allocation-forged-cumulative", "",
	)
	if _, err := pool.Exec(ctx, migrationLifecycleEventWithCumulativeSQL,
		eventID, intentID, "intent_allocated", "proposed", "allocated",
		fixture.AccountID, "paper_scored", "strategy_version", "strategy-version-1", "strategy-version-1",
		"5", "5", fixture.DecisionAt.Add(time.Second), "allocator", "lifecycle/test", "allocation-forged-cumulative",
		"allocator", "allocated", evidence, migrationLifecycleSHA(evidence),
	); err == nil {
		t.Fatal("direct allocation insert with cumulative fill quantity unexpectedly succeeded")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM execution_lifecycle_events WHERE intent_id = $1`, intentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("event count after rejected cumulative quantity = %d, want proposal only", count)
	}
}

func TestCommonExecutionLifecycleMigrationNonemptyRollbackRefuses(t *testing.T) {
	ctx, pool, fixture := newCommonExecutionLifecycleMigrationPool(t)
	intentID, _ := insertMigrationLifecycleProposal(t, ctx, pool, fixture, "rollback-refuses")
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000071_common_execution_lifecycle.down.sql")); err == nil ||
		!strings.Contains(err.Error(), "cannot roll back migration 71") {
		t.Fatalf("nonempty rollback error = %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM execution_intents WHERE id = $1`, intentID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("rollback refusal preserved intent count = %d, want 1", count)
	}
}

type commonExecutionLifecycleMigrationFixture struct {
	AccountID       uuid.UUID
	InstrumentID    uuid.UUID
	VenueContractID uuid.UUID
	QuoteSnapshotID uuid.UUID
	DecisionAt      time.Time
}

func newCommonExecutionLifecycleMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool, commonExecutionLifecycleMigrationFixture) {
	t.Helper()
	ctx, pool, accountID := newAccountingDualRunMigrationPool(t)
	fixture := commonExecutionLifecycleMigrationFixture{
		AccountID: accountID, InstrumentID: uuid.New(), VenueContractID: uuid.New(),
		QuoteSnapshotID: uuid.New(), DecisionAt: time.Date(2026, 8, 15, 19, 0, 0, 123456000, time.UTC),
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO instruments (
			id, identity_key, asset_class, primary_venue, currency,
			tick_size, lot_size, multiplier, settlement_method, status
		) VALUES ($1,$2,'equity','test-venue','USD',0.01,1,1,'physical','active');
		INSERT INTO venue_contracts (
			id, instrument_id, venue, contract_id, currency, tick_size,
			lot_size, multiplier, settlement_method, valid_from, valid_to
		) VALUES ($3,$1,'test-venue',$4,'USD',0.01,1,1,'physical',$5::TIMESTAMPTZ - interval '1 day',$5::TIMESTAMPTZ + interval '1 day');
		INSERT INTO quote_snapshots (
			id, instrument_id, venue_contract_id, provider, venue, source,
			observation_namespace, observation_id, exchange_at, received_at,
			available_at, bid, ask, bid_depth_count, ask_depth_count
		) VALUES ($6,$1,$3,'fixture','test-venue','fixture-feed','quotes/lifecycle',$7,
			$5::TIMESTAMPTZ - interval '3 seconds',$5::TIMESTAMPTZ - interval '2 seconds',$5::TIMESTAMPTZ - interval '1 second',10.24,10.26,0,0)`,
		fixture.InstrumentID, "figi:migration-lifecycle:"+fixture.InstrumentID.String(),
		fixture.VenueContractID, "LIFECYCLE-"+strings.ToUpper(strings.ReplaceAll(fixture.InstrumentID.String(), "-", "")),
		fixture.DecisionAt, fixture.QuoteSnapshotID, "quote-"+fixture.InstrumentID.String(),
	); err != nil {
		t.Fatalf("seed migration 71 references: %v", err)
	}
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000071_common_execution_lifecycle.up.sql")); err != nil {
		t.Fatalf("apply migration 71: %v", err)
	}
	return ctx, pool, fixture
}

func insertMigrationLifecycleProposal(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture commonExecutionLifecycleMigrationFixture,
	key string,
) (uuid.UUID, uuid.UUID) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	intentID := insertMigrationLifecycleIntent(t, ctx, tx, fixture, key)
	evidence := []byte(`{"signal":"entry"}`)
	proposalID := economicid.DeterministicUUID(
		"execution-lifecycle-event", intentID.String(), "ordinary", "strategy",
		"lifecycle/test", "proposal-"+key, "",
	)
	if _, err := tx.Exec(ctx, migrationLifecycleProposalEventSQL,
		proposalID, intentID, fixture.AccountID, "8", fixture.QuoteSnapshotID,
		fixture.DecisionAt, "proposal-"+key, evidence, migrationLifecycleSHA(evidence),
	); err != nil {
		t.Fatalf("insert complete migration proposal event: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit migration proposal: %v", err)
	}
	return intentID, proposalID
}

func insertMigrationLifecycleIntent(
	t *testing.T,
	ctx context.Context,
	tx pgx.Tx,
	fixture commonExecutionLifecycleMigrationFixture,
	key string,
) uuid.UUID {
	t.Helper()
	intentID := economicid.DeterministicUUID("execution-intent", fixture.AccountID.String(), key)
	if _, err := tx.Exec(ctx, `INSERT INTO execution_intents (
		id, account_id, environment, instrument_id, idempotency_key,
		desired_quantity_delta, decision_quote_snapshot_id, decision_at,
		origin_type, origin_id, strategy_version_id, metadata, created_at
	) VALUES ($1,$2,'paper_scored',$3,$4,8,$5,$6,'strategy_version',
		'strategy-version-1','strategy-version-1','{"signal":"entry"}',$6)`,
		intentID, fixture.AccountID, fixture.InstrumentID, key,
		fixture.QuoteSnapshotID, fixture.DecisionAt,
	); err != nil {
		t.Fatalf("insert migration intent: %v", err)
	}
	return intentID
}

const migrationLifecycleEventSQL = `INSERT INTO execution_lifecycle_events (
	id, intent_id, kind, observation_class, prior_state, next_state, account_id,
	environment, origin_type, origin_id, strategy_version_id, quantity_delta,
	source_at, received_at, source, source_namespace, source_event_id, actor,
	reason_code, evidence_bytes, evidence_sha256, evidence, created_at
) VALUES ($1,$2,$3,'ordinary',$4,$5,$6,$7,$8,$9,$10,$11,$12,$12,$13,$14,$15,$16,$17,$18,$19,convert_from($18,'UTF8')::JSONB,$12)`

const migrationLifecycleEventWithCumulativeSQL = `INSERT INTO execution_lifecycle_events (
	id, intent_id, kind, observation_class, prior_state, next_state, account_id,
	environment, origin_type, origin_id, strategy_version_id, quantity_delta,
	cumulative_fill_quantity, source_at, received_at, source, source_namespace,
	source_event_id, actor, reason_code, evidence_bytes, evidence_sha256,
	evidence, created_at
) VALUES ($1,$2,$3,'ordinary',$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13,$14,$15,$16,$17,$18,$19,$20,convert_from($19,'UTF8')::JSONB,$13)`

const migrationLifecycleProposalEventSQL = `INSERT INTO execution_lifecycle_events (
	id, intent_id, kind, observation_class, prior_state, next_state, account_id,
	environment, origin_type, origin_id, strategy_version_id, quantity_delta,
	quote_snapshot_id, source_at, received_at, source, source_namespace,
	source_event_id, actor, reason_code, evidence_bytes, evidence_sha256,
	evidence, created_at
) VALUES ($1,$2,'intent_proposed','ordinary','','proposed',$3,'paper_scored',
	'strategy_version','strategy-version-1','strategy-version-1',$4,$5,
	$6::TIMESTAMPTZ - interval '1 millisecond',$6::TIMESTAMPTZ,'strategy','lifecycle/test',$7,
	'strategy-runner','signal_proposed',$8,$9,convert_from($8,'UTF8')::JSONB,$6)`

func migrationLifecycleSHA(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}
