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

func TestFinancialLifecycle_FirstDeliveryReplayRollbackAndPositions(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newFinancialLifecycleIntegrationPool(t, ctx)
	defer cleanup()
	repo := &DB{Pool: pool}
	strategyID := createFinancialLifecycleStrategy(t, ctx, pool)
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	order := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "AAPL", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 10, FilledQuantity: 10, FilledAvgPrice: floatPtr(100), FilledAt: &now}
	trade := &domain.Trade{ID: uuid.New(), ExternalID: "fill-1", Ticker: "AAPL", Side: domain.OrderSideBuy, Quantity: 10, Price: 100, ExecutedAt: now, Fee: 1}
	createFinancialLifecycleOrder(t, ctx, pool, order)
	res, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "k1", Order: order, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 10, ExecutionPrice: 100}, Now: now, Trade: trade})
	if err != nil {
		t.Fatalf("ApplyOrderFill() error = %v", err)
	}
	if res.TradeID == uuid.Nil || res.PositionID == nil {
		t.Fatalf("expected ids, got %+v", res)
	}
	replay, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "k1", Order: order, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 10, ExecutionPrice: 100}, Now: now, Trade: &domain.Trade{ID: uuid.New()}})
	if err != nil {
		t.Fatalf("replay error = %v", err)
	}
	if replay.TradeID != res.TradeID || replay.OrderID != res.OrderID {
		t.Fatalf("expected exact replay, got %+v want %+v", replay, res)
	}

	bad := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "MSFT", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 1, FilledQuantity: 1, FilledAt: &now}
	createFinancialLifecycleOrder(t, ctx, pool, bad)
	_, err = repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "bad", Order: bad, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 0, ExecutionPrice: 50}, Now: now, Trade: &domain.Trade{ID: uuid.New(), Ticker: "MSFT", Side: domain.OrderSideBuy, Quantity: 0, Price: 50, ExecutedAt: now}})
	if err == nil {
		t.Fatal("expected constraint failure")
	}
	missingStrategy := &domain.Order{ID: uuid.New(), Ticker: "GOOG", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 1, FilledQuantity: 1, FilledAt: &now}
	_, err = repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "missing-strategy", Order: missingStrategy, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 1, ExecutionPrice: 50}, Now: now, Trade: &domain.Trade{ID: uuid.New(), Ticker: "GOOG", Side: domain.OrderSideBuy, Quantity: 1, Price: 50, ExecutedAt: now}})
	if err == nil {
		t.Fatal("expected validation failure for missing strategy")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM financial_fill_idempotency WHERE idempotency_key = 'bad'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected rollback, got count=%d err=%v", count, err)
	}

	pos, tradeID := testFinancialLifecyclePositionCreate(t, ctx, repo, strategyID)
	if pos == nil || tradeID == uuid.Nil {
		t.Fatal("expected position creation")
	}

	longID := createFinancialLifecyclePosition(t, ctx, pool, strategyID, "POLY:YES", domain.PositionSideLong, 10, 100)
	secondLongID := createFinancialLifecyclePosition(t, ctx, pool, strategyID, "POLY:YES", domain.PositionSideLong, 5, 120)
	closeOrder := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "POLY", PredictionSide: "YES", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideSell, Status: domain.OrderStatusFilled, Quantity: 12, FilledQuantity: 12, FilledAt: &now}
	closeTrade := &domain.Trade{ID: uuid.New(), Ticker: "POLY", Side: domain.OrderSideSell, Quantity: 12, Price: 110, ExecutedAt: now}
	createFinancialLifecycleOrder(t, ctx, pool, closeOrder)
	res2, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "poly-close", Order: closeOrder, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideSell, Quantity: 12, ExecutionPrice: 110}, Now: now, Trade: closeTrade})
	if err != nil {
		t.Fatalf("polymarket close error = %v", err)
	}
	if res2.TradeID == uuid.Nil {
		t.Fatal("expected trade id")
	}
	if res2.Position != nil {
		t.Fatalf("expected aggregate close result to omit a single position snapshot, got %+v", res2.Position)
	}
	var firstQty, secondQty, firstPnL, secondPnL float64
	var firstClosedAt, secondClosedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, realized_pnl::double precision, closed_at FROM positions WHERE id = $1`, longID).Scan(&firstQty, &firstPnL, &firstClosedAt); err != nil {
		t.Fatalf("failed to load first lot: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, realized_pnl::double precision, closed_at FROM positions WHERE id = $1`, secondLongID).Scan(&secondQty, &secondPnL, &secondClosedAt); err != nil {
		t.Fatalf("failed to load second lot: %v", err)
	}
	if firstQty != 0 || firstPnL != 100 || firstClosedAt == nil {
		t.Fatalf("unexpected first lot state: qty=%v pnl=%v closed=%v", firstQty, firstPnL, firstClosedAt)
	}
	if secondQty != 3 || secondPnL != -20 || secondClosedAt != nil {
		t.Fatalf("unexpected second lot state: qty=%v pnl=%v closed=%v", secondQty, secondPnL, secondClosedAt)
	}
	var tradeQty float64
	var tradePositionID *uuid.UUID
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, position_id FROM trades WHERE id = $1`, res2.TradeID).Scan(&tradeQty, &tradePositionID); err != nil {
		t.Fatalf("failed to load trade: %v", err)
	}
	if tradeQty != 12 || tradePositionID != nil {
		t.Fatalf("unexpected aggregate trade linkage: qty=%v position=%v", tradeQty, tradePositionID)
	}

	rollBackOrder := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "POLY", PredictionSide: "YES", MarketType: domain.MarketTypePolymarket, Side: domain.OrderSideSell, Status: domain.OrderStatusFilled, Quantity: 20, FilledQuantity: 20, FilledAt: &now}
	createFinancialLifecycleOrder(t, ctx, pool, rollBackOrder)
	_, err = repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "poly-insufficient", Order: rollBackOrder, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideSell, Quantity: 20, ExecutionPrice: 111}, Now: now, Trade: &domain.Trade{ID: uuid.New(), Ticker: "POLY", Side: domain.OrderSideSell, Quantity: 20, Price: 111, ExecutedAt: now}})
	if err == nil {
		t.Fatal("expected insufficient quantity failure")
	}
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, realized_pnl::double precision, closed_at FROM positions WHERE id = $1`, longID).Scan(&firstQty, &firstPnL, &firstClosedAt); err != nil {
		t.Fatalf("failed to reload first lot: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, realized_pnl::double precision, closed_at FROM positions WHERE id = $1`, secondLongID).Scan(&secondQty, &secondPnL, &secondClosedAt); err != nil {
		t.Fatalf("failed to reload second lot: %v", err)
	}
	if firstQty != 0 || firstPnL != 100 || firstClosedAt == nil || secondQty != 3 || secondPnL != -20 || secondClosedAt != nil {
		t.Fatalf("expected rollback to preserve lot states, got first=(%v,%v,%v) second=(%v,%v,%v)", firstQty, firstPnL, firstClosedAt, secondQty, secondPnL, secondClosedAt)
	}
	if res2.PositionID != nil {
		t.Fatalf("expected aggregate close result to omit position linkage, got %+v", res2.PositionID)
	}
}

func TestFinancialLifecycle_OptionSettlementCommitsPositionAndTradeAtomically(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newFinancialLifecycleIntegrationPool(t, ctx)
	defer cleanup()
	repo := &DB{Pool: pool}
	strategyID := createFinancialLifecycleStrategy(t, ctx, pool)
	settledAt := time.Date(2027, 12, 18, 22, 0, 0, 0, time.UTC)
	expiry := settledAt.Add(-24 * time.Hour)

	var positionID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO positions
		(id, strategy_id, ticker, side, quantity, avg_entry, asset_class, underlying_ticker, option_type, strike, expiry, contract_multiplier)
		VALUES ($1,$2,$3,$4,1,2,'option','AAPL','call',150,$5,100) RETURNING id`,
		uuid.New(), strategyID, "AAPL271217C00150000", domain.PositionSideLong, expiry,
	).Scan(&positionID); err != nil {
		t.Fatalf("create option position: %v", err)
	}
	result, err := repo.SettleOptionPosition(ctx, repository.OptionPositionSettlementInput{
		PositionID: positionID, SettlementPrice: 5, SettledAt: settledAt, ExitReason: "exercise_cash_settled",
	})
	if err != nil {
		t.Fatalf("SettleOptionPosition() error = %v", err)
	}
	var quantity, currentPrice, realizedPnL float64
	var closedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, current_price::double precision, realized_pnl::double precision, closed_at FROM positions WHERE id=$1`, positionID).Scan(&quantity, &currentPrice, &realizedPnL, &closedAt); err != nil {
		t.Fatalf("load settled position: %v", err)
	}
	if quantity != 0 || currentPrice != 5 || realizedPnL != 300 || closedAt == nil {
		t.Fatalf("unexpected settled position qty=%v current=%v pnl=%v closed=%v", quantity, currentPrice, realizedPnL, closedAt)
	}
	var tradePositionID uuid.UUID
	var premium float64
	var exitReason string
	if err := pool.QueryRow(ctx, `SELECT position_id, premium::double precision, exit_reason FROM trades WHERE id=$1`, result.TradeID).Scan(&tradePositionID, &premium, &exitReason); err != nil {
		t.Fatalf("load settlement trade: %v", err)
	}
	if tradePositionID != positionID || premium != 500 || exitReason != "exercise_cash_settled" {
		t.Fatalf("unexpected settlement trade position=%s premium=%v reason=%q", tradePositionID, premium, exitReason)
	}

	var rollbackPositionID uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO positions
		(id, strategy_id, ticker, side, quantity, avg_entry, asset_class, underlying_ticker, option_type, strike, expiry, contract_multiplier)
		VALUES ($1,$2,$3,$4,2,1,'option','AAPL','put',140,$5,100) RETURNING id`,
		uuid.New(), strategyID, "AAPL271217P00140000", domain.PositionSideLong, expiry,
	).Scan(&rollbackPositionID); err != nil {
		t.Fatalf("create rollback position: %v", err)
	}
	if _, err := pool.Exec(ctx, `ALTER TABLE trades ADD CONSTRAINT reject_new_exit_reason CHECK (exit_reason IS NULL) NOT VALID`); err != nil {
		t.Fatalf("install trade failure constraint: %v", err)
	}
	if _, err := repo.SettleOptionPosition(ctx, repository.OptionPositionSettlementInput{
		PositionID: rollbackPositionID, SettlementPrice: 0, SettledAt: settledAt, ExitReason: "expired_worthless",
	}); err == nil {
		t.Fatal("expected settlement trade failure")
	}
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, closed_at FROM positions WHERE id=$1`, rollbackPositionID).Scan(&quantity, &closedAt); err != nil {
		t.Fatalf("load rolled-back position: %v", err)
	}
	if quantity != 2 || closedAt != nil {
		t.Fatalf("position update was not rolled back: qty=%v closed=%v", quantity, closedAt)
	}
}

func TestFinancialLifecycle_SettlePredictionDecisionSingleLotKeepsLinkage(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newFinancialLifecycleSettlementIntegrationPool(t, ctx)
	defer cleanup()
	repo := &DB{Pool: pool}
	strategyID := createFinancialLifecycleStrategy(t, ctx, pool)
	decisionID := uuid.New()
	positionID := uuid.New()
	orderID := uuid.New()
	resolvedAt := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	createFinancialLifecycleOrder(t, ctx, pool, &domain.Order{ID: orderID, StrategyID: &strategyID, Ticker: "KX-TEST", MarketType: domain.MarketTypeKalshi, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 4, FilledQuantity: 4, FilledAt: &resolvedAt})
	insertFinancialLifecycleSettlementDecision(t, ctx, pool, decisionID, strategyID, orderID)
	insertFinancialLifecycleSettlementPosition(t, ctx, pool, positionID, strategyID, "KX-TEST:YES", 4, .40)
	if _, err := pool.Exec(ctx, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, executed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, uuid.New(), orderID, positionID, "KX-TEST:YES", domain.OrderSideBuy, 4, .40, resolvedAt.Add(-time.Minute)); err != nil {
		t.Fatalf("failed to insert opening trade: %v", err)
	}
	decision := &domain.TradeDecision{ID: decisionID, StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}
	res, err := repo.SettlePredictionDecision(ctx, repository.PredictionDecisionSettlementInput{IdempotencyKey: "prediction_settlement:v1:" + decisionID.String(), Decision: decision, PositionTicker: "KX-TEST:YES", Payout: 1, ResolvedAt: resolvedAt})
	if err != nil {
		t.Fatalf("SettlePredictionDecision() error = %v", err)
	}
	if res.PositionID == nil || *res.PositionID != positionID {
		t.Fatalf("expected single-lot settlement to retain linkage, got %+v", res)
	}
	var settledQuantity float64
	var settledClosedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision, closed_at FROM positions WHERE id = $1`, positionID).Scan(&settledQuantity, &settledClosedAt); err != nil {
		t.Fatalf("failed to load settled position: %v", err)
	}
	if settledQuantity != 0 || settledClosedAt == nil {
		t.Fatalf("expected exact linked position to be closed, quantity=%v closed_at=%v", settledQuantity, settledClosedAt)
	}
}

func TestFinancialLifecycle_KalshiEarlyExitClosesOpeningAndExitDecisions(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newFinancialLifecycleSettlementIntegrationPool(t, ctx)
	defer cleanup()
	repo := &DB{Pool: pool}
	strategyID := createFinancialLifecycleStrategy(t, ctx, pool)
	now := time.Date(2026, 7, 26, 16, 0, 0, 0, time.UTC)

	buy := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "KX-TEST", PredictionSide: "YES", MarketType: domain.MarketTypeKalshi, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 100, FilledQuantity: 100, FilledAvgPrice: floatPtr(0.40), FilledAt: &now}
	createFinancialLifecycleOrder(t, ctx, pool, buy)
	buyResult, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "kalshi-buy", Order: buy, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 100, ExecutionPrice: 0.40}, Now: now, Trade: &domain.Trade{ID: uuid.New()}})
	if err != nil {
		t.Fatalf("Kalshi buy error = %v", err)
	}
	openDecisionID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO trade_decisions (id,strategy_id,paper_order_id,market_type,instrument_key,outcome,status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, openDecisionID, strategyID, buy.ID, domain.MarketTypeKalshi, "KX-TEST", "YES", domain.TradeDecisionStatusPaper); err != nil {
		t.Fatalf("insert opening decision: %v", err)
	}

	sellAt := now.Add(time.Hour)
	sell := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "KX-TEST", PredictionSide: "YES", MarketType: domain.MarketTypeKalshi, Side: domain.OrderSideSell, Status: domain.OrderStatusFilled, Quantity: 100, FilledQuantity: 100, FilledAvgPrice: floatPtr(0.55), FilledAt: &sellAt}
	createFinancialLifecycleOrder(t, ctx, pool, sell)
	exitDecisionID := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO trade_decisions (id,strategy_id,paper_order_id,market_type,instrument_key,outcome,status) VALUES ($1,$2,$3,$4,$5,$6,$7)`, exitDecisionID, strategyID, sell.ID, domain.MarketTypeKalshi, "KX-TEST", "YES", domain.TradeDecisionStatusPaper); err != nil {
		t.Fatalf("insert exit decision: %v", err)
	}
	if _, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "kalshi-sell", Order: sell, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideSell, Quantity: 100, ExecutionPrice: 0.55}, Now: sellAt, Trade: &domain.Trade{ID: uuid.New()}}); err != nil {
		t.Fatalf("Kalshi sell error = %v", err)
	}

	var quantity, realized float64
	var closedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT quantity::double precision,realized_pnl::double precision,closed_at FROM positions WHERE id=$1`, *buyResult.PositionID).Scan(&quantity, &realized, &closedAt); err != nil {
		t.Fatalf("load closed position: %v", err)
	}
	if quantity != 0 || realized != 15 || closedAt == nil {
		t.Fatalf("closed position = quantity %v realized %v closed %v", quantity, realized, closedAt)
	}
	var closedDecisions int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM trade_decisions WHERE id=ANY($1::uuid[]) AND status=$2`, []uuid.UUID{openDecisionID, exitDecisionID}, domain.TradeDecisionStatusClosed).Scan(&closedDecisions); err != nil {
		t.Fatalf("count closed decisions: %v", err)
	}
	if closedDecisions != 2 {
		t.Fatalf("closed decisions = %d, want 2", closedDecisions)
	}
}

func TestFinancialLifecycle_SettlePredictionDecisionReplayAndRollback(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newFinancialLifecycleSettlementIntegrationPool(t, ctx)
	defer cleanup()
	repo := &DB{Pool: pool}
	strategyID := createFinancialLifecycleStrategy(t, ctx, pool)
	decisionID := uuid.New()
	positionID := uuid.New()
	orderID := uuid.New()
	resolvedAt := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	createFinancialLifecycleOrder(t, ctx, pool, &domain.Order{ID: orderID, StrategyID: &strategyID, Ticker: "KX-TEST", MarketType: domain.MarketTypeKalshi, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 4, FilledQuantity: 4, FilledAt: &resolvedAt})
	insertFinancialLifecycleSettlementDecision(t, ctx, pool, decisionID, strategyID, orderID)
	insertFinancialLifecycleSettlementPosition(t, ctx, pool, positionID, strategyID, "KX-TEST:YES", 4, .40)
	if _, err := pool.Exec(ctx, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, executed_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`, uuid.New(), orderID, positionID, "KX-TEST:YES", domain.OrderSideBuy, 4, .40, resolvedAt.Add(-time.Minute)); err != nil {
		t.Fatalf("failed to insert opening trade: %v", err)
	}
	decision := &domain.TradeDecision{ID: decisionID, StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusPaper}
	res, err := repo.SettlePredictionDecision(ctx, repository.PredictionDecisionSettlementInput{IdempotencyKey: "prediction_settlement:v1:" + decisionID.String(), Decision: decision, PositionTicker: "KX-TEST:YES", Payout: 1, ResolvedAt: resolvedAt})
	if err != nil {
		t.Fatalf("SettlePredictionDecision() error = %v", err)
	}
	if res.DecisionID != decisionID || res.TradeID == uuid.Nil || res.ReplayEventID == nil {
		t.Fatalf("unexpected result %+v", res)
	}
	again, err := repo.SettlePredictionDecision(ctx, repository.PredictionDecisionSettlementInput{IdempotencyKey: "prediction_settlement:v1:" + decisionID.String(), Decision: decision, PositionTicker: "KX-TEST:YES", Payout: 1, ResolvedAt: resolvedAt})
	if err != nil || again.TradeID != res.TradeID || again.ReplayEventID == nil || *again.ReplayEventID != *res.ReplayEventID {
		t.Fatalf("expected exact replay, got %+v err=%v", again, err)
	}
	invalidDecisionID := uuid.New()
	insertFinancialLifecycleSettlementDecision(t, ctx, pool, invalidDecisionID, strategyID, uuid.New())
	_, err = repo.SettlePredictionDecision(ctx, repository.PredictionDecisionSettlementInput{IdempotencyKey: "bad-settlement", Decision: &domain.TradeDecision{ID: invalidDecisionID, StrategyID: &strategyID, PaperOrderID: &orderID, MarketType: domain.MarketTypeKalshi, InstrumentKey: "KX-TEST", Outcome: "YES", Status: domain.TradeDecisionStatusClosed}, PositionTicker: "KX-TEST:YES", Payout: 1, ResolvedAt: resolvedAt})
	if err == nil {
		t.Fatal("expected invalid transition failure")
	}
	var count int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM prediction_settlement_idempotency WHERE idempotency_key = 'bad-settlement'`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("expected rollback, got count=%d err=%v", count, err)
	}
}

func TestFinancialLifecycle_PolymarketPositionTickerIdempotent(t *testing.T) {
	t.Parallel()
	if got := polymarketPositionTicker(" MARKET ", "yes"); got != "MARKET:YES" {
		t.Fatalf("first normalization = %q, want MARKET:YES", got)
	}
	if got := polymarketPositionTicker("MARKET:YES", "no"); got != "MARKET:YES" {
		t.Fatalf("already-qualified normalization = %q, want MARKET:YES", got)
	}
}

func TestFinancialLifecycle_ApplyOrderFillPersistsCanonicalPredictionTicker(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newFinancialLifecycleIntegrationPool(t, ctx)
	defer cleanup()
	repo := &DB{Pool: pool}
	strategyID := createFinancialLifecycleStrategy(t, ctx, pool)
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	order := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "MARKET:YES", MarketType: domain.MarketTypePolymarket, PredictionSide: "YES", Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 2, FilledQuantity: 2, FilledAvgPrice: floatPtr(0.4), FilledAt: &now}
	createFinancialLifecycleOrder(t, ctx, pool, order)
	trade := &domain.Trade{ID: uuid.New(), ExternalID: "fill-pred-1", Ticker: order.Ticker, Side: domain.OrderSideBuy, Quantity: 2, Price: 0.4, ExecutedAt: now, Fee: 0}
	res, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "poly-fill-1", Order: order, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 2, ExecutionPrice: 0.4}, Now: now, Trade: trade})
	if err != nil {
		t.Fatalf("ApplyOrderFill() error = %v", err)
	}
	if res.PositionID == nil {
		t.Fatal("expected position id")
	}
	var orderTicker, positionTicker string
	if err := pool.QueryRow(ctx, `SELECT ticker FROM orders WHERE id = $1`, order.ID).Scan(&orderTicker); err != nil {
		t.Fatalf("failed to load order ticker: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT ticker FROM positions WHERE id = $1`, *res.PositionID).Scan(&positionTicker); err != nil {
		t.Fatalf("failed to load position ticker: %v", err)
	}
	if orderTicker != "MARKET:YES" {
		t.Fatalf("order ticker = %q, want MARKET:YES", orderTicker)
	}
	if positionTicker != "MARKET:YES" {
		t.Fatalf("position ticker = %q, want MARKET:YES", positionTicker)
	}
}

func newFinancialLifecycleIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
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
	schemaName := "integration_financial_lifecycle_" + strings.ReplaceAll(uuid.New().String(), "-", "")
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
		`CREATE TYPE trade_side AS ENUM ('buy','sell')`,
		`CREATE TYPE position_side AS ENUM ('long','short')`,
		`CREATE TABLE strategies (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), market_type TEXT NOT NULL DEFAULT 'stock')`,
		`CREATE TABLE orders (id UUID PRIMARY KEY, strategy_id UUID REFERENCES strategies(id), ticker TEXT NOT NULL, market_type TEXT NOT NULL, side trade_side NOT NULL, status TEXT NOT NULL, quantity NUMERIC(20,8) NOT NULL, filled_quantity NUMERIC(20,8) NOT NULL DEFAULT 0, filled_avg_price NUMERIC(20,8), filled_at TIMESTAMPTZ, prediction_side TEXT)`,
		`CREATE TABLE positions (id UUID PRIMARY KEY, strategy_id UUID REFERENCES strategies(id), ticker TEXT NOT NULL, side position_side NOT NULL, quantity NUMERIC(20,8) NOT NULL, avg_entry NUMERIC(20,8) NOT NULL, current_price NUMERIC(20,8), unrealized_pnl NUMERIC(20,8), realized_pnl NUMERIC(20,8) NOT NULL DEFAULT 0, stop_loss NUMERIC(20,8), take_profit NUMERIC(20,8), opened_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), closed_at TIMESTAMPTZ, market_type TEXT, asset_class TEXT NOT NULL DEFAULT 'stock', underlying_ticker TEXT, option_type TEXT, strike NUMERIC(20,8), expiry TIMESTAMPTZ, contract_multiplier NUMERIC(20,8) NOT NULL DEFAULT 1, leg_group_id UUID, delta NUMERIC(20,8), gamma NUMERIC(20,8), theta NUMERIC(20,8), vega NUMERIC(20,8))`,
		`CREATE TABLE trades (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), external_id TEXT, order_id UUID REFERENCES orders(id), position_id UUID REFERENCES positions(id), ticker TEXT NOT NULL, side trade_side NOT NULL, quantity NUMERIC(20,8) NOT NULL CHECK (quantity > 0), price NUMERIC(20,8) NOT NULL, fee NUMERIC(20,8) NOT NULL DEFAULT 0, executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), asset_class TEXT NOT NULL DEFAULT 'stock', open_close TEXT, contract_multiplier NUMERIC(20,8) NOT NULL DEFAULT 1, premium NUMERIC(20,8), exit_reason TEXT)`,
		`CREATE TABLE financial_fill_idempotency (idempotency_key TEXT PRIMARY KEY, order_id UUID NOT NULL, position_id UUID, trade_id UUID NOT NULL, fill_quantity NUMERIC(20,8) NOT NULL, fill_price NUMERIC(20,8) NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
	}
	for _, stmt := range ddl {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
			adminPool.Close()
			t.Fatalf("failed to apply test schema DDL: %v", err)
		}
	}
	return pool, func() {
		pool.Close()
		_, _ = adminPool.Exec(ctx, `DROP SCHEMA "`+schemaName+`" CASCADE`)
		adminPool.Close()
	}
}

func newFinancialLifecycleSettlementIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	pool, cleanup := newFinancialLifecycleIntegrationPool(t, ctx)
	for _, stmt := range []string{
		`CREATE TABLE trade_decisions (id UUID PRIMARY KEY, strategy_id UUID REFERENCES strategies(id), paper_order_id UUID, market_type TEXT NOT NULL DEFAULT 'kalshi', instrument_key TEXT NOT NULL, outcome TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'paper_ordered', updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE replay_events (id UUID PRIMARY KEY DEFAULT gen_random_uuid(), trade_decision_id UUID REFERENCES trade_decisions(id), event_type TEXT NOT NULL, source TEXT NOT NULL, payload JSONB NOT NULL, occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
		`CREATE TABLE prediction_settlement_idempotency (idempotency_key TEXT PRIMARY KEY, decision_id UUID NOT NULL UNIQUE, position_id UUID, trade_id UUID NOT NULL, replay_event_id UUID, payout NUMERIC(20,8) NOT NULL, resolved_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`,
	} {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			cleanup()
			t.Fatalf("failed to create settlement tables: %v", err)
		}
	}
	return pool, cleanup
}

func insertFinancialLifecycleSettlementDecision(t *testing.T, ctx context.Context, pool *pgxpool.Pool, decisionID, strategyID, orderID uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO trade_decisions (id, strategy_id, paper_order_id, instrument_key, outcome, status) VALUES ($1,$2,$3,$4,$5,$6)`, decisionID, &strategyID, &orderID, "KX-TEST", "YES", domain.TradeDecisionStatusPaper); err != nil {
		t.Fatalf("failed to insert decision: %v", err)
	}
}

func insertFinancialLifecycleSettlementPosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, positionID, strategyID uuid.UUID, ticker string, qty, avg float64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry) VALUES ($1,$2,$3,$4,$5,$6)`, positionID, strategyID, ticker, domain.PositionSideLong, qty, avg); err != nil {
		t.Fatalf("failed to insert position: %v", err)
	}
}

func createFinancialLifecycleStrategy(t *testing.T, ctx context.Context, pool *pgxpool.Pool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO strategies DEFAULT VALUES RETURNING id`).Scan(&id); err != nil {
		t.Fatalf("failed to create strategy: %v", err)
	}
	return id
}

func createFinancialLifecycleOrder(t *testing.T, ctx context.Context, pool *pgxpool.Pool, order *domain.Order) {
	t.Helper()
	if _, err := pool.Exec(ctx, `INSERT INTO orders (id, strategy_id, ticker, market_type, side, status, quantity, filled_quantity, filled_avg_price, filled_at, prediction_side) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, order.ID, order.StrategyID, order.Ticker, order.MarketType, order.Side, order.Status, order.Quantity, order.FilledQuantity, order.FilledAvgPrice, order.FilledAt, order.PredictionSide); err != nil {
		t.Fatalf("failed to create order: %v", err)
	}
}

func createFinancialLifecyclePosition(t *testing.T, ctx context.Context, pool *pgxpool.Pool, strategyID uuid.UUID, ticker string, side domain.PositionSide, qty, avg float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := pool.QueryRow(ctx, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry) VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, uuid.New(), strategyID, ticker, side, qty, avg).Scan(&id); err != nil {
		t.Fatalf("failed to create position: %v", err)
	}
	return id
}

func testFinancialLifecyclePositionCreate(t *testing.T, ctx context.Context, repo *DB, strategyID uuid.UUID) (*domain.Position, uuid.UUID) {
	t.Helper()
	now := time.Date(2026, 3, 21, 10, 0, 0, 0, time.UTC)
	order := &domain.Order{ID: uuid.New(), StrategyID: &strategyID, Ticker: "TSLA", MarketType: domain.MarketTypeStock, Side: domain.OrderSideBuy, Status: domain.OrderStatusFilled, Quantity: 2, FilledQuantity: 2, FilledAt: &now}
	trade := &domain.Trade{ID: uuid.New(), Ticker: "TSLA", Side: domain.OrderSideBuy, Quantity: 2, Price: 200, ExecutedAt: now}
	createFinancialLifecycleOrder(t, ctx, repo.Pool, order)
	res, err := repo.ApplyOrderFill(ctx, repository.OrderFillInput{IdempotencyKey: "create-pos", Order: order, FillIntent: repository.OrderFillIntent{Side: domain.OrderSideBuy, Quantity: 2, ExecutionPrice: 200}, Now: now, Trade: trade})
	if err != nil {
		t.Fatalf("position create error: %v", err)
	}
	return &domain.Position{ID: *res.PositionID}, res.TradeID
}
func floatPtr(v float64) *float64 { return &v }
