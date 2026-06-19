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

	cases := map[string]domain.OrderStatus{
		" resting ":  domain.OrderStatusSubmitted,
		"EXECUTED":   domain.OrderStatusFilled,
		" canceled ": domain.OrderStatusCancelled,
		"rejected":   domain.OrderStatusRejected,
	}

	for raw, want := range cases {
		raw, want := raw, want
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			got, err := mapOrderStatus(raw)
			if err != nil {
				t.Fatalf("mapOrderStatus(%q) error = %v", raw, err)
			}
			if got != want {
				t.Fatalf("mapOrderStatus(%q) = %q, want %q", raw, got, want)
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
