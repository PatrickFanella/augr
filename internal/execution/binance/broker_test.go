package binance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestBrokerSubmitOrder_MapsSupportedOrderTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		order          *domain.Order
		wantExternalID string
		wantQuery      map[string]string
	}{
		{
			name: "market",
			order: &domain.Order{
				Ticker:    "btcusdt",
				Side:      domain.OrderSideBuy,
				OrderType: domain.OrderTypeMarket,
				Quantity:  0.25,
			},
			wantExternalID: "BTCUSDT:12345",
			wantQuery: map[string]string{
				"symbol":   "BTCUSDT",
				"side":     "BUY",
				"type":     "MARKET",
				"quantity": "0.25",
			},
		},
		{
			name: "limit",
			order: &domain.Order{
				Ticker:     "ETHUSDT",
				Side:       domain.OrderSideSell,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   1.5,
				LimitPrice: floatPtr(3150.5),
			},
			wantExternalID: "ETHUSDT:12345",
			wantQuery: map[string]string{
				"symbol":      "ETHUSDT",
				"side":        "SELL",
				"type":        "LIMIT",
				"quantity":    "1.5",
				"price":       "3150.5",
				"timeInForce": "GTC",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan struct {
				method string
				path   string
				query  url.Values
			}, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- struct {
					method string
					path   string
					query  url.Values
				}{
					method: r.Method,
					path:   r.URL.Path,
					query:  r.URL.Query(),
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"symbol":"` + tt.wantQuery["symbol"] + `","orderId":12345}`))
			}))
			defer server.Close()

			client := NewClient("test-key", "test-secret", true, discardLogger())
			client.SetBaseURL(server.URL)
			client.now = func() time.Time {
				return time.UnixMilli(1700000000123)
			}

			broker := NewBroker(client)
			externalID, err := broker.SubmitOrder(context.Background(), tt.order)
			if err != nil {
				t.Fatalf("SubmitOrder() error = %v", err)
			}
			if externalID != tt.wantExternalID {
				t.Fatalf("SubmitOrder() externalID = %q, want %q", externalID, tt.wantExternalID)
			}

			select {
			case request := <-requests:
				if request.method != http.MethodPost {
					t.Fatalf("request method = %s, want %s", request.method, http.MethodPost)
				}
				if request.path != "/api/v3/order" {
					t.Fatalf("request path = %s, want %s", request.path, "/api/v3/order")
				}
				for key, want := range tt.wantQuery {
					if got := request.query.Get(key); got != want {
						t.Fatalf("%s = %q, want %q", key, got, want)
					}
				}
				if request.query.Get("timestamp") == "" {
					t.Fatal("timestamp query param is missing")
				}
				if request.query.Get("signature") == "" {
					t.Fatal("signature query param is missing")
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestBrokerSubmitOrder_RejectsUnsupportedOrderType(t *testing.T) {
	t.Parallel()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	broker := NewBroker(client)

	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "BTCUSDT",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeStop,
		Quantity:  1,
	})
	if err == nil {
		t.Fatal("SubmitOrder() error = nil, want non-nil")
	}
	if err.Error() != `binance: unsupported order type "stop"` {
		t.Fatalf("SubmitOrder() error = %q, want unsupported type error", err.Error())
	}
}

func TestBrokerCancelOrder_UsesCompositeExternalID(t *testing.T) {
	t.Parallel()

	requests := make(chan struct {
		method string
		path   string
		query  url.Values
	}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- struct {
			method string
			path   string
			query  url.Values
		}{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"symbol":"BTCUSDT","orderId":12345}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)
	client.now = func() time.Time {
		return time.UnixMilli(1700000000123)
	}

	broker := NewBroker(client)
	if err := broker.CancelOrder(context.Background(), "btcusdt:12345"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodDelete {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodDelete)
		}
		if request.path != "/api/v3/order" {
			t.Fatalf("request path = %s, want %s", request.path, "/api/v3/order")
		}
		if got := request.query.Get("symbol"); got != "BTCUSDT" {
			t.Fatalf("symbol = %q, want %q", got, "BTCUSDT")
		}
		if got := request.query.Get("orderId"); got != "12345" {
			t.Fatalf("orderId = %q, want %q", got, "12345")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerGetOrderStatus_MapsBinanceStatuses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		apiStatus  string
		wantStatus domain.OrderStatus
	}{
		{name: "pending", apiStatus: "PENDING_CANCEL", wantStatus: domain.OrderStatusPending},
		{name: "submitted", apiStatus: "NEW", wantStatus: domain.OrderStatusSubmitted},
		{name: "partial", apiStatus: "PARTIALLY_FILLED", wantStatus: domain.OrderStatusPartial},
		{name: "filled", apiStatus: "FILLED", wantStatus: domain.OrderStatusFilled},
		{name: "cancelled", apiStatus: "EXPIRED", wantStatus: domain.OrderStatusCancelled},
		{name: "rejected", apiStatus: "REJECTED", wantStatus: domain.OrderStatusRejected},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan url.Values, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- r.URL.Query()

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"status":"` + tt.apiStatus + `"}`))
			}))
			defer server.Close()

			client := NewClient("test-key", "test-secret", true, discardLogger())
			client.SetBaseURL(server.URL)
			client.now = func() time.Time {
				return time.UnixMilli(1700000000456)
			}

			broker := NewBroker(client)
			got, err := broker.GetOrderStatus(context.Background(), "ETHUSDT:999")
			if err != nil {
				t.Fatalf("GetOrderStatus() error = %v", err)
			}
			if got != tt.wantStatus {
				t.Fatalf("GetOrderStatus() = %q, want %q", got, tt.wantStatus)
			}

			select {
			case query := <-requests:
				if query.Get("symbol") != "ETHUSDT" {
					t.Fatalf("symbol = %q, want %q", query.Get("symbol"), "ETHUSDT")
				}
				if query.Get("orderId") != "999" {
					t.Fatalf("orderId = %q, want %q", query.Get("orderId"), "999")
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestBrokerGetOrderStatus_RejectsMalformedExternalID(t *testing.T) {
	t.Parallel()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	broker := NewBroker(client)

	_, err := broker.GetOrderStatus(context.Background(), "12345")
	if err == nil {
		t.Fatal("GetOrderStatus() error = nil, want non-nil")
	}
	wantErr := `binance: external order id "12345" must be in SYMBOL:ORDER_ID format`
	if err.Error() != wantErr {
		t.Fatalf("GetOrderStatus() error = %q, want %q", err.Error(), wantErr)
	}
}

func TestBrokerGetPositions_MapsAccountBalances(t *testing.T) {
	t.Parallel()

	requests := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"balances": [
				{"asset":"BTC","free":"0.50","locked":"0.10"},
				{"asset":"USDT","free":"0","locked":"0"},
				{"asset":"ETH","free":"0","locked":"2.00"}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)
	client.now = func() time.Time {
		return time.UnixMilli(1700000000456)
	}

	broker := NewBroker(client)
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("len(GetPositions()) = %d, want %d", len(positions), 2)
	}

	if positions[0].Ticker != "BTC" {
		t.Fatalf("positions[0].Ticker = %q, want %q", positions[0].Ticker, "BTC")
	}
	if positions[0].Side != domain.PositionSideLong {
		t.Fatalf("positions[0].Side = %q, want %q", positions[0].Side, domain.PositionSideLong)
	}
	if positions[0].Quantity != 0.6 {
		t.Fatalf("positions[0].Quantity = %v, want %v", positions[0].Quantity, 0.6)
	}
	if positions[0].AvgEntry != 0 {
		t.Fatalf("positions[0].AvgEntry = %v, want 0", positions[0].AvgEntry)
	}

	if positions[1].Ticker != "ETH" {
		t.Fatalf("positions[1].Ticker = %q, want %q", positions[1].Ticker, "ETH")
	}
	if positions[1].Quantity != 2 {
		t.Fatalf("positions[1].Quantity = %v, want %v", positions[1].Quantity, 2.0)
	}

	select {
	case query := <-requests:
		if query.Get("timestamp") == "" {
			t.Fatal("timestamp query param is missing")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerGetPositions_RejectsInvalidNumericField(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"balances": [
				{"asset":"BTC","free":"oops","locked":"0.10"}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	_, err := broker.GetPositions(context.Background())
	if err == nil {
		t.Fatal("GetPositions() error = nil, want non-nil")
	}
	wantErr := `binance: parse free: strconv.ParseFloat: parsing "oops": invalid syntax`
	if err.Error() != wantErr {
		t.Fatalf("GetPositions() error = %q, want %q", err.Error(), wantErr)
	}
}

func TestBrokerGetAccountBalance_PrefersStablecoinCashBalance(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"balances": [
				{"asset":"BTC","free":"0.50","locked":"0.10"},
				{"asset":"USDT","free":"1000.50","locked":"250.25"}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if balance.Currency != "USDT" {
		t.Fatalf("GetAccountBalance() Currency = %q, want %q", balance.Currency, "USDT")
	}
	if balance.Cash != 1000.50 {
		t.Fatalf("GetAccountBalance() Cash = %v, want %v", balance.Cash, 1000.50)
	}
	if balance.BuyingPower != 1000.50 {
		t.Fatalf("GetAccountBalance() BuyingPower = %v, want %v", balance.BuyingPower, 1000.50)
	}
	if balance.Equity != 1250.75 {
		t.Fatalf("GetAccountBalance() Equity = %v, want %v", balance.Equity, 1250.75)
	}

	select {
	case path := <-requests:
		if path != "/api/v3/account" {
			t.Fatalf("request path = %s, want %s", path, "/api/v3/account")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestBrokerGetAccountBalance_RejectsInvalidResponseFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		body    string
		wantErr string
	}{
		{
			name:    "missing asset",
			body:    `{"balances":[{"asset":"   ","free":"1","locked":"0"}]}`,
			wantErr: "binance: balance asset is required",
		},
		{
			name:    "invalid free",
			body:    `{"balances":[{"asset":"USDT","free":"bad","locked":"0"}]}`,
			wantErr: `binance: parse free: strconv.ParseFloat: parsing "bad": invalid syntax`,
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

			client := NewClient("test-key", "test-secret", true, discardLogger())
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

func TestBrokerSubmitOrder_DecodeResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"symbol":  "BTCUSDT",
			"orderId": 12345,
		})
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	broker := NewBroker(client)
	externalID, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:    "BTCUSDT",
		Side:      domain.OrderSideBuy,
		OrderType: domain.OrderTypeMarket,
		Quantity:  1,
	})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if externalID != "BTCUSDT:12345" {
		t.Fatalf("SubmitOrder() externalID = %q, want %q", externalID, "BTCUSDT:12345")
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
