package polymarket

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

func TestBrokerSubmitOrder_MapsLimitOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		order *domain.Order
		want  map[string]string
	}{
		{
			name: "buy yes outcome",
			order: &domain.Order{
				Ticker:     "token-yes-123",
				Side:       domain.OrderSideBuy,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   10,
				LimitPrice: floatPtr(0.55),
			},
			want: map[string]string{
				"tokenID":     "token-yes-123",
				"price":       "0.55",
				"size":        "10",
				"side":        "BUY",
				"timeInForce": "GTC",
			},
		},
		{
			name: "sell no outcome",
			order: &domain.Order{
				Ticker:     "token-no-456",
				Side:       domain.OrderSideSell,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   5.5,
				LimitPrice: floatPtr(0.35),
			},
			want: map[string]string{
				"tokenID":     "token-no-456",
				"price":       "0.35",
				"size":        "5.5",
				"side":        "SELL",
				"timeInForce": "GTC",
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
				if r.URL.Path != "/order" {
					t.Fatalf("request path = %s, want %s", r.URL.Path, "/order")
				}

				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				requests <- payload

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"orderID":"poly-order-1"}`))
			}))
			defer server.Close()

			client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
			client.SetBaseURL(server.URL)

			broker := NewBroker(client)
			externalID, err := broker.SubmitOrder(context.Background(), tt.order)
			if err != nil {
				t.Fatalf("SubmitOrder() error = %v", err)
			}
			if externalID != "poly-order-1" {
				t.Fatalf("SubmitOrder() externalID = %q, want %q", externalID, "poly-order-1")
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

func TestBrokerSubmitOrder_RejectsUnsupportedOrderType(t *testing.T) {
	t.Parallel()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	broker := NewBroker(client)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "token-123",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	wantErr := `polymarket: unsupported order type "market" (only limit orders are supported)`
	if err.Error() != wantErr {
		t.Fatalf("SubmitOrder() error = %q, want %q", err.Error(), wantErr)
	}
}

func TestBrokerSubmitOrder_RejectsUnsupportedOrderSide(t *testing.T) {
	t.Parallel()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	broker := NewBroker(client)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:     "token-123",
		Side:       domain.OrderSide("hold"),
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: floatPtr(0.5),
	})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	if err.Error() != `polymarket: unsupported order side "hold"` {
		t.Fatalf("SubmitOrder() error = %q, want unsupported side error", err.Error())
	}
}

func TestBrokerSubmitOrder_RejectsInvalidLimitPrice(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		limitPrice float64
		wantErr    string
	}{
		{
			name:       "price above 1",
			limitPrice: 1.5,
			wantErr:    "polymarket: limit price must be between 0 and 1",
		},
		{
			name:       "negative price",
			limitPrice: -0.1,
			wantErr:    "polymarket: limit price must be between 0 and 1",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
			broker := NewBroker(client)

			_, err := broker.SubmitOrder(context.Background(), &domain.Order{
				Ticker:     "token-123",
				Side:       domain.OrderSideBuy,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   1,
				LimitPrice: floatPtr(tt.limitPrice),
			})
			if err == nil {
				t.Fatal("SubmitOrder() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("SubmitOrder() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestBrokerSubmitOrder_RejectsMissingFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		order   *domain.Order
		wantErr string
	}{
		{
			name:    "nil order",
			order:   nil,
			wantErr: "polymarket: order is required",
		},
		{
			name: "empty ticker",
			order: &domain.Order{
				Ticker:     "",
				Side:       domain.OrderSideBuy,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   1,
				LimitPrice: floatPtr(0.5),
			},
			wantErr: "polymarket: order ticker (token ID) is required",
		},
		{
			name: "zero quantity",
			order: &domain.Order{
				Ticker:     "token-123",
				Side:       domain.OrderSideBuy,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   0,
				LimitPrice: floatPtr(0.5),
			},
			wantErr: "polymarket: order quantity must be greater than zero",
		},
		{
			name: "missing limit price",
			order: &domain.Order{
				Ticker:    "token-123",
				Side:      domain.OrderSideBuy,
				OrderType: domain.OrderTypeLimit,
				Quantity:  1,
			},
			wantErr: "polymarket: limit order requires limit price",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
			broker := NewBroker(client)

			_, err := broker.SubmitOrder(context.Background(), tt.order)
			if err == nil {
				t.Fatal("SubmitOrder() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("SubmitOrder() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestBrokerSubmitOrder_HandlesErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"insufficient balance"}`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:     "token-123",
		Side:       domain.OrderSideBuy,
		OrderType:  domain.OrderTypeLimit,
		Quantity:   1,
		LimitPrice: floatPtr(0.5),
	})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("SubmitOrder() error type = %T, want wrapped *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusBadRequest {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusBadRequest)
	}
}

func TestBrokerCancelOrder_DeletesOrder(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodDelete)
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests <- payload

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	if err := broker.CancelOrder(context.Background(), "order-1"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}

	select {
	case payload := <-requests:
		if payload["orderID"] != "order-1" {
			t.Fatalf("orderID = %v, want %q", payload["orderID"], "order-1")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerCancelOrder_RejectsEmptyID(t *testing.T) {
	t.Parallel()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	broker := NewBroker(client)

	err := broker.CancelOrder(context.Background(), "   ")
	if err == nil {
		t.Fatal("CancelOrder() error = nil, want non-nil")
	}
	if err.Error() != "polymarket: external order id is required" {
		t.Fatalf("CancelOrder() error = %q, want external order id required", err.Error())
	}
}

func TestBrokerCancelOrder_HandlesErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"order not found"}`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
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
}

func TestBrokerGetOrderStatus_MapsStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		orderID    string
		apiStatus  string
		wantStatus domain.OrderStatus
	}{
		{
			name:       "live",
			orderID:    "order-1",
			apiStatus:  "live",
			wantStatus: domain.OrderStatusSubmitted,
		},
		{
			name:       "delayed",
			orderID:    "order-2",
			apiStatus:  "delayed",
			wantStatus: domain.OrderStatusSubmitted,
		},
		{
			name:       "matched",
			orderID:    "order-3",
			apiStatus:  "matched",
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
			apiStatus:  "cancelled",
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

			client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
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
				wantPath := "/order/" + url.PathEscape(tt.orderID)
				if path != wantPath {
					t.Fatalf("request path = %s, want %s", path, wantPath)
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestBrokerGetOrderStatus_RejectsInvalidStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		apiStatus string
		wantErr   string
	}{
		{
			name:      "blank",
			apiStatus: "   ",
			wantErr:   "polymarket: order status is required",
		},
		{
			name:      "unknown",
			apiStatus: "routing",
			wantErr:   `polymarket: unsupported order status "routing"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"` + tt.apiStatus + `"}`))
			}))
			defer server.Close()

			client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
			client.SetBaseURL(server.URL)

			broker := NewBroker(client)
			_, err := broker.GetOrderStatus(context.Background(), "order-1")
			if err == nil {
				t.Fatal("GetOrderStatus() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("GetOrderStatus() error = %q, want %q", err.Error(), tt.wantErr)
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
			{"asset":"token-123","size":"100","avgPrice":"0.55","outcome":"Yes"},
			{"asset":"token-456","size":"50","avgPrice":"0.30","outcome":"No"}
		]`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("len(GetPositions()) = %d, want %d", len(positions), 2)
	}

	if positions[0].Ticker != "token-123:Yes" {
		t.Fatalf("positions[0].Ticker = %q, want %q", positions[0].Ticker, "token-123:Yes")
	}
	if positions[0].Side != domain.PositionSideLong {
		t.Fatalf("positions[0].Side = %q, want %q", positions[0].Side, domain.PositionSideLong)
	}
	if positions[0].Quantity != 100 {
		t.Fatalf("positions[0].Quantity = %v, want %v", positions[0].Quantity, 100)
	}
	if positions[0].AvgEntry != 0.55 {
		t.Fatalf("positions[0].AvgEntry = %v, want %v", positions[0].AvgEntry, 0.55)
	}

	if positions[1].Ticker != "token-456:No" {
		t.Fatalf("positions[1].Ticker = %q, want %q", positions[1].Ticker, "token-456:No")
	}
	if positions[1].Quantity != 50 {
		t.Fatalf("positions[1].Quantity = %v, want %v", positions[1].Quantity, 50)
	}
	if positions[1].AvgEntry != 0.30 {
		t.Fatalf("positions[1].AvgEntry = %v, want %v", positions[1].AvgEntry, 0.30)
	}

	select {
	case path := <-requests:
		if path != "/positions" {
			t.Fatalf("request path = %s, want %s", path, "/positions")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerGetPositions_RejectsInvalidNumericField(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"asset":"token-123","size":"oops","avgPrice":"0.55","outcome":"Yes"}
		]`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	_, err := broker.GetPositions(context.Background())
	if err == nil {
		t.Fatal("GetPositions() error = nil, want non-nil")
	}
	wantErr := `polymarket: parse size: strconv.ParseFloat: parsing "oops": invalid syntax`
	if err.Error() != wantErr {
		t.Fatalf("GetPositions() error = %q, want %q", err.Error(), wantErr)
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
		_, _ = w.Write([]byte(`{"balance":"1000.50"}`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if balance.Currency != "USDC" {
		t.Fatalf("GetAccountBalance() Currency = %q, want %q", balance.Currency, "USDC")
	}
	if balance.Cash != 1000.50 {
		t.Fatalf("GetAccountBalance() Cash = %v, want %v", balance.Cash, 1000.50)
	}
	if balance.BuyingPower != 1000.50 {
		t.Fatalf("GetAccountBalance() BuyingPower = %v, want %v", balance.BuyingPower, 1000.50)
	}
	if balance.Equity != 1000.50 {
		t.Fatalf("GetAccountBalance() Equity = %v, want %v", balance.Equity, 1000.50)
	}

	select {
	case path := <-requests:
		if path != "/balance" {
			t.Fatalf("request path = %s, want %s", path, "/balance")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerGetAccountBalance_RejectsInvalidBalance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "blank balance",
			body:    `{"balance":"   "}`,
			wantErr: "polymarket: balance is required",
		},
		{
			name:    "invalid balance",
			body:    `{"balance":"bad"}`,
			wantErr: `polymarket: parse balance: strconv.ParseFloat: parsing "bad": invalid syntax`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
			client.SetBaseURL(server.URL)

			broker := NewBroker(client)
			_, err := broker.GetAccountBalance(context.Background())
			if err == nil {
				t.Fatal("GetAccountBalance() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("GetAccountBalance() error = %q, want %q", err.Error(), tt.wantErr)
			}
		})
	}
}

func TestBrokerNilClient_ReturnsError(t *testing.T) {
	t.Parallel()

	broker := NewBroker(nil)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{})
	if err == nil || err.Error() != "polymarket: broker client is required" {
		t.Fatalf("SubmitOrder() error = %v, want client required error", err)
	}

	err = broker.CancelOrder(context.Background(), "order-1")
	if err == nil || err.Error() != "polymarket: broker client is required" {
		t.Fatalf("CancelOrder() error = %v, want client required error", err)
	}

	_, err = broker.GetOrderStatus(context.Background(), "order-1")
	if err == nil || err.Error() != "polymarket: broker client is required" {
		t.Fatalf("GetOrderStatus() error = %v, want client required error", err)
	}

	_, err = broker.GetPositions(context.Background())
	if err == nil || err.Error() != "polymarket: broker client is required" {
		t.Fatalf("GetPositions() error = %v, want client required error", err)
	}

	_, err = broker.GetAccountBalance(context.Background())
	if err == nil || err.Error() != "polymarket: broker client is required" {
		t.Fatalf("GetAccountBalance() error = %v, want client required error", err)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
