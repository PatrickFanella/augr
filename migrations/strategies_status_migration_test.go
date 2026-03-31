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

func TestStrategiesStatusUpMigrationDefinesExpectedSchema(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000012_strategies_status.up.sql"))

	expectedFragments := []string{
		"alter table strategies add column status text not null default 'active', add column skip_next_run boolean not null default false;",
		"update strategies set status = case when is_active then 'active' else 'inactive' end;",
		"comment on column strategies.is_active is 'deprecated: use status instead.';",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestStrategiesStatusDownMigrationDropsNewColumns(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000012_strategies_status.down.sql"))

	for _, fragment := range []string{
		"drop column if exists skip_next_run",
		"drop column if exists status",
		"comment on column strategies.is_active is null;",
	} {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestStrategiesStatusMigrationAppliesAgainstExistingSchema(t *testing.T) {
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
	config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatalf("failed to create schema-scoped pool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, filename := range sortedUpMigrationsThrough(t, "000011_pipeline_run_snapshots_ordering_index.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}

	activeStrategyID := uuid.New()
	inactiveStrategyID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO strategies (id, name, ticker, market_type, is_active)
VALUES
    ($1, $2, $3, $4, $5),
    ($6, $7, $8, $9, $10)
`, activeStrategyID, "Active strategy", "AAPL", "stock", true, inactiveStrategyID, "Inactive strategy", "BTCUSD", "crypto", false); err != nil {
		t.Fatalf("failed to seed strategies before migration: %v", err)
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000012_strategies_status.up.sql")); err != nil {
		t.Fatalf("failed to apply 000012_strategies_status.up.sql: %v", err)
	}

	assertTableColumns(t, ctx, pool, "strategies", map[string]columnInfo{
		"id": {
			dataType:      "uuid",
			nullable:      "NO",
			defaultClause: "gen_random_uuid()",
		},
		"name": {
			dataType: "text",
			nullable: "NO",
		},
		"description": {
			dataType: "text",
			nullable: "YES",
		},
		"ticker": {
			dataType: "text",
			nullable: "NO",
		},
		"market_type": {
			dataType: "USER-DEFINED",
			nullable: "NO",
		},
		"schedule_cron": {
			dataType: "text",
			nullable: "YES",
		},
		"config": {
			dataType:      "jsonb",
			nullable:      "NO",
			defaultClause: "'{}'::jsonb",
		},
		"is_active": {
			dataType:      "boolean",
			nullable:      "NO",
			defaultClause: "false",
		},
		"status": {
			dataType:      "text",
			nullable:      "NO",
			defaultClause: "'active'::text",
		},
		"skip_next_run": {
			dataType:      "boolean",
			nullable:      "NO",
			defaultClause: "false",
		},
		"is_paper": {
			dataType:      "boolean",
			nullable:      "NO",
			defaultClause: "true",
		},
		"created_at": {
			dataType:      "timestamp with time zone",
			nullable:      "NO",
			defaultClause: "now()",
		},
		"updated_at": {
			dataType:      "timestamp with time zone",
			nullable:      "NO",
			defaultClause: "now()",
		},
	})

	var activeStatus string
	var inactiveStatus string
	var inactiveSkipNextRun bool
	if err := pool.QueryRow(ctx, `
SELECT
    MAX(CASE WHEN id = $1 THEN status END),
    MAX(CASE WHEN id = $2 THEN status END)
FROM strategies
WHERE id IN ($1, $2)
`, activeStrategyID, inactiveStrategyID).Scan(&activeStatus, &inactiveStatus); err != nil {
		t.Fatalf("failed to query migrated strategy status values: %v", err)
	}

	if err := pool.QueryRow(ctx, `
SELECT skip_next_run
FROM strategies
WHERE id = $1
`, inactiveStrategyID).Scan(&inactiveSkipNextRun); err != nil {
		t.Fatalf("failed to query migrated strategy skip_next_run value: %v", err)
	}

	if activeStatus != "active" {
		t.Fatalf("expected active strategy status to be %q, got %q", "active", activeStatus)
	}
	if inactiveStatus != "inactive" {
		t.Fatalf("expected inactive strategy status to be %q, got %q", "inactive", inactiveStatus)
	}
	if inactiveSkipNextRun {
		t.Fatal("expected skip_next_run to default to false for existing rows")
	}

	postMigrationStrategyID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO strategies (id, name, ticker, market_type, status, skip_next_run)
VALUES ($1, $2, $3, $4, $5, $6)
`, postMigrationStrategyID, "Paused strategy", "ETHUSD", "crypto", "paused", true); err != nil {
		t.Fatalf("failed to insert strategy after status migration: %v", err)
	}

	var strategyColumnCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'strategies'
		  AND column_name IN ('status', 'skip_next_run')
	`).Scan(&strategyColumnCount); err != nil {
		t.Fatalf("failed to count new strategies columns: %v", err)
	}
	if strategyColumnCount != 2 {
		t.Fatalf("expected status and skip_next_run columns to exist, got count=%d", strategyColumnCount)
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000012_strategies_status.down.sql")); err != nil {
		t.Fatalf("failed to apply 000012_strategies_status.down.sql: %v", err)
	}

	var droppedCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'strategies'
		  AND column_name IN ('status', 'skip_next_run')
	`).Scan(&droppedCount); err != nil {
		t.Fatalf("failed to count dropped strategies columns: %v", err)
	}
	if droppedCount != 0 {
		t.Fatalf("expected status and skip_next_run columns to be dropped, got count=%d", droppedCount)
	}

	var isActiveExists int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM information_schema.columns
		WHERE table_schema = current_schema()
		  AND table_name = 'strategies'
		  AND column_name = 'is_active'
	`).Scan(&isActiveExists); err != nil {
		t.Fatalf("failed to verify is_active column after down migration: %v", err)
	}
	if isActiveExists != 1 {
		t.Fatalf("expected is_active column to remain available after down migration, got count=%d", isActiveExists)
	}
}
