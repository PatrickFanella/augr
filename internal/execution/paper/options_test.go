package paper

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func optionOrder(price *float64) *domain.Order {
	optionType := domain.OptionTypeCall
	intent := domain.PositionIntentBuyToOpen
	return &domain.Order{Ticker: "AAPL271217C00150000", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeLimit, Quantity: 2, LimitPrice: price, AssetClass: domain.AssetClassOption, OptionType: &optionType, ContractMultiplier: 100, PositionIntent: &intent}
}

func TestSubmitOptionOrderRequiresExecutablePrice(t *testing.T) {
	broker := NewPaperBroker(10000, 0, 0)
	if _, err := broker.SubmitOptionOrder(context.Background(), optionOrder(nil)); err == nil {
		t.Fatal("expected missing executable price to be rejected")
	}
}

func TestSubmitOptionOrderFillsWithoutExternalBroker(t *testing.T) {
	price := 2.50
	broker := NewPaperBroker(10000, 0, 0)
	order := optionOrder(&price)
	externalID, err := broker.SubmitOptionOrder(context.Background(), order)
	if err != nil {
		t.Fatalf("SubmitOptionOrder() error = %v", err)
	}
	if externalID == "" || order.Status != domain.OrderStatusFilled || order.FilledQuantity != 2 || order.FilledAvgPrice == nil || *order.FilledAvgPrice != price {
		t.Fatalf("unexpected paper fill: id=%q order=%+v", externalID, order)
	}
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	wantCash := 10000 - (price * 2 * 100) - (2 * DefaultOptionFeePerContract)
	if balance.Cash != wantCash {
		t.Fatalf("cash = %.2f, want %.2f", balance.Cash, wantCash)
	}
	if err := broker.RollbackOptionOrder(context.Background(), externalID); err != nil {
		t.Fatalf("RollbackOptionOrder() error = %v", err)
	}
	balance, _ = broker.GetAccountBalance(context.Background())
	if balance.Cash != 10000 {
		t.Fatalf("rollback cash = %.2f, want 10000", balance.Cash)
	}
	if err := broker.RollbackOptionOrder(context.Background(), externalID); err == nil {
		t.Fatal("duplicate option rollback must fail")
	}
}

func TestSubmitSpreadOrderFailsClosed(t *testing.T) {
	broker := NewPaperBroker(10000, 0, 0)
	if _, err := broker.SubmitSpreadOrder(context.Background(), &domain.OptionSpread{}, 1); err == nil {
		t.Fatal("expected malformed spread to fail closed")
	}
}

func TestSubmitSpreadOrderAtomicallyDebitsVertical(t *testing.T) {
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	spread := &domain.OptionSpread{StrategyType: domain.StrategyBullCallSpread, Underlying: "AAPL", MaxRisk: 150, MaxReward: 350, Legs: []domain.SpreadLeg{
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00150000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 150, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideBuy, PositionIntent: domain.PositionIntentBuyToOpen, Ratio: 1, ExecutablePrice: 2.5},
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00155000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 155, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideSell, PositionIntent: domain.PositionIntentSellToOpen, Ratio: 1, ExecutablePrice: 1},
	}}
	broker := NewPaperBroker(10000, 0, 0)
	ids, err := broker.SubmitSpreadOrder(context.Background(), spread, 1)
	if err != nil || len(ids) != 2 {
		t.Fatalf("SubmitSpreadOrder() ids=%v err=%v", ids, err)
	}
	balance, _ := broker.GetAccountBalance(context.Background())
	if math.Abs(balance.Cash-9848.70) > 1e-9 {
		t.Fatalf("cash = %.2f, want 9848.70", balance.Cash)
	}
	if err := broker.RollbackOptionSpread(context.Background(), ids); err != nil {
		t.Fatalf("RollbackOptionSpread() error = %v", err)
	}
	balance, _ = broker.GetAccountBalance(context.Background())
	if balance.Cash != 10000 {
		t.Fatalf("spread rollback cash = %.2f, want 10000", balance.Cash)
	}
	if err := broker.RollbackOptionSpread(context.Background(), ids); err == nil {
		t.Fatal("duplicate spread rollback must fail")
	}
}

func TestFinalizeOptionSpreadRemovesCompensationRecord(t *testing.T) {
	expiry := time.Date(2027, 12, 17, 0, 0, 0, 0, time.UTC)
	spread := &domain.OptionSpread{StrategyType: domain.StrategyBullCallSpread, Underlying: "AAPL", MaxRisk: 150, MaxReward: 350, Legs: []domain.SpreadLeg{
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00150000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 150, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideBuy, PositionIntent: domain.PositionIntentBuyToOpen, Ratio: 1, ExecutablePrice: 2.5},
		{Contract: domain.OptionContract{OCCSymbol: "AAPL271217C00155000", Underlying: "AAPL", OptionType: domain.OptionTypeCall, Strike: 155, Expiry: expiry, Multiplier: 100}, Side: domain.OrderSideSell, PositionIntent: domain.PositionIntentSellToOpen, Ratio: 1, ExecutablePrice: 1},
	}}
	broker := NewPaperBroker(10000, 0, 0)
	ids, err := broker.SubmitSpreadOrder(context.Background(), spread, 1)
	if err != nil {
		t.Fatalf("SubmitSpreadOrder() error = %v", err)
	}
	if err := broker.FinalizeOptionSpread(ids); err != nil {
		t.Fatalf("FinalizeOptionSpread() error = %v", err)
	}
	if err := broker.RollbackOptionSpread(context.Background(), ids); err == nil {
		t.Fatal("finalized spread must not retain a compensation record")
	}
}
