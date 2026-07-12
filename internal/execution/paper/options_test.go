package paper

import (
	"context"
	"testing"

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
}

func TestSubmitSpreadOrderFailsClosed(t *testing.T) {
	broker := NewPaperBroker(10000, 0, 0)
	if _, err := broker.SubmitSpreadOrder(context.Background(), &domain.OptionSpread{}, 1); err == nil {
		t.Fatal("expected spreads to remain disabled without atomic persistence")
	}
}
