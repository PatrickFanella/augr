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

func TestPipelineRunPhaseTimingsUpMigrationDefinesExpectedSchema(t *testing.T) {
	t.Parallel()

	upSQL := normalizeSQL(t, readMigrationFile(t, "000013_pipeline_run_phase_timings.up.sql"))

	expectedFragments := []string{
		"alter table pipeline_runs add column phase_timings jsonb;",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestPipelineRunPhaseTimingsDownMigrationDropsColumn(t *testing.T) {
	t.Parallel()

	downSQL := normalizeSQL(t, readMigrationFile(t, "000013_pipeline_run_phase_timings.down.sql"))

	expectedFragments := []string{
		"alter table pipeline_runs drop column if exists phase_timings;",
	}

	for _, fragment := range expectedFragments {
		if !strings.Contains(downSQL, fragment) {
			t.Fatalf("expected down migration to contain %q, got:\n%s", fragment, downSQL)
		}
	}
}

func TestPipelineRunPhaseTimingsMigrationAppliesAgainstExistingSchema(t *testing.T) {
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

	for _, filename := range sortedUpMigrationsThrough(t, "000012_strategies_status.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}

	// Apply the phase_timings migration.
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000013_pipeline_run_phase_timings.up.sql")); err != nil {
		t.Fatalf("failed to apply 000013_pipeline_run_phase_timings.up.sql: %v", err)
	}

	// Verify column exists with correct type and nullability.
	assertTableColumns(t, ctx, pool, "pipeline_runs", map[string]columnInfo{
		"phase_timings": {
			dataType: "jsonb",
			nullable: "YES",
		},
	})

	// Verify existing rows have NULL phase_timings.
	strategyID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO strategies (id, name, ticker, market_type, is_active)
VALUES ($1, $2, $3, $4, $5)
`, strategyID, "Test strategy", "AAPL", "stock", true); err != nil {
		t.Fatalf("failed to seed strategy: %v", err)
	}

	runID := uuid.New()
	if _, err := pool.Exec(ctx, `
INSERT INTO pipeline_runs (id, strategy_id, status)
VALUES ($1, $2, $3)
`, runID, strategyID, "completed"); err != nil {
		t.Fatalf("failed to insert pipeline run without phase_timings: %v", err)
	}

	var phaseTimings *string
	if err := pool.QueryRow(ctx, `
SELECT phase_timings::text FROM pipeline_runs WHERE id = $1
`, runID).Scan(&phaseTimings); err != nil {
		t.Fatalf("failed to query pipeline run phase_timings: %v", err)
	}
	if phaseTimings != nil {
		t.Fatalf("expected NULL phase_timings for row without explicit value, got %q", *phaseTimings)
	}

	// Verify JSONB value can be inserted.
	runWithTimingsID := uuid.New()
	timingsJSON := `{"analysis_ms": 1234, "research_debate_ms": 5678, "trading_ms": 234, "risk_debate_ms": 3456}`
	if _, err := pool.Exec(ctx, `
INSERT INTO pipeline_runs (id, strategy_id, status, phase_timings)
VALUES ($1, $2, $3, $4::jsonb)
`, runWithTimingsID, strategyID, "completed", timingsJSON); err != nil {
		t.Fatalf("failed to insert pipeline run with phase_timings: %v", err)
	}

	var storedTimings string
	if err := pool.QueryRow(ctx, `
SELECT phase_timings::text FROM pipeline_runs WHERE id = $1
`, runWithTimingsID).Scan(&storedTimings); err != nil {
		t.Fatalf("failed to query pipeline run with phase_timings: %v", err)
	}
	if !strings.Contains(storedTimings, "analysis_ms") {
		t.Fatalf("expected stored phase_timings to contain analysis_ms, got %q", storedTimings)
	}

	// Apply down migration and verify column is removed.
	if _, err := pool.Exec(ctx, readMigrationFile(t, "000013_pipeline_run_phase_timings.down.sql")); err != nil {
		t.Fatalf("failed to apply 000013_pipeline_run_phase_timings.down.sql: %v", err)
	}

	var colCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM information_schema.columns
WHERE table_name = 'pipeline_runs' AND column_name = 'phase_timings'
`).Scan(&colCount); err != nil {
		t.Fatalf("failed to check column existence after down migration: %v", err)
	}
	if colCount != 0 {
		t.Fatal("expected phase_timings column to be dropped after down migration")
	}
}
