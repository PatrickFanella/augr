package kalshi

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution"
)

type fakeLiveClient struct {
	createReq     CreateOrderRequest
	createResp    CreateOrderResponse
	createErr     error
	cancelOrderID string
	cancelErr     error
	getOrderID    string
	getOrderResp  OrderResponse
	getOrderErr   error
	positions     []PositionResponse
	positionsErr  error
	balance       BalanceResponse
	balanceErr    error
}

func (f *fakeLiveClient) CreateOrder(_ context.Context, req CreateOrderRequest) (CreateOrderResponse, error) {
	f.createReq = req
	return f.createResp, f.createErr
}

func (f *fakeLiveClient) CancelOrder(_ context.Context, orderID string) error {
	f.cancelOrderID = orderID
	return f.cancelErr
}

func (f *fakeLiveClient) GetOrder(_ context.Context, orderID string) (OrderResponse, error) {
	f.getOrderID = orderID
	return f.getOrderResp, f.getOrderErr
}

func (f *fakeLiveClient) ListPositions(context.Context) ([]PositionResponse, error) {
	return f.positions, f.positionsErr
}

func (f *fakeLiveClient) GetBalance(context.Context) (BalanceResponse, error) {
	return f.balance, f.balanceErr
}

func TestBrokerSatisfiesExecutionBroker(t *testing.T) {
	t.Parallel()

	var _ execution.Broker = NewBroker(nil)
}

func TestBrokerSubmitOrder_IsDisabled(t *testing.T) {
	t.Parallel()

	_, err := NewBroker(nil).SubmitOrder(context.Background(), &domain.Order{})
	if !errors.Is(err, errLiveExecutionDisabled) {
		t.Fatalf("SubmitOrder() error = %v, want disabled error", err)
	}
}

func TestBrokerReadMethods_AreUnsupported(t *testing.T) {
	t.Parallel()

	broker := NewBroker(nil)
	if err := broker.CancelOrder(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("CancelOrder() error = %v, want disabled error", err)
	}
	if _, err := broker.GetOrderStatus(context.Background(), "abc"); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("GetOrderStatus() error = %v, want unsupported error", err)
	}
	if _, err := broker.GetPositions(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("GetPositions() error = %v, want unsupported error", err)
	}
	if _, err := broker.GetAccountBalance(context.Background()); err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("GetAccountBalance() error = %v, want unsupported error", err)
	}
}

func TestBrokerSubmitOrder_UsesLiveClient(t *testing.T) {
	t.Parallel()

	price := 0.42
	order := &domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeLimit, Quantity: 3, LimitPrice: &price, PredictionSide: "YES"}
	client := &fakeLiveClient{createResp: CreateOrderResponse{OrderID: "ext-123"}}

	got, err := NewBroker(client).SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if got != "ext-123" {
		t.Fatalf("SubmitOrder() = %q, want %q", got, "ext-123")
	}
	wantReq, err := mapCreateOrderRequest(order)
	if err != nil {
		t.Fatalf("mapCreateOrderRequest() error = %v", err)
	}
	if client.createReq.Ticker != wantReq.Ticker || client.createReq.Side != wantReq.Side || client.createReq.Action != wantReq.Action || client.createReq.Count != wantReq.Count || client.createReq.Type != wantReq.Type || client.createReq.ClientOrderID != wantReq.ClientOrderID {
		t.Fatalf("CreateOrder request = %#v, want %#v", client.createReq, wantReq)
	}
	if (client.createReq.YesPrice == nil) != (wantReq.YesPrice == nil) || (client.createReq.YesPrice != nil && *client.createReq.YesPrice != *wantReq.YesPrice) {
		t.Fatalf("CreateOrder yes price = %#v, want %#v", client.createReq.YesPrice, wantReq.YesPrice)
	}
}

func TestBrokerSubmitOrder_WrapsClientError(t *testing.T) {
	t.Parallel()

	client := &fakeLiveClient{createErr: errors.New("boom")}
	_, err := NewBroker(client).SubmitOrder(context.Background(), &domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1, PredictionSide: "YES"})
	if err == nil || !strings.Contains(err.Error(), "kalshi: submit order:") {
		t.Fatalf("SubmitOrder() error = %v, want wrapped client error", err)
	}
}

func TestBrokerCancelOrder_UsesLiveClient(t *testing.T) {
	t.Parallel()

	client := &fakeLiveClient{}
	if err := NewBroker(client).CancelOrder(context.Background(), "ord-abc"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	if client.cancelOrderID != "ord-abc" {
		t.Fatalf("CancelOrder() id = %q, want %q", client.cancelOrderID, "ord-abc")
	}
}

func TestBrokerGetOrderStatus_UsesLiveClient(t *testing.T) {
	t.Parallel()

	client := &fakeLiveClient{getOrderResp: OrderResponse{OrderID: "ord-abc", Status: "resting"}}
	got, err := NewBroker(client).GetOrderStatus(context.Background(), "ord-abc")
	if err != nil {
		t.Fatalf("GetOrderStatus() error = %v", err)
	}
	if got != domain.OrderStatusSubmitted {
		t.Fatalf("GetOrderStatus() = %q, want %q", got, domain.OrderStatusSubmitted)
	}
	if client.getOrderID != "ord-abc" {
		t.Fatalf("GetOrderStatus() id = %q, want %q", client.getOrderID, "ord-abc")
	}
}

func TestBrokerGetPositions_UsesLiveClient(t *testing.T) {
	t.Parallel()

	client := &fakeLiveClient{positions: []PositionResponse{{Ticker: "KX-YES", Side: "yes", Count: 2, ValueCents: 150}, {Ticker: "KX-NO", Side: "no", Count: 1, ValueCents: 75}}}
	positions, err := NewBroker(client).GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("GetPositions() len = %d, want 2", len(positions))
	}
	if positions[0].Ticker != "KX-YES" || positions[0].Side != domain.PositionSideLong || positions[0].Quantity != 2 || math.Abs(positions[0].AvgEntry-0.75) > 1e-9 {
		t.Fatalf("GetPositions()[0] = %#v", positions[0])
	}
	if positions[1].Ticker != "KX-NO" || positions[1].Side != domain.PositionSideShort || positions[1].Quantity != 1 || math.Abs(positions[1].AvgEntry-0.75) > 1e-9 {
		t.Fatalf("GetPositions()[1] = %#v", positions[1])
	}
}

func TestBrokerGetAccountBalance_UsesLiveClient(t *testing.T) {
	t.Parallel()

	client := &fakeLiveClient{balance: BalanceResponse{CashCents: 12345, BuyingPowerCents: 67890, EquityCents: 99999}}
	got, err := NewBroker(client).GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if got.Currency != "USD" || math.Abs(got.Cash-123.45) > 1e-9 || math.Abs(got.BuyingPower-678.90) > 1e-9 || math.Abs(got.Equity-999.99) > 1e-9 {
		t.Fatalf("GetAccountBalance() = %#v", got)
	}
}
