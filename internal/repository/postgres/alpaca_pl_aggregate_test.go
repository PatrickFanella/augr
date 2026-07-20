package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestAlpacaPLAggregateRepo_IncludesProvenanceLegacyAndDedupes(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newOrderTradeIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAlpacaPLAggregateRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	now := time.Now().UTC()

	provenOpen := seedAggregatePosition(t, ctx, pool, strategyID, "AAPL", 11, 101, 7, nil)
	provenClosed := seedAggregatePosition(t, ctx, pool, strategyID, "MSFT", 5, 55, 13, &now)
	legacyOpen := seedAggregatePosition(t, ctx, pool, strategyID, "NVDA", 9, 99, 3, nil)
	legacyClosed := seedAggregatePosition(t, ctx, pool, strategyID, "AMD", 4, 44, 8, &now)
	dualOpen := seedAggregatePosition(t, ctx, pool, strategyID, "TSLA", 2, 222, 19, nil)
	dualClosed := seedAggregatePosition(t, ctx, pool, strategyID, "META", 3, 333, 23, &now)
	excluded := seedAggregatePosition(t, ctx, pool, strategyID, "PAPER", 1, 1, 999, &now)

	markPositionProvenance(t, ctx, pool, provenOpen)
	markPositionProvenance(t, ctx, pool, provenClosed)
	markPositionProvenance(t, ctx, pool, dualOpen)
	markPositionProvenance(t, ctx, pool, dualClosed)

	attachAlpacaTrade(t, ctx, pool, strategyID, legacyOpen, 0.50)
	attachAlpacaTrade(t, ctx, pool, strategyID, legacyClosed, 0.75)
	attachAlpacaTrade(t, ctx, pool, strategyID, dualOpen, 1.25)
	attachAlpacaTrade(t, ctx, pool, strategyID, dualClosed, 1.50)
	attachNonAlpacaTrade(t, ctx, pool, strategyID, excluded)

	wantOpen := 7.0 + 3.0 + 19.0
	wantClosed := 13.0 + 8.0 + 23.0
	wantTrades := 4
	wantFees := 0.50 + 0.75 + 1.25 + 1.50

	open, err := repo.OpenUnrealizedPnL(ctx)
	if err != nil {
		t.Fatalf("OpenUnrealizedPnL() error = %v", err)
	}
	closed, err := repo.ClosedRealizedPnL(ctx)
	if err != nil {
		t.Fatalf("ClosedRealizedPnL() error = %v", err)
	}
	trades, err := repo.TradeCount(ctx)
	if err != nil {
		t.Fatalf("TradeCount() error = %v", err)
	}
	fees, err := repo.FeeTotal(ctx)
	if err != nil {
		t.Fatalf("FeeTotal() error = %v", err)
	}

	if open != wantOpen {
		t.Fatalf("OpenUnrealizedPnL() = %.2f, want %.2f", open, wantOpen)
	}
	if closed != wantClosed {
		t.Fatalf("ClosedRealizedPnL() = %.2f, want %.2f", closed, wantClosed)
	}
	if trades != wantTrades {
		t.Fatalf("TradeCount() = %d, want %d", trades, wantTrades)
	}
	if fees != wantFees {
		t.Fatalf("FeeTotal() = %.2f, want %.2f", fees, wantFees)
	}
}

func TestAlpacaPLAggregateRepo_ExcludesPaperAndNonAlpaca(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	pool, cleanup := newOrderTradeIntegrationPool(t, ctx)
	defer cleanup()

	repo := NewAlpacaPLAggregateRepo(pool)
	strategyID := createTestStrategy(t, ctx, pool)
	now := time.Now().UTC()
	seedAggregatePosition(t, ctx, pool, strategyID, "PAPER", 1, 1, 5, &now)
	seedAggregatePosition(t, ctx, pool, strategyID, "KALSHI", 1, 1, 6, nil)
	seedAggregatePosition(t, ctx, pool, strategyID, "POLY", 1, 1, 7, &now)

	paperTrade := seedAggregatePosition(t, ctx, pool, strategyID, "IGNORED", 1, 1, 9, nil)
	attachNonAlpacaTrade(t, ctx, pool, strategyID, paperTrade)

	open, err := repo.OpenUnrealizedPnL(ctx)
	if err != nil {
		t.Fatalf("OpenUnrealizedPnL() error = %v", err)
	}
	closed, err := repo.ClosedRealizedPnL(ctx)
	if err != nil {
		t.Fatalf("ClosedRealizedPnL() error = %v", err)
	}
	trades, err := repo.TradeCount(ctx)
	if err != nil {
		t.Fatalf("TradeCount() error = %v", err)
	}
	fees, err := repo.FeeTotal(ctx)
	if err != nil {
		t.Fatalf("FeeTotal() error = %v", err)
	}

	if open != 0 || closed != 0 || trades != 0 || fees != 0 {
		t.Fatalf("expected all aggregates to exclude non-Alpaca rows, got open=%.2f closed=%.2f trades=%d fees=%.2f", open, closed, trades, fees)
	}
}

func seedAggregatePosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, strategyID uuid.UUID, ticker string, quantity, avgEntry, pnl float64, closedAt *time.Time) uuid.UUID {
	t.Helper()
	positionID := uuid.New()
	if closedAt != nil {
		if _, err := pool.Exec(ctx, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry, realized_pnl, closed_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, positionID, strategyID, ticker, domain.PositionSideLong, quantity, avgEntry, pnl, closedAt); err != nil {
			t.Fatalf("insert closed position: %v", err)
		}
		return positionID
	}
	if _, err := pool.Exec(ctx, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry, unrealized_pnl)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, positionID, strategyID, ticker, domain.PositionSideLong, quantity, avgEntry, pnl); err != nil {
		t.Fatalf("insert open position: %v", err)
	}
	return positionID
}

func markPositionProvenance(t *testing.T, ctx context.Context, pool *pgxpool.Pool, positionID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO position_provenance (position_id, broker) VALUES ($1, 'alpaca')`, positionID); err != nil {
		t.Fatalf("insert provenance: %v", err)
	}
}

func attachAlpacaTrade(t *testing.T, ctx context.Context, pool *pgxpool.Pool, strategyID uuid.UUID, positionID uuid.UUID, fee float64) {
	t.Helper()
	orderID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, strategy_id, ticker, side, order_type, quantity, filled_quantity, status, broker, submitted_at, filled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6,$7,'alpaca',NOW(),NOW())`, orderID, strategyID, "ALP", domain.OrderSideBuy, domain.OrderTypeLimit, 1, domain.OrderStatusFilled); err != nil {
		t.Fatalf("insert order: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, fee, executed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())`, uuid.New(), orderID, positionID, "ALP", domain.OrderSideBuy, 1, 1, fee); err != nil {
		t.Fatalf("insert trade: %v", err)
	}
}

func attachNonAlpacaTrade(t *testing.T, ctx context.Context, pool *pgxpool.Pool, strategyID uuid.UUID, positionID uuid.UUID) {
	t.Helper()
	orderID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, strategy_id, ticker, side, order_type, quantity, filled_quantity, status, broker, submitted_at, filled_at)
		VALUES ($1,$2,$3,$4,$5,$6,$6,$7,'paper',NOW(),NOW())`, orderID, strategyID, "IGN", domain.OrderSideBuy, domain.OrderTypeLimit, 1, domain.OrderStatusFilled); err != nil {
		t.Fatalf("insert paper order: %v", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, fee, executed_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,NOW())`, uuid.New(), orderID, positionID, "IGN", domain.OrderSideBuy, 1, 1, 9.99); err != nil {
		t.Fatalf("insert paper trade: %v", err)
	}
}
