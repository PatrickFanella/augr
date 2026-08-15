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

func TestAccountsCapitalFlowsMigrationDefinesEconomicBoundary(t *testing.T) {
	t.Parallel()

	upSQL := normalizeSQL(t, readMigrationFile(t, "000064_accounts_capital_flows.up.sql"))
	for _, fragment := range []string{
		"create table accounts",
		"environment text not null check (environment in ('paper_scored', 'paper_stress', 'shadow', 'live'))",
		"storage_namespace text not null unique",
		"starting_capital numeric(28, 8) not null check (starting_capital > 0)",
		"buying_power_multiplier numeric(20, 8) not null check (buying_power_multiplier >= 0)",
		"create table capital_flows",
		"account_id uuid not null references accounts(id)",
		"flow_type text not null check (flow_type in ('deposit', 'withdrawal'))",
		"amount numeric(28, 8) not null check (amount > 0)",
		"unique (account_id, idempotency_key)",
		"create trigger trg_accounts_immutable_identity",
		"create trigger trg_capital_flows_validate_currency",
		"create trigger trg_capital_flows_immutable",
		"'00000000-0000-4000-8000-000000000064'::uuid",
		"'account-opening:00000000-0000-4000-8000-000000000064'",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}

	downSQL := normalizeSQL(t, readMigrationFile(t, "000064_accounts_capital_flows.down.sql"))
	for _, fragment := range []string{
		"drop table if exists capital_flows",
		"drop table if exists accounts",
		"drop function if exists reject_account_identity_mutation()",
		"drop function if exists reject_capital_flow_mutation()",
		"drop function if exists validate_capital_flow_currency()",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestAccountsCapitalFlowsMigrationAppliesAndEnforcesHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping migration integration test in short mode")
	}
	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping migration integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()
	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New(admin) error = %v", err)
	}
	t.Cleanup(adminPool.Close)

	schemaName := "migr_accounts_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	identifier := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+identifier); err != nil {
		t.Fatalf("create migration schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+identifier+` CASCADE`); err != nil {
			t.Errorf("drop migration schema: %v", err)
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

	for _, filename := range sortedUpMigrationsThrough(t, "000064_accounts_capital_flows.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("apply %s: %v", filename, err)
		}
	}

	const defaultAccountID = "00000000-0000-4000-8000-000000000064"
	var (
		accountCount    int
		startingCapital string
		openingAmount   string
		flowCount       int
	)
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), MAX(starting_capital)::TEXT FROM accounts`).Scan(&accountCount, &startingCapital); err != nil {
		t.Fatalf("query backfilled account: %v", err)
	}
	if accountCount != 1 || startingCapital != "100000.00000000" {
		t.Fatalf("backfilled accounts = count:%d capital:%q", accountCount, startingCapital)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*), MAX(amount)::TEXT FROM capital_flows WHERE account_id = $1`, defaultAccountID).Scan(&flowCount, &openingAmount); err != nil {
		t.Fatalf("query opening capital flow: %v", err)
	}
	if flowCount != 1 || openingAmount != startingCapital {
		t.Fatalf("opening history = count:%d amount:%q, want %q", flowCount, openingAmount, startingCapital)
	}

	if _, err := pool.Exec(ctx, `INSERT INTO capital_flows (account_id, flow_type, amount, currency, idempotency_key, source, effective_at) VALUES ($1, 'deposit', 1, 'EUR', 'wrong-currency', 'operator', NOW())`, defaultAccountID); err == nil {
		t.Fatal("wrong-currency capital flow succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE capital_flows SET amount = amount + 1 WHERE account_id = $1`, defaultAccountID); err == nil {
		t.Fatal("capital-flow mutation succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET starting_capital = starting_capital + 1 WHERE id = $1`, defaultAccountID); err == nil {
		t.Fatal("account opening identity mutation succeeded")
	}
	if _, err := pool.Exec(ctx, `UPDATE accounts SET status = 'paused' WHERE id = $1`, defaultAccountID); err != nil {
		t.Fatalf("account lifecycle update failed: %v", err)
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000064_accounts_capital_flows.down.sql")); err != nil {
		t.Fatalf("apply migration 64 down: %v", err)
	}
	var accountsTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.accounts')::TEXT`).Scan(&accountsTable); err != nil {
		t.Fatalf("verify accounts removal: %v", err)
	}
	if accountsTable != nil {
		t.Fatalf("accounts table remains after down migration: %q", *accountsTable)
	}
}
