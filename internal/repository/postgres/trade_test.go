package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func TestBuildTradeScopedListQuery_AllFilters(t *testing.T) {
	orderID := uuid.New()
	executedAfter := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	executedBefore := time.Date(2026, 3, 21, 12, 0, 0, 0, time.UTC)

	query, args := buildTradeScopedListQuery("order_id", orderID, repository.TradeFilter{
		Ticker:         "AAPL",
		Side:           domain.OrderSideBuy,
		ExecutedAfter:  &executedAfter,
		ExecutedBefore: &executedBefore,
	}, 20, 40)

	if len(args) != 7 {
		t.Fatalf("expected 7 args, got %d: %v", len(args), args)
	}

	assertContains(t, query, "FROM trades")
	assertContains(t, query, "order_id = $1")
	assertContains(t, query, "ticker = $2")
	assertContains(t, query, "side = $3")
	assertContains(t, query, "executed_at >= $4")
	assertContains(t, query, "executed_at <= $5")
	assertContains(t, query, "LIMIT $6 OFFSET $7")
	assertContains(t, query, "ORDER BY executed_at DESC, created_at DESC, id DESC")
}

func TestTradeRepoIntegration_CreateGetByOrderAndPosition(t *testing.T) {
	t.Helper()

	ctx := context.Background()
	pool, cleanup := newOrderTradeIntegrationPool(t, ctx)
	defer cleanup()

	orderRepo := NewOrderRepo(pool)
	tradeRepo := NewTradeRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	orderID := createTestOrder(t, ctx, orderRepo, strategyID)
	positionID := createTestPosition(t, ctx, pool, strategyID)
	otherPositionID := createTestPosition(t, ctx, pool, strategyID)
	baseTime := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)

	tradeA := &domain.Trade{
		OrderID:    &orderID,
		PositionID: &positionID,
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		Quantity:   5,
		Price:      185.00,
		Fee:        0.25,
		ExecutedAt: baseTime,
	}
	tradeB := &domain.Trade{
		OrderID:    &orderID,
		PositionID: &positionID,
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		Quantity:   5,
		Price:      185.15,
		Fee:        0.30,
		ExecutedAt: baseTime.Add(5 * time.Minute),
	}
	tradeC := &domain.Trade{
		OrderID:    &orderID,
		PositionID: &otherPositionID,
		Ticker:     "MSFT",
		Side:       domain.OrderSideSell,
		Quantity:   2,
		Price:      410.00,
		Fee:        0.10,
		ExecutedAt: baseTime.Add(10 * time.Minute),
	}

	for _, trade := range []*domain.Trade{tradeA, tradeB, tradeC} {
		if err := tradeRepo.Create(ctx, trade); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if trade.ID == uuid.Nil {
			t.Fatal("expected Create() to populate ID")
		}
		if trade.CreatedAt.IsZero() {
			t.Fatal("expected Create() to populate CreatedAt")
		}
	}

	byOrder, err := tradeRepo.GetByOrder(ctx, orderID, repository.TradeFilter{
		Ticker: "AAPL",
		Side:   domain.OrderSideBuy,
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByOrder() error = %v", err)
	}
	if len(byOrder) != 2 {
		t.Fatalf("expected 2 AAPL buy trades for order, got %d", len(byOrder))
	}
	if byOrder[0].OrderID == nil || *byOrder[0].OrderID != orderID {
		t.Fatalf("expected returned trades to link to order %s, got %v", orderID, byOrder[0].OrderID)
	}

	byPosition, err := tradeRepo.GetByPosition(ctx, positionID, repository.TradeFilter{
		ExecutedAfter: timePtr(baseTime.Add(1 * time.Minute)),
	}, 10, 0)
	if err != nil {
		t.Fatalf("GetByPosition() error = %v", err)
	}
	if len(byPosition) != 1 {
		t.Fatalf("expected 1 filtered trade for position, got %d", len(byPosition))
	}
	if byPosition[0].ID != tradeB.ID {
		t.Fatalf("expected tradeB from position filter, got %s", byPosition[0].ID)
	}

	page, err := tradeRepo.GetByOrder(ctx, orderID, repository.TradeFilter{}, 2, 2)
	if err != nil {
		t.Fatalf("GetByOrder() pagination error = %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("expected 1 trade on paged result, got %d", len(page))
	}
}

func createTestOrder(t *testing.T, ctx context.Context, repo *OrderRepo, strategyID uuid.UUID) uuid.UUID {
	t.Helper()

	order := &domain.Order{
		StrategyID: &strategyID,
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeMarket,
		Quantity:   10,
		Status:     domain.OrderStatusSubmitted,
		Broker:     "alpaca",
	}

	if err := repo.Create(ctx, order); err != nil {
		t.Fatalf("failed to create test order: %v", err)
	}

	return order.ID
}

func createTestPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, strategyID uuid.UUID) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO positions (strategy_id, ticker, side, quantity, avg_entry)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id`,
		strategyID,
		"AAPL",
		domain.PositionSideLong,
		10,
		185.00,
	).Scan(&id); err != nil {
		t.Fatalf("failed to create test position: %v", err)
	}

	return id
}
