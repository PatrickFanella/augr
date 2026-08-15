package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestImmutableLedgerMigrationDefinesAppendOnlyBalancedContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000065_immutable_ledger.up.sql"))
	for _, fragment := range []string{
		"create table ledger_transactions",
		"posting_count integer not null check (posting_count >= 2)",
		"unique (account_id, idempotency_key)",
		"create table ledger_postings",
		"amount numeric(38, 12) not null check (amount <> 0)",
		"unique (transaction_id, idempotency_key)",
		"create constraint trigger trg_ledger_transactions_balanced",
		"deferrable initially deferred",
		"create trigger trg_ledger_transactions_immutable",
		"create trigger trg_ledger_postings_immutable",
		"create table mark_observations",
		"create table projection_checkpoints",
		"create trigger trg_capital_flows_to_ledger",
		"capital_flow.deposit",
		"capital_flow.withdrawal",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000065_immutable_ledger.down.sql"))
	for _, fragment := range []string{
		"drop trigger if exists trg_capital_flows_to_ledger on capital_flows",
		"drop table if exists projection_checkpoints",
		"drop table if exists mark_observations",
		"drop table if exists ledger_postings",
		"drop table if exists ledger_transactions",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestImmutableLedgerMigrationBackfillsBalancedCapitalFlow(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)

	var transactionCount, postingCount, imbalanceCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE origin_type = 'capital_flow'`).Scan(&transactionCount); err != nil {
		t.Fatalf("count backfilled transactions: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_postings`).Scan(&postingCount); err != nil {
		t.Fatalf("count backfilled postings: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM (
		SELECT transaction_id, unit_kind, unit
		FROM ledger_postings
		GROUP BY transaction_id, unit_kind, unit
		HAVING SUM(amount) <> 0
	) AS imbalances`).Scan(&imbalanceCount); err != nil {
		t.Fatalf("count imbalances: %v", err)
	}
	if transactionCount != 1 || postingCount != 2 || imbalanceCount != 0 {
		t.Fatalf("backfill = transactions:%d postings:%d imbalances:%d, want 1/2/0", transactionCount, postingCount, imbalanceCount)
	}
}

func TestImmutableLedgerMigrationRejectsUnbalancedCommit(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	transactionID := uuid.New()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unbalanced transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		effective_at, posting_count
	) VALUES ($1, $2, 'test.unbalanced', $3, 'migration_test', $4, NOW(), 2)`,
		transactionID,
		"00000000-0000-4000-8000-000000000064",
		"test:"+transactionID.String(),
		transactionID.String(),
	); err != nil {
		t.Fatalf("insert unbalanced transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_postings (
		transaction_id, idempotency_key, ledger_account, unit_kind, unit, amount
	) VALUES
		($1, 'debit', 'asset:cash', 'currency', 'USD', 10),
		($1, 'credit', 'equity:test', 'currency', 'USD', -9)`, transactionID); err != nil {
		t.Fatalf("insert unbalanced postings: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("unbalanced ledger transaction committed")
	}

	var persisted int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM ledger_transactions WHERE id = $1`, transactionID).Scan(&persisted); err != nil {
		t.Fatalf("count rejected transaction: %v", err)
	}
	if persisted != 0 {
		t.Fatalf("rejected transaction rows = %d, want 0", persisted)
	}
}

func TestImmutableLedgerMigrationRejectsTransactionWithoutPostings(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	transactionID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		effective_at, posting_count
	) VALUES ($1, $2, 'test.empty', $3, 'migration_test', $4, NOW(), 2)`,
		transactionID,
		"00000000-0000-4000-8000-000000000064",
		"test:"+transactionID.String(),
		transactionID.String(),
	); err == nil {
		t.Fatal("ledger transaction without postings committed")
	}
}

func TestImmutableLedgerMigrationRejectsCrossCurrencyOffset(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	transactionID := uuid.New()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin cross-currency transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		effective_at, posting_count
	) VALUES ($1, $2, 'test.cross_currency', $3, 'migration_test', $4, NOW(), 2)`,
		transactionID,
		"00000000-0000-4000-8000-000000000064",
		"test:"+transactionID.String(),
		transactionID.String(),
	); err != nil {
		t.Fatalf("insert cross-currency transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_postings (
		transaction_id, idempotency_key, ledger_account, unit_kind, unit, amount
	) VALUES
		($1, 'usd', 'asset:cash', 'currency', 'USD', 10),
		($1, 'eur', 'equity:test', 'currency', 'EUR', -10)`, transactionID); err != nil {
		t.Fatalf("insert cross-currency postings: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("cross-currency offset committed without balancing each currency")
	}
}

func TestImmutableLedgerMigrationRejectsPartialReferenceIdentity(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	transactionID := uuid.New()

	if _, err := pool.Exec(ctx, `INSERT INTO ledger_transactions (
		id, account_id, event_type, idempotency_key, origin_type, origin_id,
		reference_id, effective_at, posting_count
	) VALUES ($1, $2, 'test.partial_reference', $3, 'migration_test', $4, $4, NOW(), 2)`,
		transactionID,
		"00000000-0000-4000-8000-000000000064",
		"test:"+transactionID.String(),
		transactionID.String(),
	); err == nil {
		t.Fatal("ledger transaction with only reference_id was accepted")
	}
}

func TestImmutableLedgerMigrationRejectsPostingsAppendedAfterCommit(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	var transactionID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM ledger_transactions ORDER BY created_at, id LIMIT 1`).Scan(&transactionID); err != nil {
		t.Fatalf("load committed ledger transaction: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin late-posting transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `INSERT INTO ledger_postings (
		transaction_id, idempotency_key, ledger_account, unit_kind, unit, amount
	) VALUES
		($1, 'late-debit', 'asset:cash', 'currency', 'USD', 1),
		($1, 'late-credit', 'equity:test', 'currency', 'USD', -1)`, transactionID); err != nil {
		t.Fatalf("insert late balanced postings before deferred validation: %v", err)
	}
	if err := tx.Commit(ctx); err == nil {
		t.Fatal("balanced postings were appended to an already-committed ledger transaction")
	}
}

func TestImmutableLedgerMigrationDualWritesNewCapitalFlows(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	accountID := "00000000-0000-4000-8000-000000000064"

	for _, test := range []struct {
		flowType               string
		amount                 string
		wantCash               string
		wantContributedCapital string
	}{
		{flowType: "deposit", amount: "25000", wantCash: "25000.000000000000", wantContributedCapital: "-25000.000000000000"},
		{flowType: "withdrawal", amount: "1750", wantCash: "-1750.000000000000", wantContributedCapital: "1750.000000000000"},
	} {
		t.Run(test.flowType, func(t *testing.T) {
			flowID := uuid.New()
			if _, err := pool.Exec(ctx, `INSERT INTO capital_flows (
				id, account_id, flow_type, amount, currency, idempotency_key,
				source, effective_at, observed_at
			) VALUES ($1, $2, $3, $4, 'USD', $5, 'operator', NOW(), NOW())`,
				flowID,
				accountID,
				test.flowType,
				test.amount,
				"ledger-migration-test:"+flowID.String(),
			); err != nil {
				t.Fatalf("insert capital flow: %v", err)
			}

			var transactionID uuid.UUID
			var eventType string
			if err := pool.QueryRow(ctx, `SELECT id, event_type
				FROM ledger_transactions
				WHERE account_id = $1 AND origin_type = 'capital_flow' AND origin_id = $2`,
				accountID,
				flowID.String(),
			).Scan(&transactionID, &eventType); err != nil {
				t.Fatalf("load dual-written transaction: %v", err)
			}
			if want := "capital_flow." + test.flowType; eventType != want {
				t.Fatalf("event type = %q, want %q", eventType, want)
			}

			amounts := make(map[string]string)
			rows, err := pool.Query(ctx, `SELECT idempotency_key, amount::TEXT
				FROM ledger_postings WHERE transaction_id = $1`, transactionID)
			if err != nil {
				t.Fatalf("query dual-written postings: %v", err)
			}
			for rows.Next() {
				var key, amount string
				if err := rows.Scan(&key, &amount); err != nil {
					rows.Close()
					t.Fatalf("scan dual-written posting: %v", err)
				}
				amounts[key] = amount
			}
			if err := rows.Err(); err != nil {
				rows.Close()
				t.Fatalf("iterate dual-written postings: %v", err)
			}
			rows.Close()

			if amounts["cash"] != test.wantCash || amounts["contributed-capital"] != test.wantContributedCapital {
				t.Fatalf("dual-write amounts = %#v, want cash %s and contributed capital %s", amounts, test.wantCash, test.wantContributedCapital)
			}
		})
	}
}

func TestImmutableLedgerDownRemovesLedgerAndPreservesCapitalFlows(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000065_immutable_ledger.down.sql")); err != nil {
		t.Fatalf("apply immutable-ledger down migration: %v", err)
	}

	var ledgerTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.ledger_transactions')::TEXT`).Scan(&ledgerTable); err != nil {
		t.Fatalf("check ledger removal: %v", err)
	}
	if ledgerTable != nil {
		t.Fatalf("ledger_transactions remains after down migration: %q", *ledgerTable)
	}

	var capitalFlowsTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.capital_flows')::TEXT`).Scan(&capitalFlowsTable); err != nil {
		t.Fatalf("check capital-flow preservation: %v", err)
	}
	if capitalFlowsTable == nil {
		t.Fatal("capital_flows was removed by ledger rollback")
	}
}

func TestImmutableLedgerMigrationRejectsDuplicatePostingKey(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	var transactionID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT id FROM ledger_transactions ORDER BY created_at, id LIMIT 1`).Scan(&transactionID); err != nil {
		t.Fatalf("load seeded transaction: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO ledger_postings (
		transaction_id, idempotency_key, ledger_account, unit_kind, unit, amount
	) VALUES ($1, 'cash', 'asset:cash', 'currency', 'USD', 1)`, transactionID); err == nil {
		t.Fatal("duplicate posting idempotency key was accepted")
	}
}

func TestImmutableLedgerMigrationRejectsMutation(t *testing.T) {
	ctx, pool := newImmutableLedgerMigrationPool(t)
	var transactionID, postingID uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT transaction_id, id FROM ledger_postings ORDER BY id LIMIT 1`).Scan(&transactionID, &postingID); err != nil {
		t.Fatalf("load seeded ledger rows: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE ledger_postings SET amount = amount + 1 WHERE id = $1`, postingID); err == nil {
		t.Fatal("ledger posting update succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ledger_postings WHERE id = $1`, postingID); err == nil {
		t.Fatal("ledger posting delete succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE ledger_transactions SET event_type = 'test.mutated' WHERE id = $1`, transactionID); err == nil {
		t.Fatal("ledger transaction update succeeded")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM ledger_transactions WHERE id = $1`, transactionID); err == nil {
		t.Fatal("ledger transaction delete succeeded")
	}
}

func newImmutableLedgerMigrationPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping immutable-ledger migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping immutable-ledger migration integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "migr_ledger_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create ledger migration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop ledger migration schema: %v", err)
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
	return ctx, pool
}
