package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestKalshiSettlementGateUpMigrationDefinesExpectedContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000056_kalshi_settlement_gate.up.sql"))
	for _, fragment := range []string{
		"create table kalshi_settlement_gate (",
		"job_name text primary key",
		"consecutive_successes integer not null default 0",
		"projection_fingerprint text not null default ''",
		"would_settle_markets integer not null default 0",
		"would_settle_decisions integer not null default 0",
		"insert into kalshi_settlement_gate (job_name) values ('kalshi_settlement') on conflict (job_name) do nothing;",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestKalshiSettlementGateMigrationAppliesAgainstExistingSchema(t *testing.T) {
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
		t.Fatalf("failed to create admin pool: %v", err)
	}
	t.Cleanup(adminPool.Close)
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("failed to parse db config: %v", err)
	}
	schemaName := "migr_" + strings.ReplaceAll(strings.ReplaceAll(t.Name(), "/", "_"), " ", "_")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA IF NOT EXISTS `+pgx.Identifier{schemaName}.Sanitize()); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+pgx.Identifier{schemaName}.Sanitize()+` CASCADE`)
	})
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("failed to create schema pool: %v", err)
	}
	t.Cleanup(pool.Close)
	for _, filename := range sortedUpMigrationsThrough(t, "000056_kalshi_settlement_gate.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}
	assertTableColumns(t, ctx, pool, "kalshi_settlement_gate", map[string]columnInfo{
		"job_name":               {dataType: "text", nullable: "NO"},
		"consecutive_successes":  {dataType: "integer", nullable: "NO", defaultClause: "0"},
		"eligible":               {dataType: "boolean", nullable: "NO", defaultClause: "false"},
		"projection_fingerprint": {dataType: "text", nullable: "NO", defaultClause: "''::text"},
		"would_settle_markets":   {dataType: "integer", nullable: "NO", defaultClause: "0"},
		"would_settle_decisions": {dataType: "integer", nullable: "NO", defaultClause: "0"},
	})
}
