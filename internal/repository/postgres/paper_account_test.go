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
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
)

func TestPaperAccountRepoExcludesNonLocalPaperRowsAndParsesSequence(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPaperAccountIntegrationPool(t, ctx)
	defer cleanup()
	repo := NewPaperAccountRepo(&DB{Pool: pool})

	seedPaperAccountFixtures(t, ctx, pool)

	trades, err := repo.ListPaperTrades(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListPaperTrades() error = %v", err)
	}
	if len(trades) != 3 {
		t.Fatalf("paper trades len = %d, want 3", len(trades))
	}
	for _, trade := range trades {
		if trade.Ticker == "MSFT" {
			t.Fatal("alpaca paper trade was included, want excluded")
		}
	}
	if trades[0].ExitReason != "paper_restore_regression" {
		t.Fatalf("latest paper trade exit reason = %q, want paper_restore_regression", trades[0].ExitReason)
	}

	positions, err := repo.GetOpenPaperPositions(ctx, 100, 0)
	if err != nil {
		t.Fatalf("GetOpenPaperPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("paper positions len = %d, want 2", len(positions))
	}
	for _, pos := range positions {
		if pos.Ticker == "MSFT" {
			t.Fatal("alpaca paper position was included, want excluded")
		}
	}

	orders, err := repo.ListOpenPaperOrders(ctx, 100, 0)
	if err != nil {
		t.Fatalf("ListOpenPaperOrders() error = %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("paper orders len = %d, want 2", len(orders))
	}
	seq, err := repo.GetMaxPaperExternalIDSequence(ctx)
	if err != nil {
		t.Fatalf("GetMaxPaperExternalIDSequence() error = %v", err)
	}
	if seq != 43 {
		t.Fatalf("seq = %d, want 43", seq)
	}
}

func TestPaperAccountRestoreParityWithRealDB(t *testing.T) {
	ctx := context.Background()
	pool, cleanup := newPaperAccountIntegrationPool(t, ctx)
	defer cleanup()
	repo := NewPaperAccountRepo(&DB{Pool: pool})
	seedPaperAccountFixtures(t, ctx, pool)

	trades, _ := repo.ListPaperTrades(ctx, 100, 0)
	positions, _ := repo.GetOpenPaperPositions(ctx, 100, 0)
	orders, _ := repo.ListOpenPaperOrders(ctx, 100, 0)
	seq, _ := repo.GetMaxPaperExternalIDSequence(ctx)

	broker := paper.NewPaperBroker(1000, 0, 0)
	if err := broker.RestoreAccount(executionBalanceFromRows(trades, positions)); err != nil {
		t.Fatalf("RestoreAccount() error = %v", err)
	}
	if err := broker.RestorePositions(positions); err != nil {
		t.Fatalf("RestorePositions() error = %v", err)
	}
	if err := broker.RestoreOrders(orders); err != nil {
		t.Fatalf("RestoreOrders() error = %v", err)
	}
	if err := broker.RestoreOrderSequence(seq); err != nil {
		t.Fatalf("RestoreOrderSequence() error = %v", err)
	}

	balance, err := broker.GetAccountBalance(ctx)
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	// Cash: 1000 - (100*1 + 0.5) - (100*1 + 0.5) - (0.5*3 + 0.3) = 797.2
	// Equity: cash + (105*2) + (0.75*3) = 1009.45
	if balance.Cash != 797.2 || balance.BuyingPower != 797.2 || balance.Equity != 1009.45 {
		t.Fatalf("restored balance = %+v", balance)
	}
	if got := 1000 - (100*1 + 0.5) - (100*1 + 0.5) - (0.5*3 + 0.3); got != 797.2 {
		t.Fatalf("cash arithmetic = %v, want 797.2", got)
	}
	if got := 797.2 + (105 * 2) + (0.75 * 3); got != 1009.45 {
		t.Fatalf("equity arithmetic = %v, want 1009.45", got)
	}
	positionsRestored, err := broker.GetPositions(ctx)
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if got := len(positionsRestored); got != 2 {
		t.Fatalf("restored open positions = %d, want 2", got)
	}
	if status, err := broker.GetOrderStatus(ctx, "paper-43"); err != nil || status != domain.OrderStatusSubmitted {
		t.Fatalf("restored order status = %q, %v", status, err)
	}
	if id, err := broker.SubmitOrder(ctx, &domain.Order{Ticker: "IBM", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(100)}); err != nil || id != "paper-44" {
		t.Fatalf("sequence after restore = %q, %v", id, err)
	}
}

func seedPaperAccountFixtures(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	paperStrategyID := uuid.New()
	alpacaStrategyID := uuid.New()
	paperOrderID := uuid.New()
	alpacaOrderID := uuid.New()
	openOrderID := uuid.New()
	openPositionID := uuid.New()
	secondPositionID := uuid.New()
	tradeID := uuid.New()
	trade2ID := uuid.New()
	trade3ID := uuid.New()
	trade4ID := uuid.New()
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO strategies (id, is_paper, market_type, ticker, name) VALUES ($1, true, 'stock', 'AAPL', 'local-paper'), ($2, true, 'stock', 'MSFT', 'alpaca-paper')`, paperStrategyID, alpacaStrategyID)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO orders (id, strategy_id, external_id, broker, ticker, market_type, side, order_type, quantity, limit_price, stop_price, filled_quantity, filled_avg_price, status, submitted_at, filled_at, created_at, asset_class, contract_multiplier)
		VALUES ($1,$2,'paper-42','paper','AAPL','stock','buy','market',1,NULL,100,1,100,'filled',$3,$3,$3,'equity',100)`, paperOrderID, paperStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO orders (id, strategy_id, external_id, broker, ticker, market_type, side, order_type, quantity, limit_price, stop_price, filled_quantity, filled_avg_price, status, submitted_at, filled_at, created_at, asset_class, contract_multiplier)
		VALUES ($1,$2,'paper-43','paper','AAPL','stock','buy','limit',1,99,NULL,0,NULL,'submitted',$3,NULL,$3,'equity',100)`, openOrderID, paperStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO orders (id, strategy_id, external_id, broker, ticker, market_type, side, order_type, quantity, limit_price, stop_price, filled_quantity, filled_avg_price, status, submitted_at, filled_at, created_at, asset_class, contract_multiplier, prediction_side)
		VALUES ($1,$2,'paper-41','paper','YES','stock','buy','limit',3,0.5,NULL,1,NULL,'partial',$3,NULL,$3,'equity',100,'YES')`, tradeID, paperStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO orders (id, strategy_id, external_id, broker, ticker, market_type, side, order_type, quantity, limit_price, stop_price, filled_quantity, filled_avg_price, status, submitted_at, filled_at, created_at, asset_class, contract_multiplier)
		VALUES ($1,$2,'paper-99','alpaca','MSFT','stock','buy','market',1,NULL,100,1,100,'filled',$3,$3,$3,'equity',100)`, alpacaOrderID, alpacaStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, market_type, asset_class, contract_multiplier)
		VALUES ($1,$2,'AAPL','long',2,100,105,10,0,$3,NULL,'stock','equity',100)`, openPositionID, paperStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, market_type, asset_class, contract_multiplier)
		VALUES ($1,$2,'YES','long',3,0.5,0.75,0.75,0,$3,NULL,'stock','equity',100)`, secondPositionID, paperStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO positions (id, strategy_id, ticker, side, quantity, avg_entry, current_price, unrealized_pnl, realized_pnl, opened_at, closed_at, market_type, asset_class, contract_multiplier)
		VALUES ($1,$2,'MSFT','long',1,100,100,0,0,$3,NULL,'stock','equity',100)`, uuid.New(), alpacaStrategyID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, fee, executed_at, created_at, asset_class, open_close, contract_multiplier, premium, exit_reason)
		VALUES ($1,$2,$3,'AAPL','buy',1,100,0.5,$4::timestamptz,$4::timestamptz + interval '1 second','equity','open',100,100,'paper_restore_regression')`, trade2ID, paperOrderID, openPositionID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, fee, executed_at, created_at, asset_class, open_close, contract_multiplier, premium)
		VALUES ($1,$2,$3,'AAPL','buy',1,100,0.5,$4,$4,'equity','open',100,100)`, trade3ID, paperOrderID, openPositionID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, fee, executed_at, created_at, asset_class, open_close, contract_multiplier, premium)
		VALUES ($1,$2,$3,'YES','buy',3,0.5,0.3,$4,$4,'prediction','open',100,1.5)`, trade4ID, paperOrderID, secondPositionID, now)
	mustExecPaperAccount(t, ctx, pool, `INSERT INTO trades (id, order_id, position_id, ticker, side, quantity, price, fee, executed_at, created_at, asset_class, open_close, contract_multiplier, premium)
		VALUES ($1,$2,$3,'MSFT','buy',1,100,0.5,$4,$4,'equity','open',100,100)`, uuid.New(), alpacaOrderID, uuid.New(), now)
}

func executionBalanceFromRows(trades []domain.Trade, positions []domain.Position) execution.Balance {
	cash := 1000.0
	for _, tr := range trades {
		// Debits are price * quantity plus fee; no multiplier applies to these fixture trades.
		cash -= tr.Price*tr.Quantity + tr.Fee
	}
	equity := cash
	for _, pos := range positions {
		price := pos.AvgEntry
		if pos.CurrentPrice != nil {
			price = *pos.CurrentPrice
		}
		// Open marks are marked at price * quantity; no multiplier applies.
		equity += price * pos.Quantity
	}
	return execution.Balance{Currency: "USD", Cash: cash, BuyingPower: cash, Equity: equity}
}

func newPaperAccountIntegrationPool(t *testing.T, ctx context.Context) (*pgxpool.Pool, func()) {
	t.Helper()
	connString := os.Getenv("DB_URL")
	if connString == "" {
		connString = os.Getenv("DATABASE_URL")
	}
	if connString == "" {
		t.Skip("DB_URL or DATABASE_URL not set")
	}
	adminPool, err := pgxpool.New(ctx, connString)
	if err != nil {
		t.Fatalf("admin pool: %v", err)
	}
	schemaName := "integration_paper_account_" + strings.ReplaceAll(uuid.New().String(), "-", "")
	if _, err := adminPool.Exec(ctx, `CREATE SCHEMA `+pqQuoteIdent(schemaName)); err != nil {
		adminPool.Close()
		t.Fatalf("create schema: %v", err)
	}
	cfg, _ := pgxpool.ParseConfig(connString)
	cfg.ConnConfig.RuntimeParams["search_path"] = schemaName + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		adminPool.Close()
		t.Fatalf("pool: %v", err)
	}
	stmts := []string{
		`CREATE TABLE strategies (id UUID PRIMARY KEY, is_paper BOOLEAN NOT NULL DEFAULT false, market_type TEXT NOT NULL, ticker TEXT NOT NULL, name TEXT NOT NULL DEFAULT '')`,
		`CREATE TABLE orders (id UUID PRIMARY KEY, strategy_id UUID NOT NULL, pipeline_run_id UUID, external_id TEXT, ticker TEXT NOT NULL, market_type TEXT NOT NULL, side TEXT NOT NULL, order_type TEXT NOT NULL, quantity NUMERIC NOT NULL, limit_price NUMERIC, stop_price NUMERIC, filled_quantity NUMERIC NOT NULL DEFAULT 0, filled_avg_price NUMERIC, status TEXT NOT NULL, broker TEXT NOT NULL, submitted_at TIMESTAMPTZ, filled_at TIMESTAMPTZ, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), asset_class TEXT NOT NULL DEFAULT 'equity', underlying_ticker TEXT, option_type TEXT, strike NUMERIC, expiry TIMESTAMPTZ, contract_multiplier NUMERIC NOT NULL DEFAULT 100, position_intent TEXT, leg_group_id UUID, prediction_side TEXT, polymarket_intent TEXT, CONSTRAINT orders_prediction_side_check CHECK (prediction_side IS NULL OR prediction_side IN ('YES', 'NO')))`,
		`CREATE TABLE positions (id UUID PRIMARY KEY, strategy_id UUID NOT NULL, ticker TEXT NOT NULL, side TEXT NOT NULL, quantity NUMERIC NOT NULL, avg_entry NUMERIC NOT NULL, current_price NUMERIC, unrealized_pnl NUMERIC, realized_pnl NUMERIC, stop_loss NUMERIC, take_profit NUMERIC, opened_at TIMESTAMPTZ NOT NULL, closed_at TIMESTAMPTZ, market_type TEXT NOT NULL DEFAULT 'stock', asset_class TEXT NOT NULL DEFAULT 'equity', underlying_ticker TEXT, option_type TEXT, strike NUMERIC, expiry TIMESTAMPTZ, contract_multiplier NUMERIC NOT NULL DEFAULT 100, leg_group_id UUID, delta NUMERIC, gamma NUMERIC, theta NUMERIC, vega NUMERIC)`,
		`CREATE TABLE trades (id UUID PRIMARY KEY, external_id TEXT, order_id UUID NOT NULL, position_id UUID NOT NULL, ticker TEXT NOT NULL, side TEXT NOT NULL, quantity NUMERIC NOT NULL, price NUMERIC NOT NULL, fee NUMERIC NOT NULL DEFAULT 0, executed_at TIMESTAMPTZ NOT NULL, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), asset_class TEXT NOT NULL DEFAULT 'equity', open_close TEXT, contract_multiplier NUMERIC NOT NULL DEFAULT 100, premium NUMERIC, exit_reason TEXT)`,
	}
	for _, stmt := range stmts {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			pool.Close()
			adminPool.Close()
			t.Fatalf("ddl: %v", err)
		}
	}
	return pool, func() { pool.Close(); adminPool.Close() }
}

func mustExecPaperAccount(t *testing.T, ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) {
	t.Helper()
	if _, err := pool.Exec(ctx, sql, args...); err != nil {
		t.Fatalf("exec %q: %v", sql, err)
	}
}
