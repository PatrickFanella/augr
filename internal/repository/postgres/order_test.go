package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildOrderListQuery_NoFilters(t *testing.T) {
	query, args := buildOrderListQuery(repository.OrderFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}
	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM orders")
	assertContains(t, query, "ORDER BY created_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildOrderListQuery_AllFilters(t *testing.T) {
	submittedAfter := time.Date(2026, 3, 20, 14, 0, 0, 0, time.UTC)
	submittedBefore := time.Date(2026, 3, 21, 14, 0, 0, 0, time.UTC)

	query, args := buildOrderListQuery(repository.OrderFilter{
		Ticker:          "AAPL",
		Broker:          "alpaca",
		Side:            domain.OrderSideBuy,
		OrderType:       domain.OrderTypeLimit,
		Status:          domain.OrderStatusSubmitted,
		SubmittedAfter:  &submittedAfter,
		SubmittedBefore: &submittedBefore,
	}, 25, 50)

	if len(args) != 9 {
		t.Fatalf("expected 9 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "ticker = $1")
	assertContains(t, query, "broker = $2")
	assertContains(t, query, "side = $3")
	assertContains(t, query, "order_type = $4")
	assertContains(t, query, "status = $5")
	assertContains(t, query, "submitted_at >= $6")
	assertContains(t, query, "submitted_at <= $7")
	assertContains(t, query, "LIMIT $8 OFFSET $9")
}

func TestBuildOrderScopedListQuery_StrategyScopeAndPartialFilters(t *testing.T) {
	strategyID := uuid.New()

	query, args := buildOrderScopedListQuery("strategy_id", strategyID, repository.OrderFilter{
		Status: domain.OrderStatusFilled,
		Broker: "ibkr",
	}, 5, 10)

	if len(args) != 5 {
		t.Fatalf("expected 5 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "strategy_id = $1")
	assertContains(t, query, "broker = $2")
	assertContains(t, query, "status = $3")
	assertContains(t, query, "LIMIT $4 OFFSET $5")
	assertNotContains(t, query, "ticker =")
	assertNotContains(t, query, "order_type =")
}

func TestOrderRepoIntegration_CreateGetUpdateDelete(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newOrderTradeIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewOrderRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	runID := uuid.New()
	submittedAt := time.Date(2026, 3, 21, 13, 30, 0, 0, time.UTC)
	limitPrice := 185.25

	order := &domain.Order{
		StrategyID:    &strategyID,
		PipelineRunID: &runID,
		ExternalID:    "broker-123",
		Ticker:        "AAPL",
		Side:          domain.OrderSideBuy,
		OrderType:     domain.OrderTypeLimit,
		Quantity:      10,
		LimitPrice:    &limitPrice,
		Status:        domain.OrderStatusPending,
		Broker:        "alpaca",
		SubmittedAt:   &submittedAt,
	}

	if err := repo.Create(ctx, order); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if order.ID == uuid.Nil {
		t.Fatal("expected Create() to populate ID")
	}
	if order.CreatedAt.IsZero() {
		t.Fatal("expected Create() to populate CreatedAt")
	}

	got, err := repo.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.StrategyID == nil || *got.StrategyID != strategyID {
		t.Fatalf("expected StrategyID %s, got %v", strategyID, got.StrategyID)
	}
	if got.PipelineRunID == nil || *got.PipelineRunID != runID {
		t.Fatalf("expected PipelineRunID %s, got %v", runID, got.PipelineRunID)
	}
	if got.ExternalID != order.ExternalID {
		t.Errorf("expected ExternalID %q, got %q", order.ExternalID, got.ExternalID)
	}
	if got.Broker != order.Broker {
		t.Errorf("expected Broker %q, got %q", order.Broker, got.Broker)
	}
	if got.LimitPrice == nil || *got.LimitPrice != limitPrice {
		t.Fatalf("expected LimitPrice %.2f, got %v", limitPrice, got.LimitPrice)
	}
	if got.Status != domain.OrderStatusPending {
		t.Errorf("expected pending status, got %q", got.Status)
	}

	filledAt := submittedAt.Add(2 * time.Minute)
	filledAvgPrice := 185.10
	order.FilledQuantity = 10
	order.FilledAvgPrice = &filledAvgPrice
	order.Status = domain.OrderStatusFilled
	order.FilledAt = &filledAt

	if err := repo.Update(ctx, order); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	updated, err := repo.Get(ctx, order.ID)
	if err != nil {
		t.Fatalf("Get() after Update error = %v", err)
	}
	if updated.Status != domain.OrderStatusFilled {
		t.Errorf("expected filled status, got %q", updated.Status)
	}
	if updated.FilledQuantity != 10 {
		t.Errorf("expected filled quantity 10, got %v", updated.FilledQuantity)
	}
	if updated.FilledAvgPrice == nil || *updated.FilledAvgPrice != filledAvgPrice {
		t.Fatalf("expected FilledAvgPrice %.2f, got %v", filledAvgPrice, updated.FilledAvgPrice)
	}
	if updated.FilledAt == nil || !updated.FilledAt.Equal(filledAt) {
		t.Fatalf("expected FilledAt %v, got %v", filledAt, updated.FilledAt)
	}

	if err := repo.Delete(ctx, order.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.Get(ctx, order.ID)
	if err == nil {
		t.Fatal("expected Get() after Delete to return an error")
	}
	if !strings.Contains(err.Error(), ErrNotFound.Error()) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestOrderRepoIntegration_ListAndScopedFilters(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newOrderTradeIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewOrderRepo(pool)
	strategyA := createTestStrategy(t, ctx, pool)
	strategyB := createTestStrategy(t, ctx, pool)
	runA := uuid.New()
	runB := uuid.New()
	baseTime := time.Date(2026, 3, 21, 9, 0, 0, 0, time.UTC)

	orderA := &domain.Order{
		StrategyID:    &strategyA,
		PipelineRunID: &runA,
		Ticker:        "AAPL",
		Side:          domain.OrderSideBuy,
		OrderType:     domain.OrderTypeLimit,
		Quantity:      10,
		Status:        domain.OrderStatusSubmitted,
		Broker:        "alpaca",
		SubmittedAt:   timePtr(baseTime),
	}
	orderB := &domain.Order{
		StrategyID:    &strategyA,
		PipelineRunID: &runA,
		Ticker:        "AAPL",
		Side:          domain.OrderSideBuy,
		OrderType:     domain.OrderTypeLimit,
		Quantity:      5,
		Status:        domain.OrderStatusFilled,
		Broker:        "alpaca",
		SubmittedAt:   timePtr(baseTime.Add(30 * time.Minute)),
	}
	orderC := &domain.Order{
		StrategyID:    &strategyB,
		PipelineRunID: &runB,
		Ticker:        "MSFT",
		Side:          domain.OrderSideSell,
		OrderType:     domain.OrderTypeMarket,
		Quantity:      7,
		Status:        domain.OrderStatusCancelled,
		Broker:        "ibkr",
		SubmittedAt:   timePtr(baseTime.Add(60 * time.Minute)),
	}

	for _, order := range []*domain.Order{orderA, orderB, orderC} {
		if err := repo.Create(ctx, order); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	listed, err := repo.List(ctx, repository.OrderFilter{
		Ticker: "AAPL",
		Broker: "alpaca",
		Side:   domain.OrderSideBuy,
	}, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 AAPL/alpaca orders, got %d", len(listed))
	}

	strategyOrders, err := repo.GetByStrategy(ctx, strategyA, repository.OrderFilter{
		Status: domain.OrderStatusFilled,
		Broker: "alpaca",
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByStrategy() error = %v", err)
	}
	if len(strategyOrders) != 1 {
		t.Fatalf("expected 1 filled strategy order, got %d", len(strategyOrders))
	}
	if strategyOrders[0].ID != orderB.ID {
		t.Fatalf("expected orderB, got %s", strategyOrders[0].ID)
	}

	runOrders, err := repo.GetByRun(ctx, runA, repository.OrderFilter{
		SubmittedAfter: timePtr(baseTime.Add(15 * time.Minute)),
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByRun() error = %v", err)
	}
	if len(runOrders) != 1 {
		t.Fatalf("expected 1 run-scoped order, got %d", len(runOrders))
	}
	if runOrders[0].ID != orderB.ID {
		t.Fatalf("expected orderB from run filter, got %s", runOrders[0].ID)
	}

	page, err := repo.List(ctx, repository.OrderFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("List() pagination error = %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 orders on first page, got %d", len(page))
	}
}

func newOrderTradeIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_order_trade_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TYPE order_status AS ENUM (
			'pending',
			'submitted',
			'partial',
			'filled',
			'cancelled',
			'rejected'
		)`,
		`CREATE TYPE trade_side AS ENUM (
			'buy',
			'sell'
		)`,
		`CREATE TYPE order_type AS ENUM (
			'market',
			'limit',
			'stop',
			'stop_limit'
		)`,
		`CREATE TYPE position_side AS ENUM (
			'long',
			'short'
		)`,
		`CREATE TABLE strategies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid()
		)`,
		`CREATE TABLE positions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			strategy_id UUID REFERENCES strategies (id),
			ticker TEXT NOT NULL,
			side position_side NOT NULL,
			quantity NUMERIC(20, 8) NOT NULL,
			avg_entry NUMERIC(20, 8) NOT NULL,
			realized_pnl NUMERIC(20, 8) NOT NULL DEFAULT 0,
			opened_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			closed_at TIMESTAMPTZ
		)`,
		`CREATE TABLE orders (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			strategy_id UUID REFERENCES strategies (id),
			pipeline_run_id UUID,
			external_id TEXT,
			ticker TEXT NOT NULL,
			side trade_side NOT NULL,
			order_type order_type NOT NULL,
			quantity NUMERIC(20, 8) NOT NULL,
			limit_price NUMERIC(20, 8),
			stop_price NUMERIC(20, 8),
			filled_quantity NUMERIC(20, 8) NOT NULL DEFAULT 0,
			filled_avg_price NUMERIC(20, 8),
			status order_status NOT NULL DEFAULT 'pending',
			broker TEXT,
			submitted_at TIMESTAMPTZ,
			filled_at TIMESTAMPTZ,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE trades (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			order_id UUID REFERENCES orders (id),
			position_id UUID REFERENCES positions (id),
			ticker TEXT NOT NULL,
			side trade_side NOT NULL,
			quantity NUMERIC(20, 8) NOT NULL,
			price NUMERIC(20, 8) NOT NULL,
			fee NUMERIC(20, 8) NOT NULL DEFAULT 0,
			executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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

func createTestStrategy(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO strategies DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("failed to create test strategy: %v", err)
	}

	return id
}

func timePtr(t time.Time) *time.Time {
	return &t
}
