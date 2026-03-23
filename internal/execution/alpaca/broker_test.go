package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestBrokerCancelOrder_DeletesOrder(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodDelete)
		}
		requests <- r.URL.Path
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
		if path != "/v2/orders/order%2F1" {
			t.Fatalf("request path = %s, want %s", path, "/v2/orders/order%2F1")
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

func floatPtr(value float64) *float64 {
	return &value
}
