package migrations_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestProviderRateLimitCooldownsUpMigrationDefinesExpectedContract(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000057_provider_rate_limit_cooldowns.up.sql"))
	for _, fragment := range []string{"create table provider_rate_limit_cooldowns (", "provider text primary key", "retry_after_until timestamptz not null"} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestProviderRateLimitCooldownsMigrationAppliesAgainstExistingSchema(t *testing.T) {
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
	for _, filename := range sortedUpMigrationsThrough(t, "000057_provider_rate_limit_cooldowns.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}
	assertTableColumns(t, ctx, pool, "provider_rate_limit_cooldowns", map[string]columnInfo{
		"provider":          {dataType: "text", nullable: "NO"},
		"retry_after_until": {dataType: "timestamp with time zone", nullable: "NO"},
		"updated_at":        {dataType: "timestamp with time zone", nullable: "NO"},
	})
}
