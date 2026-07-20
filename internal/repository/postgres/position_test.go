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
	assertContains(t, query, "ORDER BY p.opened_at DESC, p.id DESC")
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

	assertContains(t, query, "p.ticker = $1")
	assertContains(t, query, "p.side = $2")
	assertContains(t, query, "p.opened_at >= $3")
	assertContains(t, query, "p.opened_at <= $4")
	assertContains(t, query, "LIMIT $5 OFFSET $6")
}

func TestBuildPositionOpenQuery_FiltersOnlyOpenPositions(t *testing.T) {
	query, args := buildPositionOpenQuery(repository.PositionFilter{}, 10, 0)

	if len(args) != 2 {
		t.Fatalf("expected 2 args (limit, offset), got %d", len(args))
	}

	assertContains(t, query, "p.closed_at IS NULL")
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

	assertContains(t, query, "p.closed_at IS NULL")
	assertContains(t, query, "p.side = $1")
	assertContains(t, query, "LIMIT $2 OFFSET $3")
}

func TestBuildPositionScopedQuery_StrategyScope(t *testing.T) {
	strategyID := uuid.New()

	query, args := buildPositionScopedQuery("p.strategy_id", strategyID, repository.PositionFilter{
		Side: domain.PositionSideShort,
	}, 5, 10)

	if len(args) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "p.strategy_id = $1")
	assertContains(t, query, "p.side = $2")
	assertContains(t, query, "LIMIT $3 OFFSET $4")
	assertNotContains(t, query, "closed_at IS NULL")
}

func TestPositionRepoIntegration_CreateGetUpdateDelete(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	strategyID := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeStock)

	currentPrice := 185.50
	unrealizedPnL := 55.0
	stopLoss := 170.0
	takeProfit := 200.0

	position := &domain.Position{
		StrategyID:    &strategyID,
		MarketType:    domain.MarketTypeStock,
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
	if got.MarketType != domain.MarketTypeStock {
		t.Fatalf("expected MarketType stock, got %q", got.MarketType)
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
	openCount, err := repo.CountOpen(ctx, repository.PositionFilter{Ticker: position.Ticker})
	if err != nil {
		t.Fatalf("CountOpen() error = %v", err)
	}
	var sqlOpenCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM positions WHERE closed_at IS NULL AND ticker = $1`, position.Ticker).Scan(&sqlOpenCount); err != nil {
		t.Fatalf("direct SQL count error = %v", err)
	}
	if openCount != sqlOpenCount {
		t.Fatalf("CountOpen() = %d, direct SQL = %d", openCount, sqlOpenCount)
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
	strategyA := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeStock)
	strategyB := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeStock)
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

func TestPositionRepoIntegration_CountOpenByMarketAndGrossExposureParity(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	stockStrategy := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeStock)
	cryptoStrategy := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeCrypto)
	closedAt := time.Now().UTC()
	current := 11.0
	positions := []*domain.Position{
		{StrategyID: &stockStrategy, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 10, AvgEntry: 10, CurrentPrice: &current},
		{StrategyID: &stockStrategy, Ticker: "AAPL", Side: domain.PositionSideShort, Quantity: 5, AvgEntry: 8, RealizedPnL: 4, ClosedAt: &closedAt},
		{StrategyID: &cryptoStrategy, Ticker: "BTC", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 100, CurrentPrice: nil},
	}
	for _, pos := range positions {
		if err := repo.Create(ctx, pos); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}

	counts, err := repo.CountOpenByMarket(ctx, repository.PositionFilter{})
	if err != nil {
		t.Fatalf("CountOpenByMarket() error = %v", err)
	}
	if counts[domain.MarketTypeStock] != 1 || counts[domain.MarketTypeCrypto] != 1 {
		t.Fatalf("unexpected open counts by market: %#v", counts)
	}
	var sqlCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM positions p JOIN strategies s ON s.id = p.strategy_id WHERE p.closed_at IS NULL AND s.market_type = $1`, domain.MarketTypeStock).Scan(&sqlCount); err != nil {
		t.Fatalf("direct SQL count error = %v", err)
	}
	if counts[domain.MarketTypeStock] != sqlCount {
		t.Fatalf("CountOpenByMarket() = %d, direct SQL = %d", counts[domain.MarketTypeStock], sqlCount)
	}

	exposure, err := repo.GrossExposureOpen(ctx, repository.PositionFilter{})
	if err != nil {
		t.Fatalf("GrossExposureOpen() error = %v", err)
	}
	var sqlExposure float64
	if err := pool.QueryRow(ctx, `SELECT COALESCE(SUM(COALESCE(p.current_price, p.avg_entry) * p.quantity),0) FROM positions p WHERE p.closed_at IS NULL`).Scan(&sqlExposure); err != nil {
		t.Fatalf("direct exposure SQL error = %v", err)
	}
	if exposure != sqlExposure {
		t.Fatalf("GrossExposureOpen() = %v, direct SQL = %v", exposure, sqlExposure)
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
		`CREATE TYPE market_type AS ENUM ('stock', 'crypto', 'polymarket', 'kalshi', 'options')`,
		`CREATE TYPE position_side AS ENUM (
			'long',
			'short'
		)`,
		`CREATE TYPE trade_side AS ENUM (
			'buy',
			'sell'
		)`,
		`CREATE TABLE strategies (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			market_type market_type NOT NULL
		)`,
		`CREATE TABLE orders (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			broker TEXT,
			external_id TEXT,
			ticker TEXT NOT NULL,
			side trade_side NOT NULL,
			order_type TEXT NOT NULL DEFAULT 'market',
			quantity NUMERIC(20,8) NOT NULL,
			status TEXT NOT NULL DEFAULT 'filled',
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
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
		`CREATE TABLE trades (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			order_id UUID REFERENCES orders (id),
			position_id UUID REFERENCES positions (id),
			ticker TEXT NOT NULL,
			side trade_side NOT NULL,
			quantity NUMERIC(20,8) NOT NULL,
			price NUMERIC(20,8) NOT NULL,
			fee NUMERIC(20,8) NOT NULL DEFAULT 0,
			executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`,
		`CREATE TABLE position_provenance (
			position_id UUID PRIMARY KEY REFERENCES positions(id) ON DELETE CASCADE,
			broker TEXT NOT NULL CHECK (broker IN ('alpaca')),
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

func createTestPositionStrategy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, marketType domain.MarketType) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO strategies (market_type) VALUES ($1) RETURNING id`, marketType).Scan(&id); err != nil {
		t.Fatalf("failed to create test strategy: %v", err)
	}

	return id
}

func TestPositionRepoIntegration_ListOpenAlpacaOwned(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	strategyID := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeStock)
	openAt := time.Now().UTC()
	proven := &domain.Position{StrategyID: &strategyID, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 100, OpenedAt: openAt}
	local := &domain.Position{StrategyID: &strategyID, MarketType: domain.MarketTypeStock, Ticker: "PAPER", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 50, OpenedAt: openAt}
	for _, pos := range []*domain.Position{proven, local} {
		if err := repo.Create(ctx, pos); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	orderID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, broker, external_id, ticker, side, quantity, status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, orderID, "alpaca", "alp-1", "AAPL", domain.OrderSideBuy, 1, "filled"); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trades (order_id, position_id, ticker, side, quantity, price) VALUES ($1,$2,$3,$4,$5,$6)`, orderID, proven.ID, "AAPL", domain.OrderSideBuy, 1, 100); err != nil {
		t.Fatalf("insert trade: %v", err)
	}
	open, err := repo.ListOpenAlpacaOwned(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListOpenAlpacaOwned() error = %v", err)
	}
	if len(open) != 1 || open[0].Ticker != "AAPL" {
		t.Fatalf("ListOpenAlpacaOwned() = %#v, want only proven alpaca position", open)
	}
}

func TestPositionRepoIntegration_CreateAlpacaOwnedDedupesAndRollsBack(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	position := &domain.Position{MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 101}
	if err := repo.CreateAlpacaOwned(ctx, position); err != nil {
		t.Fatalf("CreateAlpacaOwned() error = %v", err)
	}
	firstID := position.ID
	if position.MarketType != domain.MarketTypeStock {
		t.Fatalf("expected truthful market type stock, got %q", position.MarketType)
	}
	if err := repo.CreateAlpacaOwned(ctx, &domain.Position{MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 101}); err != nil {
		t.Fatalf("CreateAlpacaOwned() dedupe error = %v", err)
	}
	if position.ID != firstID {
		t.Fatalf("expected first call to preserve ID, got %s vs %s", position.ID, firstID)
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM positions`).Scan(&count); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 position, got %d", count)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM position_provenance`).Scan(&count); err != nil {
		t.Fatalf("count provenance: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 provenance row, got %d", count)
	}
}

func TestPositionRepoIntegration_ListOpenAlpacaOwnedIncludesProvenanceAndLegacy(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	strategyID := createTestPositionStrategy(t, ctx, pool, domain.MarketTypeStock)
	proven := &domain.Position{StrategyID: &strategyID, MarketType: domain.MarketTypeStock, Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 100}
	legacy := &domain.Position{StrategyID: &strategyID, MarketType: domain.MarketTypeStock, Ticker: "MSFT", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 200}
	ignored := &domain.Position{StrategyID: &strategyID, MarketType: domain.MarketTypeStock, Ticker: "PAPER", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 50}
	for _, pos := range []*domain.Position{proven, legacy, ignored} {
		if err := repo.Create(ctx, pos); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO position_provenance (position_id, broker) VALUES ($1, 'alpaca')`, proven.ID); err != nil {
		t.Fatalf("insert provenance: %v", err)
	}
	orderID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, broker, external_id, ticker, side, quantity, status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, orderID, "alpaca", "alp-1", "MSFT", domain.OrderSideBuy, 1, "filled"); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trades (order_id, position_id, ticker, side, quantity, price) VALUES ($1,$2,$3,$4,$5,$6)`, orderID, legacy.ID, "MSFT", domain.OrderSideBuy, 1, 200); err != nil {
		t.Fatalf("insert trade: %v", err)
	}
	open, err := repo.ListOpenAlpacaOwned(ctx, 10, 0)
	if err != nil {
		t.Fatalf("ListOpenAlpacaOwned() error = %v", err)
	}
	if len(open) != 2 {
		t.Fatalf("expected 2 alpaca-owned positions, got %#v", open)
	}
}

func TestPositionRepoIntegration_CreateAlpacaOwnedUsesTransactionalRollback(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newPositionIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewPositionRepo(pool)
	_, _ = pool.Exec(ctx, `CREATE OR REPLACE FUNCTION fail_position_provenance() RETURNS trigger AS $$ BEGIN RAISE EXCEPTION 'boom'; END; $$ LANGUAGE plpgsql`)
	_, _ = pool.Exec(ctx, `CREATE TRIGGER position_provenance_fail BEFORE INSERT ON position_provenance FOR EACH ROW EXECUTE FUNCTION fail_position_provenance()`)
	defer pool.Exec(ctx, `DROP TRIGGER IF EXISTS position_provenance_fail ON position_provenance`)
	pos := &domain.Position{MarketType: domain.MarketTypeStock, Ticker: "TSLA", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 300}
	if err := repo.CreateAlpacaOwned(ctx, pos); err == nil {
		t.Fatal("expected error")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM positions`).Scan(&count); err != nil {
		t.Fatalf("count positions: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected rollback with 0 positions, got %d", count)
	}
}
