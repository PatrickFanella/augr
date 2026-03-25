package paper

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

// extremeSlippageBps represents 200% slippage (20000 bps) for sell-side clamp coverage.
const extremeSlippageBps = 20000

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

func TestPaperBrokerSubmitOrder_LimitOrderWithoutReferenceRemainsSubmitted(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 25, 0)
	order := &domain.Order{
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: floatPtr(100),
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

func TestPaperBrokerSubmitOrder_NormalizesTickerForPositions(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "aapl",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	})
	if err != nil {
		t.Fatalf("SubmitOrder(first) error = %v", err)
	}

	_, err = broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    " AAPL ",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  2,
		StopPrice: floatPtr(100),
	})
	if err != nil {
		t.Fatalf("SubmitOrder(second) error = %v", err)
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("GetPositions() len = %d, want %d", len(positions), 1)
	}
	if positions[0].Ticker != "AAPL" {
		t.Fatalf("positions[0].Ticker = %q, want %q", positions[0].Ticker, "AAPL")
	}
	assertFloatClose(t, positions[0].Quantity, 3, 1e-9)
}

func TestPaperBrokerSubmitOrder_UsesInjectedClock(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 25, 15, 30, 0, 0, time.UTC)
	broker := NewPaperBroker(1000, 0, 0)
	broker.SetNowFunc(func() time.Time { return now })

	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	if _, err := broker.SubmitOrder(context.Background(), order); err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.SubmittedAt == nil || !order.SubmittedAt.Equal(now) {
		t.Fatalf("SubmittedAt = %v, want %s", order.SubmittedAt, now)
	}
	if order.FilledAt == nil || !order.FilledAt.Equal(now) {
		t.Fatalf("FilledAt = %v, want %s", order.FilledAt, now)
	}
	if !order.CreatedAt.Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", order.CreatedAt, now)
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("GetPositions() len = %d, want 1", len(positions))
	}
	if !positions[0].OpenedAt.Equal(now) {
		t.Fatalf("positions[0].OpenedAt = %s, want %s", positions[0].OpenedAt, now)
	}
}

func TestPaperBrokerSubmitOrder_ClampsExtremeSellSlippage(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, extremeSlippageBps, 0)
	order := &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideSell,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
		StopPrice: floatPtr(100),
	}

	_, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.FilledAvgPrice == nil {
		t.Fatal("SubmitOrder() FilledAvgPrice = nil, want non-nil")
	}
	if *order.FilledAvgPrice <= 0 {
		t.Fatalf("SubmitOrder() FilledAvgPrice = %v, want > 0", *order.FilledAvgPrice)
	}
}

func TestPaperBrokerCancelOrder_CancelsSubmittedOrder(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	order := &domain.Order{
		Ticker:     "AAPL",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: floatPtr(100),
	}

	externalID, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if order.Status != domain.OrderStatusSubmitted {
		t.Fatalf("SubmitOrder() status = %q, want %q", order.Status, domain.OrderStatusSubmitted)
	}

	if err := broker.CancelOrder(context.Background(), externalID); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}

	status, err := broker.GetOrderStatus(context.Background(), externalID)
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusCancelled {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusCancelled)
	}
}

func TestPaperBrokerGetOrderStatus_ReturnsTrackedStatus(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	broker.orders["paper-42"] = &domain.Order{
		ExternalID: "paper-42",
		Status:     domain.OrderStatusPartial,
	}

	status, err := broker.GetOrderStatus(context.Background(), "paper-42")
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if status != domain.OrderStatusPartial {
		t.Fatalf("GetOrderStatus() = %q, want %q", status, domain.OrderStatusPartial)
	}
}

func TestPaperBrokerGetPositions_ReturnsClonedSortedPositions(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	broker.positions["MSFT"] = &domain.Position{
		Ticker:       "MSFT",
		Side:         domain.PositionSideLong,
		Quantity:     2,
		AvgEntry:     250,
		CurrentPrice: floatPtr(255),
	}
	broker.positions["AAPL"] = &domain.Position{
		Ticker:       "AAPL",
		Side:         domain.PositionSideLong,
		Quantity:     1,
		AvgEntry:     100,
		CurrentPrice: floatPtr(105),
	}

	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("GetPositions() len = %d, want %d", len(positions), 2)
	}
	if positions[0].Ticker != "AAPL" || positions[1].Ticker != "MSFT" {
		t.Fatalf("GetPositions() tickers = [%q %q], want [\"AAPL\" \"MSFT\"]", positions[0].Ticker, positions[1].Ticker)
	}

	positions[0].Quantity = 99
	*positions[0].CurrentPrice = 999

	refetched, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() second call error = %v", err)
	}
	assertFloatClose(t, refetched[0].Quantity, 1, 1e-9)
	assertFloatClose(t, *refetched[0].CurrentPrice, 105, 1e-9)
}

func TestPaperBrokerGetAccountBalance_ReturnsSnapshot(t *testing.T) {
	t.Parallel()

	broker := NewPaperBroker(1000, 0, 0)
	broker.balance = execution.Balance{
		Currency:    "USD",
		Cash:        850,
		BuyingPower: 850,
		Equity:      910,
	}

	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if balance.Currency != "USD" {
		t.Fatalf("GetAccountBalance() currency = %q, want %q", balance.Currency, "USD")
	}
	assertFloatClose(t, balance.Cash, 850, 1e-9)
	assertFloatClose(t, balance.BuyingPower, 850, 1e-9)
	assertFloatClose(t, balance.Equity, 910, 1e-9)

	balance.Cash = 1

	refetched, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() second call error = %v", err)
	}
	assertFloatClose(t, refetched.Cash, 850, 1e-9)
}

func assertFloatClose(t *testing.T, got float64, want float64, epsilon float64) {
	t.Helper()
	if math.Abs(got-want) > epsilon {
		t.Fatalf("float mismatch: got %v, want %v (epsilon %v)", got, want, epsilon)
	}
}
