package main

import (
	"context"
	"math"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/paper"
)

func TestReconstructPaperBalance(t *testing.T) {
	tests := []struct {
		name      string
		trades    []domain.Trade
		positions []domain.Position
		wantCash  float64
		wantEq    float64
	}{
		{
			name: "stock and paper prediction share unmultiplied reconstruction",
			trades: []domain.Trade{
				{Side: domain.OrderSideBuy, Quantity: 10, Price: 10, Fee: 1},
				{AssetClass: domain.AssetClassOption, Side: domain.OrderSideBuy, Quantity: 2, Price: 5, Fee: 1.3, ContractMultiplier: 100},
			},
			positions: []domain.Position{
				{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 10, AvgEntry: 10, CurrentPrice: floatPtr(11)},
				{Ticker: "YES", MarketType: domain.MarketTypeStock, Side: domain.PositionSideLong, Quantity: 2, AvgEntry: 5, CurrentPrice: floatPtr(6), ContractMultiplier: 100},
			},
			wantCash: 100000 - 101 - 11.3,
			wantEq:   (100000 - 101 - 11.3) + 110 + 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			balance, err := reconstructPaperBalance(100000, tt.trades, tt.positions)
			if err != nil {
				t.Fatalf("reconstructPaperBalance() error = %v", err)
			}
			if math.Abs(balance.Cash-tt.wantCash) > 1e-9 || math.Abs(balance.Equity-tt.wantEq) > 1e-9 {
				t.Fatalf("unexpected restored balance: %+v", balance)
			}
		})
	}
}

type fakePaperAccountRepo struct {
	trades    []domain.Trade
	positions []domain.Position
	orders    []domain.Order
	max       uint64
}

func (f fakePaperAccountRepo) ListPaperTrades(context.Context, int, int) ([]domain.Trade, error) {
	return f.trades, nil
}

func (f fakePaperAccountRepo) GetOpenPaperPositions(context.Context, int, int) ([]domain.Position, error) {
	return f.positions, nil
}

func (f fakePaperAccountRepo) ListOpenPaperOrders(context.Context, int, int) ([]domain.Order, error) {
	return f.orders, nil
}

func (f fakePaperAccountRepo) GetMaxPaperExternalIDSequence(context.Context) (uint64, error) {
	return f.max, nil
}

func TestBootstrapPaperOptionsAccountRestoresSharedBroker(t *testing.T) {
	broker := paper.NewPaperBroker(1000, 0, 0)
	repo := fakePaperAccountRepo{trades: []domain.Trade{{Side: domain.OrderSideBuy, Quantity: 1, Price: 2, Fee: 1}}, positions: []domain.Position{{Ticker: "AAPL", Side: domain.PositionSideLong, Quantity: 1, AvgEntry: 2, CurrentPrice: floatPtr(3)}}, orders: []domain.Order{{ExternalID: "paper-9", Status: domain.OrderStatusPartial, Ticker: "AAPL", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeLimit, Quantity: 1, FilledQuantity: 0.5, LimitPrice: floatPtr(2)}}, max: 9}
	if err := bootstrapPaperOptionsAccount(context.Background(), broker, repo); err != nil {
		t.Fatalf("bootstrapPaperOptionsAccount() error = %v", err)
	}
	balance, _ := broker.GetAccountBalance(context.Background())
	if balance.Cash != 997 {
		t.Fatalf("unexpected cash %v", balance.Cash)
	}
	order := &domain.Order{Ticker: "MSFT", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(100)}
	id, err := broker.SubmitOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if id != "paper-10" {
		t.Fatalf("sequence = %q, want paper-10", id)
	}
	status, err := broker.GetOrderStatus(context.Background(), "paper-9")
	if err != nil || status != domain.OrderStatusPartial {
		t.Fatalf("GetOrderStatus() = %q, %v", status, err)
	}
}

func TestBootstrapPaperOptionsAccountStartsAtPaperOneOnEmptyDB(t *testing.T) {
	broker := paper.NewPaperBroker(1000, 0, 0)
	repo := fakePaperAccountRepo{}
	if err := bootstrapPaperOptionsAccount(context.Background(), broker, repo); err != nil {
		t.Fatalf("bootstrapPaperOptionsAccount() error = %v", err)
	}
	id, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "IBM", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1, StopPrice: floatPtr(100)})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if id != "paper-1" {
		t.Fatalf("sequence = %q, want paper-1", id)
	}
}
