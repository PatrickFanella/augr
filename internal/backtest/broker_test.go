package backtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestNewBrokerAdapterRejectsNilFillEngine(t *testing.T) {
	t.Parallel()

	_, err := NewBrokerAdapter(1000, nil)
	if !errors.Is(err, ErrNilFillEngine) {
		t.Fatalf("NewBrokerAdapter() error = %v, want %v", err, ErrNilFillEngine)
	}
}

func TestBrokerAdapterSubmitOrder_MarketOrderUsesFillEngineAndMarksToMarket(t *testing.T) {
	t.Parallel()

	engine, err := NewFillEngine(FillConfig{
		Slippage: ProportionalSlippage{BasisPoints: 100},
		Costs: TransactionCosts{
			CommissionPerOrder: 1,
		},
	})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}

	broker, err := NewBrokerAdapter(1000, engine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter() error = %v", err)
	}

	bar := domain.OHLCV{
		Timestamp: time.Date(2026, 3, 25, 15, 30, 0, 0, time.UTC),
		Open:      100,
		High:      102,
		Low:       99,
		Close:     100,
		Volume:    1000,
	}
	if err := broker.SetMarketBar("AAPL", bar); err != nil {
		t.Fatalf("SetMarketBar() error = %v", err)
	}

	order := &domain.Order{
		Ticker:    "aapl",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  2,
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if externalID == "" {
		t.Fatal("SubmitOrder() externalID = empty, want non-empty")
	}
	if order.Status != domain.OrderStatusFilled {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusFilled)
	}
	if order.FilledAvgPrice == nil {
		t.Fatal("SubmitOrder() FilledAvgPrice = nil, want non-nil")
	}

	floatClose(t, *order.FilledAvgPrice, 101, 1e-9)
	if order.SubmittedAt == nil || !order.SubmittedAt.Equal(bar.Timestamp) {
		t.Fatalf("SubmittedAt = %v, want %s", order.SubmittedAt, bar.Timestamp)
	}
	if order.FilledAt == nil || !order.FilledAt.Equal(bar.Timestamp) {
		t.Fatalf("FilledAt = %v, want %s", order.FilledAt, bar.Timestamp)
	}

	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	floatClose(t, balance.Cash, 797, 1e-9)
	floatClose(t, balance.BuyingPower, 797, 1e-9)
	floatClose(t, balance.Equity, 997, 1e-9)

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("GetPositions() len = %d, want 1", len(positions))
	}
	if positions[0].Ticker != "AAPL" {
		t.Fatalf("positions[0].Ticker = %q, want %q", positions[0].Ticker, "AAPL")
	}
	floatClose(t, positions[0].Quantity, 2, 1e-9)
	floatClose(t, positions[0].AvgEntry, 101, 1e-9)
	if positions[0].CurrentPrice == nil {
		t.Fatal("positions[0].CurrentPrice = nil, want non-nil")
	}
	floatClose(t, *positions[0].CurrentPrice, 100, 1e-9)
}

func TestBrokerAdapterSubmitOrder_PartialFillUsesEngineResult(t *testing.T) {
	t.Parallel()

	engine, err := NewFillEngine(FillConfig{
		Slippage:     FixedSlippage{},
		MaxVolumePct: 0.5,
	})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}

	broker, err := NewBrokerAdapter(1000, engine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter() error = %v", err)
	}

	if err := broker.SetMarketBar("AAPL", testBar(100, 101, 99, 10)); err != nil {
		t.Fatalf("SetMarketBar() error = %v", err)
	}

	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  10,
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Status != domain.OrderStatusPartial {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusPartial)
	}
	floatClose(t, order.FilledQuantity, 5, 1e-9)

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusPartial {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusPartial)
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("GetPositions() len = %d, want 1", len(positions))
	}
	floatClose(t, positions[0].Quantity, 5, 1e-9)
}

func TestBrokerAdapterSubmitOrder_LimitOrderWithoutFillRemainsSubmitted(t *testing.T) {
	t.Parallel()

	engine, err := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}

	broker, err := NewBrokerAdapter(1000, engine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter() error = %v", err)
	}

	if err := broker.SetMarketBar("AAPL", testBar(105, 106, 104, 100)); err != nil {
		t.Fatalf("SetMarketBar() error = %v", err)
	}

	order := &domain.Order{
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: fp(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Status != domain.OrderStatusSubmitted {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusSubmitted)
	}
	if order.FilledAvgPrice != nil {
		t.Fatalf("SubmitOrder() FilledAvgPrice = %v, want nil", *order.FilledAvgPrice)
	}

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusSubmitted {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusSubmitted)
	}
}

func TestBrokerAdapterSubmitOrder_RejectsMissingMarketBar(t *testing.T) {
	t.Parallel()

	engine, err := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}

	broker, err := NewBrokerAdapter(1000, engine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter() error = %v", err)
	}

	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	if !errors.Is(err, ErrMissingBar) {
		t.Fatalf("SubmitOrder() error = %v, want ErrMissingBar", err)
	}
	if order.Status != domain.OrderStatusRejected {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusRejected)
	}

	status, statusErr := broker.GetOrderStatus(context.Background(), externalID)
	if statusErr != nil {
		t.Fatalf("GetOrderStatus() error = %v", statusErr)
	}
	if status != domain.OrderStatusRejected {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusRejected)
	}
}

func TestBrokerAdapterSetMarketBarUpdatesEquity(t *testing.T) {
	t.Parallel()

	engine, err := NewFillEngine(FillConfig{Slippage: FixedSlippage{}})
	if err != nil {
		t.Fatalf("NewFillEngine() error = %v", err)
	}

	broker, err := NewBrokerAdapter(1000, engine)
	if err != nil {
		t.Fatalf("NewBrokerAdapter() error = %v", err)
	}

	if err := broker.SetMarketBar("AAPL", testBar(100, 101, 99, 100)); err != nil {
		t.Fatalf("SetMarketBar(initial) error = %v", err)
	}

	if _, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	}); err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}

	if err := broker.SetMarketBar("AAPL", testBar(110, 111, 109, 100)); err != nil {
		t.Fatalf("SetMarketBar(updated) error = %v", err)
	}

	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	floatClose(t, balance.Cash, 900, 1e-9)
	floatClose(t, balance.Equity, 1010, 1e-9)

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 || positions[0].CurrentPrice == nil {
		t.Fatalf("GetPositions() = %+v, want one marked position", positions)
	}
	floatClose(t, *positions[0].CurrentPrice, 110, 1e-9)
}
