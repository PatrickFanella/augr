package kalshi

import (
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/google/uuid"
)

func TestMapCreateOrderRequestYesLimitBuy(t *testing.T) {
	t.Parallel()

	price := 0.42
	orderID := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")

	req, err := mapCreateOrderRequest(&domain.Order{
		ID:             orderID,
		Ticker:         "  KX-EXAMPLE  ",
		Side:           domain.OrderSideBuy,
		OrderType:      domain.OrderTypeLimit,
		Quantity:       3,
		LimitPrice:     &price,
		PredictionSide: "YES",
	})
	if err != nil {
		t.Fatalf("mapCreateOrderRequest() error = %v", err)
	}
	if req.Ticker != "KX-EXAMPLE" {
		t.Fatalf("Ticker = %q, want %q", req.Ticker, "KX-EXAMPLE")
	}
	if req.Side != "yes" || req.Action != "buy" || req.Type != "limit" || req.Count != 3 {
		t.Fatalf("request = %#v", req)
	}
	if req.YesPrice == nil || *req.YesPrice != 42 {
		t.Fatalf("YesPrice = %#v, want 42", req.YesPrice)
	}
	if req.NoPrice != nil {
		t.Fatalf("NoPrice = %#v, want nil", req.NoPrice)
	}
	if req.ClientOrderID != orderID.String() {
		t.Fatalf("ClientOrderID = %q, want %q", req.ClientOrderID, orderID.String())
	}
}

func TestMapCreateOrderRequestSellMarket(t *testing.T) {
	t.Parallel()

	req, err := mapCreateOrderRequest(&domain.Order{
		Ticker:         "KX-EXAMPLE",
		Side:           domain.OrderSideSell,
		OrderType:      domain.OrderTypeMarket,
		Quantity:       1,
		PredictionSide: "YES",
	})
	if err != nil {
		t.Fatalf("mapCreateOrderRequest() error = %v", err)
	}
	if req.Action != "sell" || req.Type != "market" || req.Side != "yes" || req.Count != 1 {
		t.Fatalf("request = %#v", req)
	}
}

func TestMapCreateOrderRequestNoLimitBuy(t *testing.T) {
	t.Parallel()

	price := 0.58
	req, err := mapCreateOrderRequest(&domain.Order{
		Ticker:         "KX-EXAMPLE",
		Side:           domain.OrderSideBuy,
		OrderType:      domain.OrderTypeLimit,
		Quantity:       2,
		LimitPrice:     &price,
		PredictionSide: " no ",
	})
	if err != nil {
		t.Fatalf("mapCreateOrderRequest() error = %v", err)
	}
	if req.Side != "no" || req.NoPrice == nil || *req.NoPrice != 58 || req.YesPrice != nil {
		t.Fatalf("request = %#v, want no-side price", req)
	}
}

func TestMapCreateOrderRequestRejectsMissingLimitPrice(t *testing.T) {
	t.Parallel()

	_, err := mapCreateOrderRequest(&domain.Order{
		Ticker:         "KX-EXAMPLE",
		Side:           domain.OrderSideBuy,
		OrderType:      domain.OrderTypeLimit,
		Quantity:       1,
		PredictionSide: "YES",
	})
	if err == nil {
		t.Fatal("mapCreateOrderRequest() error = nil, want error")
	}
}

func TestMapCreateOrderRequestRejectsUnsupportedOrderType(t *testing.T) {
	t.Parallel()

	price := 0.42
	_, err := mapCreateOrderRequest(&domain.Order{
		Ticker:         "KX-EXAMPLE",
		Side:           domain.OrderSideBuy,
		OrderType:      domain.OrderTypeStop,
		Quantity:       1,
		LimitPrice:     &price,
		PredictionSide: "YES",
	})
	if err == nil {
		t.Fatal("mapCreateOrderRequest() error = nil, want error")
	}
}

func TestMapCreateOrderRequestRejectsMissingPredictionSide(t *testing.T) {
	t.Parallel()

	_, err := mapCreateOrderRequest(&domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1})
	if err == nil {
		t.Fatal("mapCreateOrderRequest() error = nil, want error")
	}
}

func TestMapCreateOrderRequestRejectsFractionalQuantity(t *testing.T) {
	t.Parallel()

	_, err := mapCreateOrderRequest(&domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeMarket, Quantity: 1.5, PredictionSide: "YES"})
	if err == nil {
		t.Fatal("mapCreateOrderRequest() error = nil, want error")
	}
}

func TestMapCreateOrderRequestRejectsOutOfRangeLimitPrice(t *testing.T) {
	t.Parallel()

	price := 1.25
	_, err := mapCreateOrderRequest(&domain.Order{Ticker: "KX-EXAMPLE", Side: domain.OrderSideBuy, OrderType: domain.OrderTypeLimit, Quantity: 1, LimitPrice: &price, PredictionSide: "YES"})
	if err == nil {
		t.Fatal("mapCreateOrderRequest() error = nil, want error")
	}
}

func TestMapOrderStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		raw  string
		want domain.OrderStatus
	}{
		{name: "submitted_resting", raw: " resting ", want: domain.OrderStatusSubmitted},
		{name: "submitted_open", raw: "open", want: domain.OrderStatusSubmitted},
		{name: "submitted_pending", raw: "pending", want: domain.OrderStatusSubmitted},
		{name: "filled_executed", raw: "EXECUTED", want: domain.OrderStatusFilled},
		{name: "filled_alias", raw: "filled", want: domain.OrderStatusFilled},
		{name: "partial_partially_executed", raw: "partially_executed", want: domain.OrderStatusPartial},
		{name: "partial_alias", raw: " partial ", want: domain.OrderStatusPartial},
		{name: "cancelled_canceled", raw: " canceled ", want: domain.OrderStatusCancelled},
		{name: "cancelled_alias", raw: "cancelled_by_user", want: domain.OrderStatusCancelled},
		{name: "rejected", raw: "rejected", want: domain.OrderStatusRejected},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := mapOrderStatus(tc.raw)
			if err != nil {
				t.Fatalf("mapOrderStatus(%q) error = %v", tc.raw, err)
			}
			if got != tc.want {
				t.Fatalf("mapOrderStatus(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestMapOrderStatusRejectsUnsupportedStatus(t *testing.T) {
	t.Parallel()

	if _, err := mapOrderStatus("unknown"); err == nil {
		t.Fatal("mapOrderStatus() error = nil, want error")
	}
}
