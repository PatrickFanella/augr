package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildPositionListQuery_NoFilters(t *testing.T) {
	query, args := buildPositionListQuery(repository.PositionFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	if args[0] != 10 {
		t.Errorf("expected limit=10, got %v", args[0])
	}
	if args[1] != 0 {
		t.Errorf("expected offset=0, got %v", args[1])
	}

	assertContains(t, query, "FROM positions")
	assertContains(t, query, "ORDER BY opened_at DESC, id DESC")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
	assertNotContains(t, query, "WHERE")
}

func TestBuildPositionListQuery_AllFilters(t *testing.T) {
	openedAfter := time.Date(2026, 3, 20, 0, 0, 0, 0, time.UTC)
	openedBefore := time.Date(2026, 3, 21, 0, 0, 0, 0, time.UTC)

	query, args := buildPositionListQuery(repository.PositionFilter{
		Ticker:       "AAPL",
		Side:         domain.PositionSideLong,
		OpenedAfter:  &openedAfter,
		OpenedBefore: &openedBefore,
	}, 25, 50)

	if len(args) != 6 {
		t.Fatalf("expected 6 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "ticker = $1")
	assertContains(t, query, "side = $2")
	assertContains(t, query, "opened_at >= $3")
	assertContains(t, query, "opened_at <= $4")
	assertContains(t, query, "LIMIT $5 OFFSET $6")
}

func TestBuildPositionOpenQuery_FiltersOnlyOpenPositions(t *testing.T) {
	query, args := buildPositionOpenQuery(repository.PositionFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	assertContains(t, query, "closed_at IS NULL")
	assertNotContains(t, query, "closed_at = ")
	assertContains(t, query, "LIMIT $1 OFFSET $2")
}

func TestBuildPositionOpenQuery_WithSideFilter(t *testing.T) {
	query, args := buildPositionOpenQuery(repository.PositionFilter{
		Side: domain.PositionSideLong,
	}, 5, 0)

	if len(args) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "closed_at IS NULL")
	assertContains(t, query, "side = $1")
	assertContains(t, query, "LIMIT $2 OFFSET $3")
}

func TestBuildPositionScopedQuery_StrategyScope(t *testing.T) {
	strategyID := uuid.New()

	query, args := buildPositionScopedQuery("strategy_id", strategyID, repository.PositionFilter{
		Side: domain.PositionSideShort,
	}, 5, 10)

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "strategy_id = $1")
	assertContains(t, query, "side = $2")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
	assertNotContains(t, query, "closed_at IS NULL")
}

func TestPositionRepoIntegration_CreateGetUpdateDelete(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	strategyID := createTestPositionStrategy(t, ctx, pool)

	currentPrice := 185.50
	unrealizedPnL := 55.0
	stopLoss := 170.0
	takeProfit := 200.0

	position := &domain.Position{
		StrategyID:    &strategyID,
		Ticker:        "AAPL",
		Side:          domain.PositionSideLong,
		Quantity:      10,
		AvgEntry:      180.0,
		CurrentPrice:  &currentPrice,
		UnrealizedPnL: &unrealizedPnL,
		RealizedPnL:   0,
		StopLoss:      &stopLoss,
		TakeProfit:    &takeProfit,
	}

	if err := repo.Create(ctx, position); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if position.ID == uuid.Nil {
		t.Fatal("expected Create() to populate ID")
	}
	if position.OpenedAt.IsZero() {
		t.Fatal("expected Create() to populate OpenedAt")
	}

	got, err := repo.Get(ctx, position.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got.StrategyID == nil || *got.StrategyID != strategyID {
		t.Fatalf("expected StrategyID %s, got %v", strategyID, got.StrategyID)
	}
	if got.Ticker != position.Ticker {
		t.Errorf("expected Ticker %q, got %q", position.Ticker, got.Ticker)
	}
	if got.Side != domain.PositionSideLong {
		t.Errorf("expected Side long, got %q", got.Side)
	}
	if got.Quantity != 10 {
		t.Errorf("expected Quantity 10, got %v", got.Quantity)
	}
	if got.AvgEntry != 180.0 {
		t.Errorf("expected AvgEntry 180.0, got %v", got.AvgEntry)
	}
	if got.CurrentPrice == nil || *got.CurrentPrice != currentPrice {
		t.Fatalf("expected CurrentPrice %.2f, got %v", currentPrice, got.CurrentPrice)
	}
	if got.UnrealizedPnL == nil || *got.UnrealizedPnL != unrealizedPnL {
		t.Fatalf("expected UnrealizedPnL %.2f, got %v", unrealizedPnL, got.UnrealizedPnL)
	}
	if got.StopLoss == nil || *got.StopLoss != stopLoss {
		t.Fatalf("expected StopLoss %.2f, got %v", stopLoss, got.StopLoss)
	}
	if got.TakeProfit == nil || *got.TakeProfit != takeProfit {
		t.Fatalf("expected TakeProfit %.2f, got %v", takeProfit, got.TakeProfit)
	}
	if got.ClosedAt != nil {
		t.Errorf("expected ClosedAt to be nil for open position, got %v", got.ClosedAt)
	}

	// Update: close the position with realized P&L
	newCurrentPrice := 195.0
	realizedPnL := 150.0
	closedAt := time.Now().UTC().Truncate(time.Microsecond)
	position.CurrentPrice = &newCurrentPrice
	position.RealizedPnL = realizedPnL
	position.UnrealizedPnL = nil
	position.ClosedAt = &closedAt

	if err := repo.Update(ctx, position); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	updated, err := repo.Get(ctx, position.ID)
	if err != nil {
		t.Fatalf("Get() after Update error = %v", err)
	}
	if updated.RealizedPnL != realizedPnL {
		t.Errorf("expected RealizedPnL %.2f, got %v", realizedPnL, updated.RealizedPnL)
	}
	if updated.ClosedAt == nil {
		t.Fatal("expected ClosedAt to be set after closing position")
	}
	if updated.UnrealizedPnL != nil {
		t.Errorf("expected UnrealizedPnL to be nil after closing, got %v", updated.UnrealizedPnL)
	}

	if err := repo.Delete(ctx, position.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	_, err = repo.Get(ctx, position.ID)
	if err == nil {
		t.Fatal("expected Get() after Delete to return an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after Delete, got %v", err)
	}
}

func TestPositionRepoIntegration_GetNotFound(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)

	_, err := repo.Get(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected Get() with unknown ID to return an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPositionRepoIntegration_UpdateNotFound(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)

	err := repo.Update(ctx, &domain.Position{
		ID:       uuid.New(),
		Ticker:   "AAPL",
		Side:     domain.PositionSideLong,
		Quantity: 1,
		AvgEntry: 100.0,
	})
	if err == nil {
		t.Fatal("expected Update() with unknown ID to return an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPositionRepoIntegration_DeleteNotFound(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)

	err := repo.Delete(ctx, uuid.New())
	if err == nil {
		t.Fatal("expected Delete() with unknown ID to return an error")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestPositionRepoIntegration_ListGetOpenGetByStrategy(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	strategyA := createTestPositionStrategy(t, ctx, pool)
	strategyB := createTestPositionStrategy(t, ctx, pool)
	closedAt := time.Now().UTC()

	// posA: open, long, AAPL, strategyA
	posA := &domain.Position{
		StrategyID: &strategyA,
		Ticker:     "AAPL",
		Side:       domain.PositionSideLong,
		Quantity:   10,
		AvgEntry:   180.0,
	}
	// posB: open, short, MSFT, strategyA
	posB := &domain.Position{
		StrategyID: &strategyA,
		Ticker:     "MSFT",
		Side:       domain.PositionSideShort,
		Quantity:   5,
		AvgEntry:   350.0,
	}
	// posC: closed, long, AAPL, strategyB
	posC := &domain.Position{
		StrategyID:  &strategyB,
		Ticker:      "AAPL",
		Side:        domain.PositionSideLong,
		Quantity:    8,
		AvgEntry:    175.0,
		RealizedPnL: 80.0,
		ClosedAt:    &closedAt,
	}

	for _, pos := range []*domain.Position{posA, posB, posC} {
		if err := repo.Create(ctx, pos); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	// List with ticker filter: should return posA and posC
	listed, err := repo.List(ctx, repository.PositionFilter{Ticker: "AAPL"}, 10, 0)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("expected 2 AAPL positions, got %d", len(listed))
	}

	// GetOpen: should return posA and posB (posC is closed)
	open, err := repo.GetOpen(ctx, repository.PositionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("GetOpen() error = %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 open positions, got %d", len(open))
	}
	for _, pos := range open {
		if pos.ClosedAt != nil {
			t.Errorf("GetOpen() returned a closed position: %s", pos.ID)
		}
	}

	// GetOpen with side filter: only posA is open and long
	openLong, err := repo.GetOpen(ctx, repository.PositionFilter{Side: domain.PositionSideLong}, 10, 0)
	if err != nil {
		t.Fatalf("GetOpen() with side filter error = %v", err)
	}
	if len(openLong) != 1 {
		t.Fatalf("expected 1 open long position, got %d", len(openLong))
	}
	if openLong[0].ID != posA.ID {
		t.Fatalf("expected posA, got %s", openLong[0].ID)
	}

	// GetByStrategy for strategyA: posA and posB
	strategyAPositions, err := repo.GetByStrategy(ctx, strategyA, repository.PositionFilter{}, 10, 0)
	if err != nil {
		t.Fatalf("GetByStrategy() error = %v", err)
	}
	if len(strategyAPositions) != 2 {
		t.Fatalf("expected 2 positions for strategyA, got %d", len(strategyAPositions))
	}

	// GetByStrategy with side filter
	strategyALong, err := repo.GetByStrategy(ctx, strategyA, repository.PositionFilter{Side: domain.PositionSideLong}, 10, 0)
	if err != nil {
		t.Fatalf("GetByStrategy() with side filter error = %v", err)
	}
	if len(strategyALong) != 1 {
		t.Fatalf("expected 1 long position for strategyA, got %d", len(strategyALong))
	}
	if strategyALong[0].ID != posA.ID {
		t.Fatalf("expected posA, got %s", strategyALong[0].ID)
	}

	// Pagination
	page, err := repo.List(ctx, repository.PositionFilter{}, 2, 0)
	if err != nil {
		t.Fatalf("List() pagination error = %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 positions on first page, got %d", len(page))
	}
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newPositionIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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

	schemaName := "integration_position_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TYPE position_side AS ENUM (
			'long',
			'short'
		)`,
		`CREATE TABLE strategies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid()
		)`,
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

func createTestPositionStrategy(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO strategies DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("failed to create test strategy: %v", err)
	}

	return id
}
