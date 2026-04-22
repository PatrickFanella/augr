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

func TestDiscoveryStrategyDedupUpMigrationDefinesExpectedSQL(t *testing.T) {
	upSQL := normalizeSQL(t, readMigrationFile(t, "000031_discovery_strategy_dedup.up.sql"))

	for _, fragment := range []string{
		"create temp table _strategy_dedup_map on commit drop as",
		"partition by ticker, market_type, is_paper, name",
		"where is_paper = true and (name like 'discovery:%' or name like 'options:%')",
		"update backtest_configs",
		"update orders",
		"update positions",
		"update report_artifacts",
		"update pipeline_runs",
		"update agent_events",
		"delete from strategies",
		"create unique index if not exists idx_strategies_discovery_unique",
		"on strategies (ticker, market_type, is_paper, name)",
	} {
		if !strings.Contains(upSQL, fragment) {
			t.Fatalf("expected up migration to contain %q, got:\n%s", fragment, upSQL)
		}
	}
}

func TestDiscoveryStrategyDedupDownMigrationDropsIndex(t *testing.T) {
	downSQL := normalizeSQL(t, readMigrationFile(t, "000031_discovery_strategy_dedup.down.sql"))
	if !strings.Contains(downSQL, "drop index if exists idx_strategies_discovery_unique") {
		t.Fatalf("expected down migration to drop discovery dedup index, got:\n%s", downSQL)
	}
}

func TestDiscoveryStrategyDedupMigrationAppliesAndEnforcesUniqueness(t *testing.T) {
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

	for _, filename := range sortedUpMigrationsThrough(t, "000030_embeddings.up.sql") {
		if _, err := pool.Exec(ctx, readMigrationFile(t, filename)); err != nil {
			t.Fatalf("failed to apply %s: %v", filename, err)
		}
	}

	// Seed duplicates before migration 000031 runs.
	var keeperStockID uuid.UUID
	if err := pool.QueryRow(ctx, `
INSERT INTO strategies (name, ticker, market_type, is_paper, status)
VALUES ('discovery: PBM RSI Momentum Breakout', 'PBM', 'stock', true, 'active')
RETURNING id
`).Scan(&keeperStockID); err != nil {
		t.Fatalf("failed to seed keeper stock strategy: %v", err)
	}
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO strategies (name, ticker, market_type, is_paper, status)
VALUES ('discovery: PBM RSI Momentum Breakout', 'PBM', 'stock', true, 'active')
`); err != nil {
			t.Fatalf("failed to seed stock duplicate %d: %v", i, err)
		}
	}
	for i := 0; i < 2; i++ {
		if _, err := pool.Exec(ctx, `
INSERT INTO strategies (name, ticker, market_type, is_paper, status)
VALUES ('options: QQQ bull_put_spread', 'QQQ', 'options', true, 'active')
`); err != nil {
			t.Fatalf("failed to seed options duplicate %d: %v", i, err)
		}
	}

	if _, err := pool.Exec(ctx, `
INSERT INTO backtest_configs (strategy_id, name, start_date, end_date, simulation_params)
VALUES ($1, 'dedup cfg', DATE '2025-01-01', DATE '2025-02-01', '{}'::jsonb)
`, keeperStockID); err != nil {
		t.Fatalf("failed to seed backtest config: %v", err)
	}

	// Attach one backtest config to a duplicate strategy id so migration must rewire it.
	var duplicateStockID uuid.UUID
	if err := pool.QueryRow(ctx, `
SELECT id
  FROM strategies
 WHERE name = 'discovery: PBM RSI Momentum Breakout'
   AND ticker = 'PBM'
   AND market_type = 'stock'
   AND is_paper = true
   AND id <> $1
 ORDER BY created_at DESC, id DESC
 LIMIT 1
`, keeperStockID).Scan(&duplicateStockID); err != nil {
		t.Fatalf("failed to select duplicate stock strategy id: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE backtest_configs
   SET strategy_id = $2
 WHERE strategy_id = $1
`, keeperStockID, duplicateStockID); err != nil {
		t.Fatalf("failed to point backtest config at duplicate strategy: %v", err)
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000031_discovery_strategy_dedup.up.sql")); err != nil {
		t.Fatalf("failed to apply 000031 up migration: %v", err)
	}

	var stockCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM strategies
 WHERE name = 'discovery: PBM RSI Momentum Breakout'
   AND ticker = 'PBM'
   AND market_type = 'stock'
   AND is_paper = true
`).Scan(&stockCount); err != nil {
		t.Fatalf("failed counting deduped stock strategies: %v", err)
	}
	if stockCount != 1 {
		t.Fatalf("stock dedup count = %d, want 1", stockCount)
	}

	var optionsCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*) FROM strategies
 WHERE name = 'options: QQQ bull_put_spread'
   AND ticker = 'QQQ'
   AND market_type = 'options'
   AND is_paper = true
`).Scan(&optionsCount); err != nil {
		t.Fatalf("failed counting deduped options strategies: %v", err)
	}
	if optionsCount != 1 {
		t.Fatalf("options dedup count = %d, want 1", optionsCount)
	}

	var rewiredCount int
	if err := pool.QueryRow(ctx, `
SELECT COUNT(*)
  FROM backtest_configs
 WHERE strategy_id = $1
`, keeperStockID).Scan(&rewiredCount); err != nil {
		t.Fatalf("failed counting rewired backtest configs: %v", err)
	}
	if rewiredCount != 1 {
		t.Fatalf("rewired backtest config count = %d, want 1", rewiredCount)
	}

	// Verify uniqueness is now enforced by index.
	_, err = pool.Exec(ctx, `
INSERT INTO strategies (name, ticker, market_type, is_paper, status)
VALUES ('discovery: PBM RSI Momentum Breakout', 'PBM', 'stock', true, 'active')
`)
	if err == nil {
		t.Fatal("expected unique violation after dedup migration, got nil")
	}
	errText := strings.ToLower(err.Error())
	if !strings.Contains(errText, "unique") && !strings.Contains(errText, "duplicate") {
		t.Fatalf("expected unique/duplicate error, got: %v", err)
	}

	if _, err := pool.Exec(ctx, readMigrationFile(t, "000031_discovery_strategy_dedup.down.sql")); err != nil {
		t.Fatalf("failed to apply 000031 down migration: %v", err)
	}
}
