package domain

import "testing"

func TestOrderIsReduceOnlyRequiresExplicitCloseIntent(t *testing.T) {
	t.Parallel()

	buyOpen := PositionIntentBuyToOpen
	sellClose := PositionIntentSellToClose
	if (&Order{Side: OrderSideSell}).IsReduceOnly() {
		t.Fatal("bare sell inferred as reduce-only")
	}
	if (&Order{Side: OrderSideBuy, PositionIntent: &buyOpen}).IsReduceOnly() {
		t.Fatal("opening intent treated as reduce-only")
	}
	if !(&Order{Side: OrderSideSell, PositionIntent: &sellClose}).IsReduceOnly() {
		t.Fatal("explicit close intent not treated as reduce-only")
	}
	if (&Order{Side: OrderSideBuy, PositionIntent: &sellClose}).IsReduceOnly() {
		t.Fatal("side/intent mismatch treated as reduce-only")
	}
}
