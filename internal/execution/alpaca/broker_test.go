package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestBrokerSubmitOrder_MapsOrderTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		order *domain.Order
		want  map[string]string
	}{
		{
			name: "market",
			order: &domain.Order{
				Ticker:    "AAPL",
				Side:      domain.OrderSideBuy,
				OrderType: domain.OrderTypeMarket,
				Quantity:  1.25,
			},
			want: map[string]string{
				"symbol":        "AAPL",
				"qty":           "1.25",
				"side":          "buy",
				"type":          "market",
				"time_in_force": "day",
			},
		},
		{
			name: "limit",
			order: &domain.Order{
				Ticker:     "AAPL",
				Side:       domain.OrderSideBuy,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   2,
				LimitPrice: floatPtr(185.25),
			},
			want: map[string]string{
				"symbol":        "AAPL",
				"qty":           "2",
				"side":          "buy",
				"type":          "limit",
				"time_in_force": "day",
				"limit_price":   "185.25",
			},
		},
		{
			name: "stop",
			order: &domain.Order{
				Ticker:    "AAPL",
				Side:      domain.OrderSideSell,
				OrderType: domain.OrderTypeStop,
				Quantity:  3,
				StopPrice: floatPtr(180.5),
			},
			want: map[string]string{
				"symbol":        "AAPL",
				"qty":           "3",
				"side":          "sell",
				"type":          "stop",
				"time_in_force": "day",
				"stop_price":    "180.5",
			},
		},
		{
			name: "stop limit",
			order: &domain.Order{
				Ticker:     "AAPL",
				Side:       domain.OrderSideSell,
				OrderType:  domain.OrderTypeStopLimit,
				Quantity:   4,
				LimitPrice: floatPtr(179.75),
				StopPrice:  floatPtr(180),
			},
			want: map[string]string{
				"symbol":        "AAPL",
				"qty":           "4",
				"side":          "sell",
				"type":          "stop_limit",
				"time_in_force": "day",
				"limit_price":   "179.75",
				"stop_price":    "180",
			},
		},
		{
			name: "trailing stop",
			order: &domain.Order{
				Ticker:    "AAPL",
				Side:      domain.OrderSideSell,
				OrderType: domain.OrderTypeTrailingStop,
				Quantity:  5,
				StopPrice: floatPtr(1.5),
			},
			want: map[string]string{
				"symbol":        "AAPL",
				"qty":           "5",
				"side":          "sell",
				"type":          "trailing_stop",
				"time_in_force": "day",
				"trail_price":   "1.5",
			},
		},
		{
			name: "trailing stop percent",
			order: &domain.Order{
				Ticker:     "AAPL",
				Side:       domain.OrderSideSell,
				OrderType:  domain.OrderTypeTrailingStop,
				Quantity:   5,
				LimitPrice: floatPtr(1.5),
			},
			want: map[string]string{
				"symbol":        "AAPL",
				"qty":           "5",
				"side":          "sell",
				"type":          "trailing_stop",
				"time_in_force": "day",
				"trail_percent": "1.5",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan map[string]any, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					t.Fatalf("request method = %s, want %s", r.Method, http.MethodPost)
				}
				if r.URL.Path != "/v2/orders" {
					t.Fatalf("request path = %s, want %s", r.URL.Path, "/v2/orders")
				}

				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				requests <- payload

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"alpaca-order-1"}`))
			}))
			defer server.Close()

			client := NewClient("test-key", "test-secret", true, discardLogger())
			client.SetBaseURL(server.URL)

			broker := NewBroker(client)
			externalID, err := broker.SubmitOrder(context.Background(), tt.order)
			if err != nil {
				t.Fatalf("SubmitOrder() error = %v", err)
			}
			if externalID != "alpaca-order-1" {
				t.Fatalf("SubmitOrder() externalID = %q, want %q", externalID, "alpaca-order-1")
			}

			select {
			case request := <-requests:
				for key, want := range tt.want {
					if got := request[key]; got != want {
						t.Fatalf("%s = %v, want %q", key, got, want)
					}
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestBrokerSubmitOrder_HandlesAlpacaErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"code":42210000,"message":"insufficient buying power"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("SubmitOrder() error type = %T, want wrapped *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusUnprocessableEntity {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusUnprocessableEntity)
	}
	if apiErr.Code != 42210000 {
		t.Fatalf("Code = %d, want %d", apiErr.Code, 42210000)
	}
}

func TestBrokerSubmitOrder_RejectsUnsupportedOrderSide(t *testing.T) {
	t.Parallel()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	broker := NewBroker(client)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "AAPL",
		Side:      domain.OrderSide("hold"),
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	if err.Error() != `alpaca: unsupported order side "hold"` {
		t.Fatalf("SubmitOrder() error = %q, want unsupported side error", err.Error())
	}
}

func TestBrokerCancelOrder_DeletesOrder(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodDelete)
		}
		requests <- r.RequestURI
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	if err := broker.CancelOrder(context.Background(), "order/1"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}

	select {
	case path := <-requests:
		wantPath := "/v2/orders/" + url.PathEscape("order/1")
		if path != wantPath {
			t.Fatalf("request path = %s, want %s", path, wantPath)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerCancelOrder_HandlesAlpacaErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":40410000,"message":"order not found"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	err := broker.CancelOrder(context.Background(), "missing")
	if err == nil {
		t.Fatal("CancelOrder() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("CancelOrder() error type = %T, want wrapped *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusNotFound {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusNotFound)
	}
	if apiErr.Code != 40410000 {
		t.Fatalf("Code = %d, want %d", apiErr.Code, 40410000)
	}
}

func TestBrokerGetOrderStatus_MapsAlpacaStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		orderID    string
		apiStatus  string
		wantStatus domain.OrderStatus
	}{
		{
			name:       "pending",
			orderID:    "order/1",
			apiStatus:  "pending_new",
			wantStatus: domain.OrderStatusPending,
		},
		{
			name:       "submitted",
			orderID:    "order-2",
			apiStatus:  "accepted",
			wantStatus: domain.OrderStatusSubmitted,
		},
		{
			name:       "partial",
			orderID:    "order-3",
			apiStatus:  "partially_filled",
			wantStatus: domain.OrderStatusPartial,
		},
		{
			name:       "filled",
			orderID:    "order-4",
			apiStatus:  "filled",
			wantStatus: domain.OrderStatusFilled,
		},
		{
			name:       "cancelled",
			orderID:    "order-5",
			apiStatus:  "expired",
			wantStatus: domain.OrderStatusCancelled,
		},
		{
			name:       "rejected",
			orderID:    "order-6",
			apiStatus:  "rejected",
			wantStatus: domain.OrderStatusRejected,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan string, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Fatalf("request method = %s, want %s", r.Method, http.MethodGet)
				}
				requests <- r.RequestURI

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"` + tt.apiStatus + `"}`))
			}))
			defer server.Close()

			client := NewClient("test-key", "test-secret", true, discardLogger())
			client.SetBaseURL(server.URL)

			broker := NewBroker(client)
			got, err := broker.GetOrderStatus(context.Background(), tt.orderID)
			if err != nil {
				t.Fatalf("GetOrderStatus() error = %v", err)
			}
			if got != tt.wantStatus {
				t.Fatalf("GetOrderStatus() = %q, want %q", got, tt.wantStatus)
			}

			select {
			case path := <-requests:
				wantPath := "/v2/orders/" + url.PathEscape(tt.orderID)
				if path != wantPath {
					t.Fatalf("request path = %s, want %s", path, wantPath)
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestBrokerGetPositions_MapsResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodGet)
		}
		requests <- r.RequestURI

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"symbol":"AAPL","side":"long","qty":"2.5","avg_entry_price":"185.25","current_price":"190.10","unrealized_pl":"12.13"},
			{"symbol":"TSLA","side":"short","qty":"1","avg_entry_price":"210.5","current_price":"205.25","unrealized_pl":"5.25"}
		]`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("len(GetPositions()) = %d, want %d", len(positions), 2)
	}

	if positions[0].Ticker != "AAPL" {
		t.Fatalf("positions[0].Ticker = %q, want %q", positions[0].Ticker, "AAPL")
	}
	if positions[0].Side != domain.PositionSideLong {
		t.Fatalf("positions[0].Side = %q, want %q", positions[0].Side, domain.PositionSideLong)
	}
	if positions[0].Quantity != 2.5 {
		t.Fatalf("positions[0].Quantity = %v, want %v", positions[0].Quantity, 2.5)
	}
	if positions[0].AvgEntry != 185.25 {
		t.Fatalf("positions[0].AvgEntry = %v, want %v", positions[0].AvgEntry, 185.25)
	}
	if positions[0].CurrentPrice == nil || *positions[0].CurrentPrice != 190.10 {
		t.Fatalf("positions[0].CurrentPrice = %v, want %v", positions[0].CurrentPrice, 190.10)
	}
	if positions[0].UnrealizedPnL == nil || *positions[0].UnrealizedPnL != 12.13 {
		t.Fatalf("positions[0].UnrealizedPnL = %v, want %v", positions[0].UnrealizedPnL, 12.13)
	}

	if positions[1].Ticker != "TSLA" {
		t.Fatalf("positions[1].Ticker = %q, want %q", positions[1].Ticker, "TSLA")
	}
	if positions[1].Side != domain.PositionSideShort {
		t.Fatalf("positions[1].Side = %q, want %q", positions[1].Side, domain.PositionSideShort)
	}

	select {
	case path := <-requests:
		if path != "/v2/positions" {
			t.Fatalf("request path = %s, want %s", path, "/v2/positions")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerGetAccountBalance_MapsResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodGet)
		}
		requests <- r.RequestURI

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"currency":"USD","cash":"1000.50","buying_power":"4002.00","equity":"5005.25"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if balance.Currency != "USD" {
		t.Fatalf("GetAccountBalance() Currency = %q, want %q", balance.Currency, "USD")
	}
	if balance.Cash != 1000.50 {
		t.Fatalf("GetAccountBalance() Cash = %v, want %v", balance.Cash, 1000.50)
	}
	if balance.BuyingPower != 4002.00 {
		t.Fatalf("GetAccountBalance() BuyingPower = %v, want %v", balance.BuyingPower, 4002.00)
	}
	if balance.Equity != 5005.25 {
		t.Fatalf("GetAccountBalance() Equity = %v, want %v", balance.Equity, 5005.25)
	}

	select {
	case path := <-requests:
		if path != "/v2/account" {
			t.Fatalf("request path = %s, want %s", path, "/v2/account")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
