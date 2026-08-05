package postgres

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestBuildQueuedForAllocationQuery(t *testing.T) {
	query := opportunitySelectSQL + ` WHERE status = $1 AND expires_at > $2 ORDER BY expires_at ASC, created_at ASC, id ASC`
	assertContains(t, query, "WHERE status = $1 AND expires_at > $2")
	assertContains(t, query, "ORDER BY expires_at ASC, created_at ASC, id ASC")
	_ = fmt.Sprintf
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
		PredictionSide:    "YES",
		Signal:            domain.PipelineSignalBuy,
		Status:            domain.OpportunityStatusQueued,
		Score:             &initialScore,
		Confidence:        0.87,
		EdgePct:           2.5,
		ExpectedReturnPct: 4.5,
		MaxLossPct:        1.0,
		EntryPrice:        150.25,
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
	if got.EntryPrice != 150.25 {
		t.Fatalf("unexpected entry price roundtrip: %v", got.EntryPrice)
	}
	if got.PredictionSide != "YES" {
		t.Fatalf("unexpected prediction side roundtrip: %q", got.PredictionSide)
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
	if updated.Status != domain.OpportunityStatusQueued {
		t.Fatalf("expected queued status to remain queued, got %s", updated.Status)
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

	opportunity.Status = domain.OpportunityStatusQueued
	opportunity.Confidence = 0.99
	opportunity.Reason = "new same-day signal"
	if err := repo.UpsertQueuedByDedupeKey(ctx, opportunity); err != nil {
		t.Fatalf("UpsertQueuedByDedupeKey() after terminal status error = %v", err)
	}
	terminal, err := repo.Get(ctx, opportunity.ID)
	if err != nil {
		t.Fatalf("Get() after terminal upsert error = %v", err)
	}
	if terminal.Status != domain.OpportunityStatusRejected || terminal.Reason == "new same-day signal" || terminal.Confidence == 0.99 {
		t.Fatalf("terminal opportunity was resurrected or refreshed: %+v", terminal)
	}
}

func TestOpportunityRepoIntegration_UpsertQueuedByDedupeKeyDoesNotRequeueSelected(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newOpportunityIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewOpportunityRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	expiresAt := time.Date(2026, 6, 20, 15, 0, 0, 0, time.UTC)
	score := 1.0
	opportunity := &domain.Opportunity{
		StrategyID:     strategyID,
		MarketType:     domain.MarketTypeStock,
		Ticker:         "AAPL",
		Side:           domain.OrderSideBuy,
		PredictionSide: "YES",
		Signal:         domain.PipelineSignalBuy,
		Status:         domain.OpportunityStatusSelected,
		Score:          &score,
		Confidence:     0.5,
		ExpiresAt:      expiresAt,
		DedupeKey:      "selected-dedupe",
	}
	if err := repo.Create(ctx, opportunity); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	originalID := opportunity.ID

	refreshScore := 2.0
	opportunity.Status = domain.OpportunityStatusQueued
	opportunity.Score = &refreshScore
	opportunity.Confidence = 0.9
	if err := repo.UpsertQueuedByDedupeKey(ctx, opportunity); err != nil {
		t.Fatalf("UpsertQueuedByDedupeKey() error = %v", err)
	}
	selected, err := repo.Get(ctx, originalID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if selected.Status != domain.OpportunityStatusSelected || selected.Confidence != 0.5 {
		t.Fatalf("selected opportunity was overwritten: %+v", selected)
	}
	if selected.Score == nil || *selected.Score != score {
		t.Fatalf("selected opportunity score changed: %+v", selected.Score)
	}

	queued := &domain.Opportunity{
		StrategyID:     strategyID,
		MarketType:     domain.MarketTypeStock,
		Ticker:         "MSFT",
		Side:           domain.OrderSideBuy,
		PredictionSide: "YES",
		Signal:         domain.PipelineSignalBuy,
		Status:         domain.OpportunityStatusQueued,
		Score:          &score,
		Confidence:     0.1,
		ExpiresAt:      expiresAt,
		DedupeKey:      "queued-dedupe",
	}
	if err := repo.Create(ctx, queued); err != nil {
		t.Fatalf("Create() queued error = %v", err)
	}
	queued.Confidence = 0.8
	queued.Reason = "refreshed"
	if err := repo.UpsertQueuedByDedupeKey(ctx, queued); err != nil {
		t.Fatalf("UpsertQueuedByDedupeKey() queued error = %v", err)
	}
	refreshed, err := repo.Get(ctx, queued.ID)
	if err != nil {
		t.Fatalf("Get() refreshed error = %v", err)
	}
	if refreshed.Status != domain.OpportunityStatusQueued || refreshed.Confidence != 0.8 || refreshed.Reason != "refreshed" {
		t.Fatalf("queued opportunity was not refreshed: %+v", refreshed)
	}
}

func TestOpportunityRepo_ExpireQueuedBefore(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newOpportunityIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewOpportunityRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	seedOpportunity := func(status domain.OpportunityStatus, expiresAt time.Time, dedupe string) uuid.UUID {
		op := &domain.Opportunity{StrategyID: strategyID, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, PredictionSide: "YES", Signal: domain.PipelineSignalBuy, Status: status, Confidence: 1, ExpiresAt: expiresAt, DedupeKey: dedupe}
		if err := repo.Create(ctx, op); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		return op.ID
	}
	pastQueuedID := seedOpportunity(domain.OpportunityStatusQueued, now.Add(-time.Hour), "past-queued")
	_ = seedOpportunity(domain.OpportunityStatusQueued, now.Add(time.Hour), "future-queued")
	_ = seedOpportunity(domain.OpportunityStatusSelected, now.Add(-time.Hour), "past-selected")

	changed, err := repo.ExpireQueuedBefore(ctx, now)
	if err != nil {
		t.Fatalf("ExpireQueuedBefore() error = %v", err)
	}
	if changed != 1 {
		t.Fatalf("changed = %d, want 1", changed)
	}

	got, err := repo.Get(ctx, pastQueuedID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Status != domain.OpportunityStatusExpired || got.RejectReason != "expired_before_allocation" {
		t.Fatalf("unexpected expired opportunity: %+v", got)
	}
}

func TestOpportunityRepo_ListQueuedForAllocation(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newOpportunityIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewOpportunityRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	asOf := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	for i := 0; i < 205; i++ {
		expires := asOf.Add(time.Duration(i+1) * time.Minute)
		op := &domain.Opportunity{StrategyID: strategyID, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.OrderSideBuy, PredictionSide: "YES", Signal: domain.PipelineSignalBuy, Status: domain.OpportunityStatusQueued, Confidence: 1, ExpiresAt: expires, CreatedAt: asOf.Add(-time.Duration(i) * time.Minute), DedupeKey: fmt.Sprintf("queued-%03d", i)}
		if err := repo.Create(ctx, op); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	for _, status := range []domain.OpportunityStatus{domain.OpportunityStatusQueued, domain.OpportunityStatusExpired} {
		op := &domain.Opportunity{StrategyID: strategyID, MarketType: domain.MarketTypeStock, Ticker: "MSFT", Side: domain.OrderSideBuy, PredictionSide: "YES", Signal: domain.PipelineSignalBuy, Status: status, Confidence: 1, ExpiresAt: asOf, DedupeKey: fmt.Sprintf("excluded-%s", status)}
		if err := repo.Create(ctx, op); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	items, err := repo.ListQueuedForAllocation(ctx, asOf)
	if err != nil {
		t.Fatalf("ListQueuedForAllocation() error = %v", err)
	}
	if len(items) != 205 {
		t.Fatalf("len = %d, want 205", len(items))
	}
	for i, item := range items {
		if item.ExpiresAt.Before(asOf) || !item.ExpiresAt.After(asOf) {
			t.Fatalf("item %d expires_at not after asOf: %v", i, item.ExpiresAt)
		}
		if item.DedupeKey != fmt.Sprintf("queued-%03d", i) {
			t.Fatalf("item %d dedupe=%s", i, item.DedupeKey)
		}
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
			prediction_side TEXT NOT NULL DEFAULT '',
			signal pipeline_signal NOT NULL,
			status TEXT NOT NULL CHECK (status IN ('queued', 'selected', 'rejected', 'expired', 'executed')),
			score NUMERIC,
			confidence NUMERIC NOT NULL DEFAULT 0,
			edge_pct NUMERIC NOT NULL DEFAULT 0,
			expected_return_pct NUMERIC NOT NULL DEFAULT 0,
			max_loss_pct NUMERIC NOT NULL DEFAULT 0,
			entry_price NUMERIC NOT NULL DEFAULT 0,
			liquidity_usd NUMERIC NOT NULL DEFAULT 0,
			market_cap_usd NUMERIC NOT NULL DEFAULT 0,
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
		`CREATE TABLE allocation_decisions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			opportunity_id UUID REFERENCES portfolio_opportunities (id),
			strategy_id UUID REFERENCES strategies (id),
			mode TEXT NOT NULL CHECK (mode IN ('shadow', 'paper')),
			action TEXT NOT NULL CHECK (action IN ('shadow_selected', 'shadow_rejected', 'paper_order_intent', 'execution_rejected', 'executed')),
			score NUMERIC NOT NULL DEFAULT 0,
			notional_usd NUMERIC NOT NULL DEFAULT 0,
			quantity NUMERIC NOT NULL DEFAULT 0,
			reasons TEXT[] NOT NULL DEFAULT '{}',
			created_order_id UUID REFERENCES orders (id),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
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
