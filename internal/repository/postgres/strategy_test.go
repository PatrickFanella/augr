package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildListQuery_NoFilters(t *testing.T) {
	query, args := buildListQuery(repository.StrategyFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}

	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM strategies")
	assertContains(t, query, "ORDER BY created_at DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildListQuery_AllFilters(t *testing.T) {
	paper := false

	filter := repository.StrategyFilter{
		Ticker:     "AAPL",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    &paper,
	}

	query, args := buildListQuery(filter, 25, 50)

	// 4 filter args + limit + offset = 6
	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "ticker = $1")
	assertContains(t, query, "market_type = $2")
	assertContains(t, query, "status = $3")
	assertContains(t, query, "is_paper = $4")
	assertContains(t, query, "LIMIT $5 OFFSET $6")

	if args[0] != "AAPL" {
		t.Errorf("expected ticker arg AAPL, got %v", args[0])
	}

	if args[1] != domain.MarketTypeStock {
		t.Errorf("expected market_type arg stock, got %v", args[1])
	}

	if args[2] != domain.StrategyStatusActive {
		t.Errorf("expected status arg active, got %v", args[2])
	}

	if args[3] != false {
		t.Errorf("expected is_paper arg false, got %v", args[3])
	}
}

func TestBuildListQuery_PartialFilters(t *testing.T) {
	filter := repository.StrategyFilter{
		Ticker: "BTC",
		Status: domain.StrategyStatusActive,
	}

	query, args := buildListQuery(filter, 10, 0)

	// 2 filter args + limit + offset = 4
	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "ticker = $1")
	assertNotContains(t, query, "market_type =")
	assertContains(t, query, "status = $2")
	assertNotContains(t, query, "is_paper =")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
}

func TestMarshalConfig_ValidJSON(t *testing.T) {
	input := json.RawMessage(`{"lookback":20,"threshold":0.5}`)

	got, err := marshalConfig(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != `{"lookback":20,"threshold":0.5}` {
		t.Errorf("expected config pass-through, got %s", got)
	}
}

func TestMarshalConfig_NilDefaults(t *testing.T) {
	got, err := marshalConfig(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != "{}" {
		t.Errorf("expected default {}, got %s", got)
	}
}

func TestMarshalConfig_EmptyDefaults(t *testing.T) {
	got, err := marshalConfig(json.RawMessage{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(got) != "{}" {
		t.Errorf("expected default {}, got %s", got)
	}
}

func TestMarshalConfig_InvalidJSON(t *testing.T) {
	_, err := marshalConfig(json.RawMessage(`{not valid`))
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestStrategyRepoIntegration_CreateListAndUpdateStatus(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newStrategyIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewStrategyRepo(pool)

	paused := &domain.Strategy{
		Name:         "Paused Strategy",
		Ticker:       "AAPL",
		MarketType:   domain.MarketTypeStock,
		ScheduleCron: "0 9 * * 1-5",
		Status:       domain.StrategyStatusPaused,
		SkipNextRun:  true,
		IsPaper:      true,
	}
	if err := repo.Create(ctx, paused); err != nil {
		t.Fatalf("Create(paused) error = %v", err)
	}
	if paused.ID == uuid.Nil {
		t.Fatal("expected paused strategy ID to be set")
	}
	if paused.CreatedAt.IsZero() {
		t.Fatal("expected paused strategy CreatedAt to be set")
	}

	storedPaused, err := repo.Get(ctx, paused.ID)
	if err != nil {
		t.Fatalf("Get(paused) error = %v", err)
	}
	if storedPaused.Status != domain.StrategyStatusPaused {
		t.Fatalf("paused strategy status = %q, want %q", storedPaused.Status, domain.StrategyStatusPaused)
	}
	if storedPaused.ScheduleCron != paused.ScheduleCron {
		t.Fatalf("paused strategy schedule_cron = %q, want %q", storedPaused.ScheduleCron, paused.ScheduleCron)
	}
	if !storedPaused.SkipNextRun {
		t.Fatal("paused strategy skip_next_run = false, want true")
	}

	active := &domain.Strategy{
		Name:       "Active Strategy",
		Ticker:     "MSFT",
		MarketType: domain.MarketTypeStock,
		Status:     domain.StrategyStatusActive,
		IsPaper:    false,
	}
	if err := repo.Create(ctx, active); err != nil {
		t.Fatalf("Create(active) error = %v", err)
	}

	pausedOnly, err := repo.List(ctx, repository.StrategyFilter{Status: domain.StrategyStatusPaused}, 10, 0)
	if err != nil {
		t.Fatalf("List(status=paused) error = %v", err)
	}
	if len(pausedOnly) != 1 {
		t.Fatalf("paused strategy count = %d, want 1", len(pausedOnly))
	}
	if pausedOnly[0].ID != paused.ID {
		t.Fatalf("paused strategy id = %s, want %s", pausedOnly[0].ID, paused.ID)
	}

	paused.Status = domain.StrategyStatusInactive
	paused.ScheduleCron = "0 15 * * 1-5"
	paused.SkipNextRun = false
	if err := repo.Update(ctx, paused); err != nil {
		t.Fatalf("Update(paused) error = %v", err)
	}
	if paused.UpdatedAt.IsZero() {
		t.Fatal("expected paused strategy UpdatedAt to be set")
	}

	updatedPaused, err := repo.Get(ctx, paused.ID)
	if err != nil {
		t.Fatalf("Get(updated paused) error = %v", err)
	}
	if updatedPaused.Status != domain.StrategyStatusInactive {
		t.Fatalf("updated status = %q, want %q", updatedPaused.Status, domain.StrategyStatusInactive)
	}
	if updatedPaused.ScheduleCron != paused.ScheduleCron {
		t.Fatalf("updated schedule_cron = %q, want %q", updatedPaused.ScheduleCron, paused.ScheduleCron)
	}
	if updatedPaused.SkipNextRun {
		t.Fatal("updated skip_next_run = true, want false")
	}
}

// assertContains fails if substr is not found in s.
func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected query to contain %q, got:\n%s", substr, s)
	}
}

// assertNotContains fails if substr is found in s.
func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected query NOT to contain %q, got:\n%s", substr, s)
	}
}

func newStrategyIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}

	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	schemaName := "integration_strategy_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	ddl := []string{
		`CREATE TYPE market_type AS ENUM ('stock', 'crypto', 'polymarket')`,
		`CREATE TABLE strategies (
			id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name          TEXT NOT NULL,
			description   TEXT NOT NULL DEFAULT '',
			ticker        TEXT NOT NULL,
			market_type   market_type NOT NULL,
			schedule_cron TEXT NOT NULL DEFAULT '',
			config        JSONB NOT NULL DEFAULT '{}',
			status        TEXT NOT NULL DEFAULT 'active',
			skip_next_run BOOLEAN NOT NULL DEFAULT false,
			is_paper      BOOLEAN NOT NULL DEFAULT true,
			created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}
