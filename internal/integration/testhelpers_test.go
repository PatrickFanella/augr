package integration

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
)

// testDB holds a connection pool scoped to an isolated schema for one test.
type testDB struct {
	Pool      *pgxpool.Pool
	AdminPool *pgxpool.Pool
	Schema    string
}

// repos groups all repository implementations for a single test schema.
type repos struct {
	Strategy      *postgres.StrategyRepo
	PipelineRun   *postgres.PipelineRunRepo
	AgentDecision *postgres.AgentDecisionRepo
	Order         *postgres.OrderRepo
	Position      *postgres.PositionRepo
	Trade         *postgres.TradeRepo
	Memory        *postgres.MemoryRepo
}

// newTestDB creates an isolated PostgreSQL schema for the test.
// It skips the test if neither DB_URL nor DATABASE_URL is set or -short mode is used.
func newTestDB(t *testing.T) *testDB {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	databaseURL := os.Getenv("DB_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		t.Skip("skipping integration test: DB_URL or DATABASE_URL is not set")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("failed to create admin pool: %v", err)
	}

	if _, err := adminPool.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pgcrypto`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to ensure pgcrypto extension: %v", err)
	}

	schemaName := "integ_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA "`+schemaName+`"`); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		dropSchema(adminPool, schemaName)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		dropSchema(adminPool, schemaName)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	applyDDL(t, pool)

	db := &testDB{
		Pool:      pool,
		AdminPool: adminPool,
		Schema:    schemaName,
	}

	t.Cleanup(func() {
		pool.Close()
		dropSchema(adminPool, schemaName)
		adminPool.Close()
	})

	return db
}

// newRepos creates all repository implementations for the given test DB.
func newRepos(db *testDB) repos {
	return repos{
		Strategy:      postgres.NewStrategyRepo(db.Pool),
		PipelineRun:   postgres.NewPipelineRunRepo(db.Pool),
		AgentDecision: postgres.NewAgentDecisionRepo(db.Pool),
		Order:         postgres.NewOrderRepo(db.Pool),
		Position:      postgres.NewPositionRepo(db.Pool),
		Trade:         postgres.NewTradeRepo(db.Pool),
		Memory:        postgres.NewMemoryRepo(db.Pool),
	}
}

// applyDDL creates all enum types and tables needed for integration tests.
func applyDDL(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()

	ddl := []string{
		// Enum types
		`CREATE TYPE pipeline_status AS ENUM ('running', 'completed', 'failed', 'cancelled')`,
		`CREATE TYPE order_status AS ENUM ('pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected')`,
		`CREATE TYPE trade_side AS ENUM ('buy', 'sell')`,
		`CREATE TYPE order_type AS ENUM ('market', 'limit', 'stop', 'stop_limit')`,
		`CREATE TYPE position_side AS ENUM ('long', 'short')`,
		`CREATE TYPE market_type AS ENUM ('stock', 'crypto', 'polymarket')`,

		// Strategies
		`CREATE TABLE strategies (
			id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			name        TEXT        NOT NULL,
			description TEXT        NOT NULL DEFAULT '',
			ticker      TEXT        NOT NULL,
			market_type market_type NOT NULL DEFAULT 'stock',
			schedule    TEXT        NOT NULL DEFAULT '',
			config      JSONB       NOT NULL DEFAULT '{}',
			is_active   BOOLEAN     NOT NULL DEFAULT true,
			is_paper    BOOLEAN     NOT NULL DEFAULT true,
			created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,

		// Pipeline runs (partitioned)
		`CREATE TABLE pipeline_runs (
			id              UUID            NOT NULL DEFAULT gen_random_uuid(),
			strategy_id     UUID            NOT NULL,
			ticker          TEXT            NOT NULL,
			trade_date      DATE            NOT NULL,
			status          pipeline_status NOT NULL DEFAULT 'running',
			signal          TEXT            NOT NULL DEFAULT '',
			started_at      TIMESTAMPTZ     NOT NULL,
			completed_at    TIMESTAMPTZ,
			error_message   TEXT            NOT NULL DEFAULT '',
			config_snapshot JSONB,
			PRIMARY KEY (id, trade_date)
		) PARTITION BY RANGE (trade_date)`,
		`CREATE TABLE pipeline_runs_2026_q1 PARTITION OF pipeline_runs
			FOR VALUES FROM ('2026-01-01') TO ('2026-04-01')`,
		`CREATE TABLE pipeline_runs_default PARTITION OF pipeline_runs DEFAULT`,
		`CREATE INDEX idx_pipeline_runs_strategy_id ON pipeline_runs (strategy_id)`,

		// Agent decisions (partitioned)
		`CREATE TABLE agent_decisions (
			id                UUID        NOT NULL DEFAULT gen_random_uuid(),
			pipeline_run_id   UUID        NOT NULL,
			agent_role        TEXT        NOT NULL,
			phase             TEXT        NOT NULL,
			round_number      INT,
			input_summary     TEXT,
			output_text       TEXT        NOT NULL,
			output_structured JSONB,
			llm_provider      TEXT,
			llm_model         TEXT,
			prompt_tokens     INT,
			completion_tokens INT,
			latency_ms        INT,
			created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			PRIMARY KEY (id, created_at)
		) PARTITION BY RANGE (created_at)`,
		`CREATE TABLE agent_decisions_2026_q1 PARTITION OF agent_decisions
			FOR VALUES FROM ('2026-01-01') TO ('2026-04-01')`,
		`CREATE TABLE agent_decisions_default PARTITION OF agent_decisions DEFAULT`,
		`CREATE INDEX idx_agent_decisions_pipeline_run_id ON agent_decisions (pipeline_run_id)`,

		// Positions
		`CREATE TABLE positions (
			id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
			strategy_id     UUID           REFERENCES strategies (id),
			ticker          TEXT           NOT NULL,
			side            position_side  NOT NULL,
			quantity        NUMERIC(20, 8) NOT NULL,
			avg_entry       NUMERIC(20, 8) NOT NULL,
			current_price   NUMERIC(20, 8),
			unrealized_pnl  NUMERIC(20, 8),
			realized_pnl    NUMERIC(20, 8) NOT NULL DEFAULT 0,
			stop_loss       NUMERIC(20, 8),
			take_profit     NUMERIC(20, 8),
			opened_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
			closed_at       TIMESTAMPTZ
		)`,

		// Orders
		`CREATE TABLE orders (
			id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
			strategy_id     UUID           REFERENCES strategies (id),
			pipeline_run_id UUID,
			external_id     TEXT,
			ticker          TEXT           NOT NULL,
			side            trade_side     NOT NULL,
			order_type      order_type     NOT NULL,
			quantity        NUMERIC(20, 8) NOT NULL,
			limit_price     NUMERIC(20, 8),
			stop_price      NUMERIC(20, 8),
			filled_quantity NUMERIC(20, 8) NOT NULL DEFAULT 0,
			filled_avg_price NUMERIC(20, 8),
			status          order_status   NOT NULL DEFAULT 'pending',
			broker          TEXT,
			submitted_at    TIMESTAMPTZ,
			filled_at       TIMESTAMPTZ,
			created_at      TIMESTAMPTZ    NOT NULL DEFAULT NOW()
		)`,

		// Trades
		`CREATE TABLE trades (
			id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
			order_id    UUID           REFERENCES orders (id),
			position_id UUID           REFERENCES positions (id),
			ticker      TEXT           NOT NULL,
			side        trade_side     NOT NULL,
			quantity    NUMERIC(20, 8) NOT NULL,
			price       NUMERIC(20, 8) NOT NULL,
			fee         NUMERIC(20, 8) NOT NULL DEFAULT 0,
			executed_at TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
			created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW()
		)`,

		// Agent memories (with FTS)
		`CREATE TABLE agent_memories (
			id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
			agent_role       TEXT        NOT NULL,
			situation        TEXT        NOT NULL,
			situation_tsv    TSVECTOR,
			recommendation   TEXT        NOT NULL DEFAULT '',
			outcome          TEXT,
			pipeline_run_id  UUID,
			relevance_score  NUMERIC(5, 4),
			created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE OR REPLACE FUNCTION agent_memories_tsv_trigger() RETURNS trigger AS $$
		 BEGIN
			NEW.situation_tsv := to_tsvector('english', NEW.situation);
			RETURN NEW;
		 END;
		 $$ LANGUAGE plpgsql`,
		`CREATE TRIGGER trg_agent_memories_tsv
			BEFORE INSERT OR UPDATE OF situation ON agent_memories
			FOR EACH ROW EXECUTE FUNCTION agent_memories_tsv_trigger()`,
		`CREATE INDEX idx_agent_memories_situation_tsv ON agent_memories USING GIN (situation_tsv)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			t.Fatalf("failed to apply DDL: %v\nstatement: %s", err, stmt)
		}
	}
}

func dropSchema(pool *pgxpool.Pool, schema string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = pool.Exec(ctx, `DROP SCHEMA "`+schema+`" CASCADE`)
}

// ---------------------------------------------------------------------------
// Fixture helpers
// ---------------------------------------------------------------------------

func createStrategy(t *testing.T, ctx context.Context, r *postgres.StrategyRepo, name, ticker string) *domain.Strategy {
	t.Helper()
	s := &domain.Strategy{
		Name:       name,
		Ticker:     ticker,
		MarketType: domain.MarketTypeStock,
		IsActive:   true,
		IsPaper:    true,
	}
	if err := r.Create(ctx, s); err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}
	return s
}

func createPipelineRun(t *testing.T, ctx context.Context, r *postgres.PipelineRunRepo, strategyID uuid.UUID, ticker string, tradeDate time.Time) *domain.PipelineRun {
	t.Helper()
	run := &domain.PipelineRun{
		StrategyID: strategyID,
		Ticker:     ticker,
		TradeDate:  tradeDate,
		Status:     domain.PipelineStatusRunning,
		StartedAt:  tradeDate.Add(9*time.Hour + 30*time.Minute),
	}
	if err := r.Create(ctx, run); err != nil {
		t.Fatalf("failed to create pipeline run: %v", err)
	}
	return run
}

func createPosition(t *testing.T, ctx context.Context, r *postgres.PositionRepo, strategyID uuid.UUID, ticker string, side domain.PositionSide, quantity, avgEntry float64) *domain.Position {
	t.Helper()
	pos := &domain.Position{
		StrategyID: &strategyID,
		Ticker:     ticker,
		Side:       side,
		Quantity:    quantity,
		AvgEntry:   avgEntry,
	}
	if err := r.Create(ctx, pos); err != nil {
		t.Fatalf("failed to create position: %v", err)
	}
	return pos
}

func createOrder(t *testing.T, ctx context.Context, r *postgres.OrderRepo, strategyID uuid.UUID, runID *uuid.UUID, ticker string, side domain.OrderSide, orderType domain.OrderType, qty float64) *domain.Order {
	t.Helper()
	order := &domain.Order{
		StrategyID:    &strategyID,
		PipelineRunID: runID,
		Ticker:        ticker,
		Side:          side,
		OrderType:     orderType,
		Quantity:      qty,
		Status:        domain.OrderStatusPending,
		Broker:        "alpaca",
	}
	if err := r.Create(ctx, order); err != nil {
		t.Fatalf("failed to create order: %v", err)
	}
	return order
}

// ---------------------------------------------------------------------------
// Mock LLM provider
// ---------------------------------------------------------------------------

type mockLLMProvider struct {
	response *llm.CompletionResponse
	err      error
	calls    int
}

func (m *mockLLMProvider) Complete(_ context.Context, _ llm.CompletionRequest) (*llm.CompletionResponse, error) {
	m.calls++
	return m.response, m.err
}
