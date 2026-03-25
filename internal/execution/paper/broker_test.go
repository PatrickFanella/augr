package paper

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestPaperBrokerSubmitOrder_MarketOrderAppliesSlippage(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 10, 0)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
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
	expectedFillPrice := 100 * (1 + 10.0/10000)
	assertFloatClose(t, *order.FilledAvgPrice, expectedFillPrice, 1e-9)

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusFilled {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusFilled)
	}
}

func TestPaperBrokerSubmitOrder_DeductsFee(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0.01)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  2,
		StopPrice: floatPtr(100),
	}

	_, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}

	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}

	assertFloatClose(t, balance.Cash, 798, 1e-9)
	assertFloatClose(t, balance.BuyingPower, 798, 1e-9)
	assertFloatClose(t, balance.Equity, 998, 1e-9)
}

func TestPaperBrokerSubmitOrder_RejectsInsufficientBalance(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(50, 0, 0.01)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "insufficient balance") {
		t.Fatalf("SubmitOrder() error = %q, want insufficient balance", err.Error())
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

	balance, balanceErr := broker.GetAccountBalance(context.Background())
	if balanceErr != nil {
		t.Fatalf("GetAccountBalance() error = %v", balanceErr)
	}
	assertFloatClose(t, balance.Cash, 50, 1e-9)
}

func assertFloatClose(t *testing.T, got float64, want float64, epsilon float64) {
	t.Helper()
	if math.Abs(got-want) > epsilon {
		t.Fatalf("float mismatch: got %v, want %v (epsilon %v)", got, want, epsilon)
	}
}
