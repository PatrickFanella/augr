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

func TestAgentDecisionsPromptCostUpMigrationDefinesExpectedColumns(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000008_agent_decisions_prompt_cost.up.sql"))

	expectedFragments := []string{
		"alter table agent_decisions",
		"add column prompt_text text",
		"add column cost_usd numeric(12,6) default 0",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestAgentDecisionsPromptCostDownMigrationDropsBothColumns(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000008_agent_decisions_prompt_cost.down.sql"))

	for _, fragment := range []string{
		"drop column if exists prompt_text",
		"drop column if exists cost_usd",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestAgentDecisionsPromptCostMigrationAppliesAgainstExistingSchema(t *testing.T) {
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

	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	schemaName := "migr_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	sanitizedSchemaName := pgx.Identifier{schemaName}.Sanitize()
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+sanitizedSchemaName); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	t.Cleanup(func() {
		if _, err := adminPool.Exec(ctx, `DROP SCHEMA IF EXISTS `+sanitizedSchemaName+` CASCADE`); err != nil {
			t.Errorf("failed to drop schema %q: %v", schemaName, err)
		}
	})

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		t.Fatalf("failed to parse database config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	// Migrations are stored as multi-statement SQL files, so the test uses
	// simple protocol to execute each file in a single Exec call.
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("failed to create schema-scoped pool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, filename := range []string{
		"000001_initial_schema.up.sql",
		"000002_historical_ohlcv.up.sql",
		"000003_backtest_configs.up.sql",
		"000004_backtest_runs.up.sql",
		"000005_backtest_config_schedule.up.sql",
		"000006_api_keys.up.sql",
		"000007_users.up.sql",
		"000008_agent_decisions_prompt_cost.up.sql",
	} {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}

	type columnInfo struct {
		dataType      string
		nullable      string
		defaultClause string
	}

	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'agent_decisions'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("failed to query agent_decisions columns: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]columnInfo)
	for rows.Next() {
		var (
			name          string
			dataType      string
			nullable      string
			defaultClause string
		)
		if err := rows.Scan(&name, &dataType, &nullable, &defaultClause); err != nil {
			t.Fatalf("failed to scan agent_decisions column: %v", err)
		}
		columns[name] = columnInfo{
			dataType:      dataType,
			nullable:      nullable,
			defaultClause: strings.ToLower(defaultClause),
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate agent_decisions columns: %v", err)
	}

	newColumns := map[string]columnInfo{
		"prompt_text": {
			dataType: "text",
			nullable: "YES",
		},
		"cost_usd": {
			dataType:      "numeric",
			nullable:      "YES",
			defaultClause: "0",
		},
	}

	for name, expected := range newColumns {
		got, ok := columns[name]
		if !ok {
			t.Fatalf("expected column %q to exist in agent_decisions after up migration", name)
		}
		if got.dataType != expected.dataType {
			t.Fatalf("expected column %q to have data type %q, got %q", name, expected.dataType, got.dataType)
		}
		if got.nullable != expected.nullable {
			t.Fatalf("expected column %q nullable=%q, got %q", name, expected.nullable, got.nullable)
		}
		if expected.defaultClause != "" && !strings.Contains(got.defaultClause, expected.defaultClause) {
			t.Fatalf("expected column %q default to contain %q, got %q", name, expected.defaultClause, got.defaultClause)
		}
	}

	// Apply down migration and verify both columns are removed.
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000008_agent_decisions_prompt_cost.down.sql")); err != nil {
		t.Fatalf("failed to apply 000008_agent_decisions_prompt_cost.down.sql: %v", err)
	}

	for _, colName := range []string{"prompt_text", "cost_usd"} {
		var count int
		if err := pool.QueryRow(ctx, `
			SELECT COUNT(*)
			FROM information_schema.columns
			WHERE table_schema = current_schema()
			  AND table_name   = 'agent_decisions'
			  AND column_name  = $1
		`, colName).Scan(&count); err != nil {
			t.Fatalf("failed to check column %q after down migration: %v", colName, err)
		}
		if count != 0 {
			t.Fatalf("expected column %q to be dropped, but it still exists", colName)
		}
	}
}
