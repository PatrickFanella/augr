package postgres

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildOpportunityQuery(t *testing.T) {
	strategyID := uuid.New()
	createdAfter := time.Date(2026, 6, 18, 9, 0, 0, 0, time.UTC)
	expiresBefore := time.Date(2026, 6, 19, 9, 0, 0, 0, time.UTC)

	query, args := buildOpportunityListQuery(repository.OpportunityFilter{
		Status:        domain.OpportunityStatusQueued,
		MarketType:    domain.MarketTypeStock,
		StrategyID:    &strategyID,
		Ticker:        "AAPL",
		ExpiresBefore: &expiresBefore,
		CreatedAfter:  &createdAfter,
	}, 25, 50)

	if len(args) != 8 {
		t.Fatalf("expected 8 args, got %d: %#v", len(args), args)
	}
	assertContains(t, query, "status = $1")
	assertContains(t, query, "market_type = $2")
	assertContains(t, query, "strategy_id = $3")
	assertContains(t, query, "ticker = $4")
	assertContains(t, query, "expires_at <= $5")
	assertContains(t, query, "created_at >= $6")
	assertContains(t, query, "LIMIT $7 OFFSET $8")
}

func TestOpportunityRepoIntegration_CRUDAndUpsert(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newOpportunityIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewOpportunityRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	runID := uuid.New()
	expiresAt := time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC)
	initialScore := 1.25
	evidence := json.RawMessage(`{"signals":["momentum"]}`)

	opportunity := &domain.Opportunity{
		StrategyID:        strategyID,
		PipelineRunID:     &runID,
		MarketType:        domain.MarketTypeStock,
		Ticker:            "AAPL",
		Side:              domain.OrderSideBuy,
		Signal:            domain.PipelineSignalBuy,
		Status:            domain.OpportunityStatusQueued,
		Score:             &initialScore,
		Confidence:        0.87,
		EdgePct:           2.5,
		ExpectedReturnPct: 4.5,
		MaxLossPct:        1.0,
		LiquidityUSD:      1250000,
		MarketCapUSD:      3000000000000,
		SpreadPct:         0.15,
		ProposedNotional:  2500,
		SelectedNotional:  0,
		Reason:            "strong setup",
		RejectReason:      "",
		Evidence:          evidence,
		ExpiresAt:         expiresAt,
		DedupeKey:         "AAPL|stock|buy|buy|2026-06-20",
	}
	if err := repo.Create(ctx, opportunity); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if opportunity.ID == uuid.Nil || opportunity.CreatedAt.IsZero() || opportunity.UpdatedAt.IsZero() {
		t.Fatal("expected Create() to populate id/timestamps")
	}

	got, err := repo.Get(ctx, opportunity.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.StrategyID != strategyID || got.Ticker != "AAPL" || got.Status != domain.OpportunityStatusQueued {
		t.Fatalf("unexpected opportunity roundtrip: %+v", got)
	}
	if got.Score == nil || *got.Score != initialScore {
		t.Fatalf("unexpected score roundtrip: %+v", got.Score)
	}
	if got.MarketCapUSD != 3000000000000 {
		t.Fatalf("unexpected market cap roundtrip: %v", got.MarketCapUSD)
	}
	if !jsonBytesEqual(got.Evidence, evidence) {
		t.Fatalf("unexpected evidence roundtrip: %s", got.Evidence)
	}

	newScore := 2.5
	opportunity.Score = &newScore
	opportunity.Confidence = 0.91
	opportunity.ProposedNotional = 4000
	opportunity.MarketCapUSD = 3100000000000
	opportunity.Reason = "refreshed setup"
	if err := repo.UpsertQueuedByDedupeKey(ctx, opportunity); err != nil {
		t.Fatalf("UpsertQueuedByDedupeKey() error = %v", err)
	}
	if opportunity.ID != got.ID {
		t.Fatalf("expected upsert to keep id %s, got %s", got.ID, opportunity.ID)
	}

	updated, err := repo.Get(ctx, opportunity.ID)
	if err != nil {
		t.Fatalf("Get() after upsert error = %v", err)
	}
	if updated.Confidence != 0.91 || updated.ProposedNotional != 4000 || updated.Reason != "refreshed setup" {
		t.Fatalf("unexpected updated opportunity: %+v", updated)
	}
	if updated.MarketCapUSD != 3100000000000 {
		t.Fatalf("expected updated market cap, got %v", updated.MarketCapUSD)
	}
	if updated.Score == nil || *updated.Score != newScore {
		t.Fatalf("expected updated score %.2f, got %+v", newScore, updated.Score)
	}

	count, err := repo.Count(ctx, repository.OpportunityFilter{StrategyID: &strategyID})
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count 1, got %d", count)
	}

	listed, err := repo.List(ctx, repository.OpportunityFilter{Ticker: "AAPL", Status: domain.OpportunityStatusQueued}, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].ID != opportunity.ID {
		t.Fatalf("expected single listed opportunity, got %+v", listed)
	}

	if err := repo.UpdateStatus(ctx, opportunity.ID, domain.OpportunityStatusRejected, "spread too wide"); err != nil {
		t.Fatalf("UpdateStatus() error = %v", err)
	}
	statusUpdated, err := repo.Get(ctx, opportunity.ID)
	if err != nil {
		t.Fatalf("Get() after status update error = %v", err)
	}
	if statusUpdated.Status != domain.OpportunityStatusRejected || statusUpdated.RejectReason != "spread too wide" {
		t.Fatalf("unexpected status update result: %+v", statusUpdated)
	}
}

func newOpportunityIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_opportunity_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+quoteIdent(schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("failed to create test schema: %v", err)
	}

	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+quoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to parse pool config: %v", err)
	}
	config.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+quoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
		t.Fatalf("failed to create test pool: %v", err)
	}

	ddl := []string{
		`CREATE TYPE market_type AS ENUM ('stock', 'crypto', 'polymarket')`,
		`CREATE TYPE order_side AS ENUM ('buy', 'sell')`,
		`CREATE TYPE pipeline_signal AS ENUM ('buy', 'sell', 'hold')`,
		`CREATE TABLE strategies (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`,
		`CREATE TABLE pipeline_runs (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`,
		`CREATE TABLE orders (id UUID PRIMARY KEY DEFAULT gen_random_uuid())`,
		`CREATE TABLE portfolio_opportunities (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			strategy_id UUID NOT NULL REFERENCES strategies (id),
			pipeline_run_id UUID,
			market_type market_type NOT NULL,
			ticker TEXT NOT NULL,
			side order_side NOT NULL,
			signal pipeline_signal NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('queued', 'selected', 'rejected', 'expired', 'executed')),
			score NUMERIC,
			confidence NUMERIC NOT NULL DEFAULT 0,
			edge_pct NUMERIC NOT NULL DEFAULT 0,
			expected_return_pct NUMERIC NOT NULL DEFAULT 0,
			max_loss_pct NUMERIC NOT NULL DEFAULT 0,
			liquidity_usd NUMERIC NOT NULL DEFAULT 0,
			spread_pct NUMERIC NOT NULL DEFAULT 0,
			proposed_notional NUMERIC NOT NULL DEFAULT 0,
			selected_notional NUMERIC NOT NULL DEFAULT 0,
			reason TEXT NOT NULL DEFAULT '',
			reject_reason TEXT NOT NULL DEFAULT '',
			evidence JSONB NOT NULL DEFAULT '{}'::jsonb,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			dedupe_key TEXT NOT NULL UNIQUE
		)`,
		`CREATE INDEX idx_portfolio_opportunities_status_expires_at ON portfolio_opportunities (status, expires_at)`,
		`CREATE INDEX idx_portfolio_opportunities_strategy_id ON portfolio_opportunities (strategy_id)`,
		`CREATE INDEX idx_portfolio_opportunities_market_type_ticker ON portfolio_opportunities (market_type, ticker)`,
	}

	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+quoteIdent(schemaName)+` CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}

	cleanup := func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA `+quoteIdent(schemaName)+` CASCADE`)
		adminPool.Close()
	}

	return pool, cleanup
}

func quoteIdent(name string) string { return `"` + strings.ReplaceAll(name, `"`, `""`) + `"` }
