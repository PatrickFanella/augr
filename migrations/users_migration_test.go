package migrations_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestUsersUpMigrationDefinesExpectedSchema(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000007_users.up.sql"))

	expectedFragments := []string{
		"create table users (",
		"id uuid primary key default gen_random_uuid()",
		"username text not null unique",
		"password_hash text not null",
		"created_at timestamptz not null default now()",
		"updated_at timestamptz not null default now()",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestUsersDownMigrationDropsUsersTable(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000007_users.down.sql"))

	if !strings.Contains(downSQL, "drop table if exists users cascade;") {
		t.Fatalf("expected down migration to drop users table, got:\n%s", downSQL)
	}
}

func TestUsersMigrationAppliesAgainstExistingSchema(t *testing.T) {
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
	} {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}

	var usersTable string
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.users')::text`).Scan(&usersTable); err != nil {
		t.Fatalf("failed to check users table: %v", err)
	}
	if usersTable != "users" {
		t.Fatalf("expected users table to exist, got %q", usersTable)
	}

	rows, err := pool.Query(ctx, `
		SELECT column_name, data_type, is_nullable, COALESCE(column_default, '')
		FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'users'
		ORDER BY ordinal_position
	`)
	if err != nil {
		t.Fatalf("failed to query users columns: %v", err)
	}
	defer rows.Close()

	type columnInfo struct {
		dataType      string
		nullable      string
		defaultClause string
	}

	columns := make(map[string]columnInfo)
	for rows.Next() {
		var (
			name          string
			dataType      string
			nullable      string
			defaultClause string
		)
		if err := rows.Scan(&name, &dataType, &nullable, &defaultClause); err != nil {
			t.Fatalf("failed to scan users column: %v", err)
		}
		columns[name] = columnInfo{
			dataType:      dataType,
			nullable:      nullable,
			defaultClause: strings.ToLower(defaultClause),
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("failed to iterate users columns: %v", err)
	}

	expectedColumns := map[string]columnInfo{
		"id": {
			dataType:      "uuid",
			nullable:      "NO",
			defaultClause: "gen_random_uuid()",
		},
		"username": {
			dataType: "text",
			nullable: "NO",
		},
		"password_hash": {
			dataType: "text",
			nullable: "NO",
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
	}

	if len(columns) != len(expectedColumns) {
		t.Fatalf("expected %d users columns, got %d", len(expectedColumns), len(columns))
	}

	for name, expected := range expectedColumns {
		got, ok := columns[name]
		if !ok {
			t.Fatalf("expected column %q to exist", name)
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

	var usernameIndexCount int
	if err := pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE schemaname = current_schema()
		  AND tablename = 'users'
		  AND indexdef ILIKE '%(username)%'
	`).Scan(&usernameIndexCount); err != nil {
		t.Fatalf("failed to query users indexes: %v", err)
	}
	if usernameIndexCount == 0 {
		t.Fatal("expected an index on users.username")
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000007_users.down.sql")); err != nil {
		t.Fatalf("failed to apply 000007_users.down.sql: %v", err)
	}

	var droppedUsersTable *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass(current_schema() || '.users')::text`).Scan(&droppedUsersTable); err != nil {
		t.Fatalf("failed to verify users table removal: %v", err)
	}
	if droppedUsersTable != nil {
		t.Fatalf("expected users table to be dropped, got %q", *droppedUsersTable)
	}
}

func readMigrationFile(t *testing.T, filename string) string {
	t.Helper()

	path := filepath.Join(migrationsDir(t), filename)
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read migration %s: %v", path, err)
	}

	return string(contents)
}

func migrationsDir(t *testing.T) string {
	t.Helper()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to determine migration test file path")
	}

	return filepath.Dir(filename)
}

func normalizeSQL(t *testing.T, sql string) string {
	t.Helper()
	return strings.ToLower(strings.Join(strings.Fields(sql), " "))
}
