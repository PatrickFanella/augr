package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPositionProvenanceUpMigrationDefinesExpectedContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000058_position_provenance.up.sql"))
	for _, fragment := range []string{
		"create table if not exists position_provenance (",
		"position_id uuid primary key references positions(id) on delete cascade",
		"broker text not null check (broker in ('alpaca'))",
		"created_at timestamptz not null default now()",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestPositionProvenanceMigrationAppliesAgainstExistingSchema(t *testing.T) {
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
	for _, filename := range sortedUpMigrationsThrough(t, "000058_position_provenance.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}
	assertTableColumns(t, ctx, pool, "position_provenance", map[string]columnInfo{
		"position_id": {dataType: "uuid", nullable: "NO"},
		"broker":      {dataType: "text", nullable: "NO"},
		"created_at":  {dataType: "timestamp with time zone", nullable: "NO", defaultClause: "now()"},
	})
}
