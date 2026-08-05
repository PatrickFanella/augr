package polymarket

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/risk"
	"github.com/google/uuid"
)

type fakeBreaker struct {
	allowErr   error
	allowCalls int
}

func (f *fakeBreaker) Allow(_ context.Context, _ string) error {
	f.allowCalls++
	return f.allowErr
}
func (f *fakeBreaker) Trip(_ context.Context, _, _ string) error { return nil }
func (f *fakeBreaker) Reset(_ context.Context, _ string) error   { return nil }

type fakeOfficialCLOBClient struct {
	calls    int
	snapshot domain.PolymarketBookSnapshot
	err      error
}

func (f *fakeOfficialCLOBClient) GetOrderBook(_ context.Context, _ string) (domain.PolymarketBookSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestBrokerSendTemplate_RejectsMissingDryRunQuery(t *testing.T) {
	t.Parallel()

	client := newTestClient()
	broker := NewBroker(client)
	broker.DryRun = true

	tmpl, err := NewOrderTemplate(mustDecodeSecretBytes(), http.MethodPost, "https://api.polymarket.us/v1/orders", []byte(`{}`))
	if err != nil {
		t.Fatalf("NewOrderTemplate() error = %v", err)
	}

	_, err = broker.SendTemplate(context.Background(), tmpl)
	if err == nil || err.Error() != "polymarket: dry-run mode active but template URL missing dry=1" {
		t.Fatalf("SendTemplate() error = %v, want dry-run URL error", err)
	}
}

func TestBrokerSendTemplate_RejectsDryRunSubstringWithoutQuery(t *testing.T) {
	t.Parallel()

	client := newTestClient()
	broker := NewBroker(client)
	broker.DryRun = true

	tmpl, err := NewOrderTemplate(mustDecodeSecretBytes(), http.MethodPost, "https://api.polymarket.us/v1/orders?notdry=1", []byte(`{}`))
	if err != nil {
		t.Fatalf("NewOrderTemplate() error = %v", err)
	}

	_, err = broker.SendTemplate(context.Background(), tmpl)
	if err == nil || err.Error() != "polymarket: dry-run mode active but template URL missing dry=1" {
		t.Fatalf("SendTemplate() error = %v, want dry-run URL error", err)
	}
}

func TestBrokerSubmitOrder_DryRunDoesNotUseOfficialCLOBClient(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("dry") != "1" {
			t.Fatalf("request query = %s, want dry=1", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"poly-order-dry"}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	broker := NewBroker(client)
	broker.DryRun = true
	official := &fakeOfficialCLOBClient{}
	broker.SetOfficialCLOBClient(official)

	orderID, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:         "btc-100k-2025",
		Side:           domain.OrderSideBuy,
		PredictionSide: "YES",
		OrderType:      domain.OrderTypeLimit,
		Quantity:       1,
		LimitPrice:     floatPtr(0.5),
	})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if orderID != "poly-order-dry" {
		t.Fatalf("SubmitOrder() externalID = %q, want poly-order-dry", orderID)
	}
	if official.calls != 0 {
		t.Fatalf("official CLOB calls = %d, want 0", official.calls)
	}
}

func TestBrokerGetOrderBook_UsesOfficialCLOBClient(t *testing.T) {
	t.Parallel()

	broker := NewBroker(nil)
	broker.SetOfficialCLOBClient(&fakeOfficialCLOBClient{snapshot: domain.PolymarketBookSnapshot{TokenID: "token-1", BestBid: 0.5, BestAsk: 0.55}})

	snapshot, err := broker.GetOrderBook(context.Background(), "token-1")
	if err != nil {
		t.Fatalf("GetOrderBook() error = %v", err)
	}
	if snapshot.TokenID != "token-1" || snapshot.BestBid != 0.5 || snapshot.BestAsk != 0.55 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
}

func TestBrokerSendTemplate_UsesBreakerScopes(t *testing.T) {
	t.Parallel()

	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"poly-order-1"}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	broker := NewBroker(client)
	breaker := &fakeBreaker{}
	broker.Breaker = breaker

	tmpl, err := NewOrderTemplate(mustDecodeSecretBytes(), http.MethodPost, server.URL+"/v1/orders?dry=1", []byte(`{}`))
	if err != nil {
		t.Fatalf("NewOrderTemplate() error = %v", err)
	}
	tmpl.StrategyID = "strategy-1"

	if _, err := broker.SendTemplate(context.Background(), tmpl); err != nil {
		t.Fatalf("SendTemplate() error = %v", err)
	}
	if breaker.allowCalls != 2 {
		t.Fatalf("breaker allowCalls = %d, want 2", breaker.allowCalls)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

func TestBrokerSubmitOrder_MapsLimitOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		order *domain.Order
		want  map[string]any
	}{
		{
			name: "buy yes outcome",
			order: &domain.Order{
				Ticker:         "btc-100k-2025",
				Side:           domain.OrderSideBuy,
				PredictionSide: "YES",
				OrderType:      domain.OrderTypeLimit,
				Quantity:       10,
				LimitPrice:     floatPtr(0.55),
			},
			want: map[string]any{
				"marketSlug": "btc-100k-2025",
				"intent":     "ORDER_INTENT_BUY_LONG",
				"type":       "ORDER_TYPE_LIMIT",
				"tif":        defaultTimeInForce,
			},
		},
		{
			name: "sell no outcome",
			order: &domain.Order{
				Ticker:         "btc-100k-2025",
				Side:           domain.OrderSideSell,
				PredictionSide: "NO",
				OrderType:      domain.OrderTypeLimit,
				Quantity:       5.5,
				LimitPrice:     floatPtr(0.35),
			},
			want: map[string]any{
				"marketSlug": "btc-100k-2025",
				"intent":     "ORDER_INTENT_SELL_SHORT",
				"type":       "ORDER_TYPE_LIMIT",
				"tif":        defaultTimeInForce,
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
				if r.URL.Path != "/v1/orders" {
					t.Fatalf("request path = %s, want %s", r.URL.Path, "/v1/orders")
				}

				var payload map[string]any
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatalf("Decode() error = %v", err)
				}
				requests <- payload

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"id":"poly-order-1"}`))
			}))
			defer server.Close()

			client := newTestClient()
			client.SetAPIBaseURL(server.URL)

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
						t.Fatalf("%s = %v, want %v", key, got, want)
					}
				}
				price, ok := request["price"].(map[string]any)
				if !ok {
					t.Fatalf("price = %T, want object", request["price"])
				}
				if price["currency"] != "USD" {
					t.Fatalf("price.currency = %v, want %q", price["currency"], "USD")
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestBrokerSubmitOrder_MapsMarketOrder(t *testing.T) {
	t.Parallel()

	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"poly-order-2"}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)

	broker := NewBroker(client)
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:         "btc-100k-2025",
		Side:           domain.OrderSideBuy,
		PredictionSide: "NO",
		OrderType:      domain.OrderTypeMarket,
		Quantity:       2,
	})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}

	select {
	case request := <-requests:
		if request["type"] != "ORDER_TYPE_MARKET" {
			t.Fatalf("type = %v, want %q", request["type"], "ORDER_TYPE_MARKET")
		}
		if request["intent"] != "ORDER_INTENT_BUY_SHORT" {
			t.Fatalf("intent = %v, want %q", request["intent"], "ORDER_INTENT_BUY_SHORT")
		}
		if _, ok := request["price"]; ok {
			t.Fatalf("price present on market order, want omitted")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
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
				Ticker:         "",
				Side:           domain.OrderSideBuy,
				PredictionSide: "YES",
				OrderType:      domain.OrderTypeLimit,
				Quantity:       1,
				LimitPrice:     floatPtr(0.5),
			},
			wantErr: "polymarket: order ticker (market slug) is required",
		},
		{
			name: "missing prediction side",
			order: &domain.Order{
				Ticker:     "btc-100k-2025",
				Side:       domain.OrderSideBuy,
				OrderType:  domain.OrderTypeLimit,
				Quantity:   1,
				LimitPrice: floatPtr(0.5),
			},
			wantErr: "polymarket: prediction side is required",
		},
		{
			name: "zero quantity",
			order: &domain.Order{
				Ticker:         "btc-100k-2025",
				Side:           domain.OrderSideBuy,
				PredictionSide: "YES",
				OrderType:      domain.OrderTypeLimit,
				Quantity:       0,
				LimitPrice:     floatPtr(0.5),
			},
			wantErr: "polymarket: order quantity must be greater than zero",
		},
		{
			name: "missing limit price",
			order: &domain.Order{
				Ticker:         "btc-100k-2025",
				Side:           domain.OrderSideBuy,
				PredictionSide: "YES",
				OrderType:      domain.OrderTypeLimit,
				Quantity:       1,
			},
			wantErr: "polymarket: limit order requires limit price",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := newTestClient()
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

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)

	broker := NewBroker(client)
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{
		Ticker:         "btc-100k-2025",
		Side:           domain.OrderSideBuy,
		PredictionSide: "YES",
		OrderType:      domain.OrderTypeLimit,
		Quantity:       1,
		LimitPrice:     floatPtr(0.5),
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

func TestBrokerCancelOrder_PostsCancelEndpoint(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodPost)
		}
		requests <- r.RequestURI
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)

	broker := NewBroker(client)
	if err := broker.CancelOrder(context.Background(), "order-1"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}

	select {
	case path := <-requests:
		wantPath := "/v1/order/" + url.PathEscape("order-1") + "/cancel"
		if path != wantPath {
			t.Fatalf("request path = %s, want %s", path, wantPath)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
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
		{name: "pending", orderID: "order-1", apiStatus: "ORDER_STATE_PENDING_NEW", wantStatus: domain.OrderStatusSubmitted},
		{name: "partial", orderID: "order-2", apiStatus: "ORDER_STATE_PARTIALLY_FILLED", wantStatus: domain.OrderStatusPartial},
		{name: "filled", orderID: "order-3", apiStatus: "ORDER_STATE_FILLED", wantStatus: domain.OrderStatusFilled},
		{name: "cancelled", orderID: "order-4", apiStatus: "ORDER_STATE_CANCELED", wantStatus: domain.OrderStatusCancelled},
		{name: "rejected", orderID: "order-5", apiStatus: "ORDER_STATE_REJECTED", wantStatus: domain.OrderStatusRejected},
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
				_, _ = w.Write([]byte(`{"order":{"state":"` + tt.apiStatus + `"}}`))
			}))
			defer server.Close()

			client := newTestClient()
			client.SetAPIBaseURL(server.URL)

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
				wantPath := "/v1/order/" + url.PathEscape(tt.orderID)
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
		_, _ = w.Write([]byte(`[{"slug":"btc-100k-2025","outcome":"YES","size":100,"avgPrice":0.55,"curPrice":0.60},{"slug":"eth-5k-2025","outcome":"NO","size":50,"avgPrice":0.30,"curPrice":0.25}]`))
	}))
	defer server.Close()
	oldDataAPIBaseURL := dataAPIBaseURL
	dataAPIBaseURL = server.URL
	t.Cleanup(func() { dataAPIBaseURL = oldDataAPIBaseURL })

	client := newTestClient()

	broker := NewBroker(client)
	positions, err := broker.GetPositions(context.Background())
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("len(GetPositions()) = %d, want %d", len(positions), 2)
	}

	seen := map[string]domain.Position{}
	for _, position := range positions {
		seen[position.Ticker] = position
	}
	if seen["btc-100k-2025:YES"].Side != domain.PositionSideLong {
		t.Fatalf("btc-100k-2025:YES side = %q, want %q", seen["btc-100k-2025:YES"].Side, domain.PositionSideLong)
	}
	if seen["eth-5k-2025:NO"].Side != domain.PositionSideLong {
		t.Fatalf("eth-5k-2025:NO side = %q, want %q", seen["eth-5k-2025:NO"].Side, domain.PositionSideLong)
	}

	select {
	case path := <-requests:
		if !strings.HasPrefix(path, "/positions?") || !strings.Contains(path, "user=0x0000000000000000000000000000000000000001") {
			t.Fatalf("request path = %s, want data-api positions user query", path)
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
		_, _ = w.Write([]byte(`{"balances":[{"currentBalance":1000.5,"currency":"USD","buyingPower":900.25,"assetNotional":250.75}]}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)

	broker := NewBroker(client)
	balance, err := broker.GetAccountBalance(context.Background())
	if err != nil {
		t.Fatalf("GetAccountBalance() error = %v", err)
	}
	if balance.Currency != "USD" {
		t.Fatalf("GetAccountBalance() Currency = %q, want %q", balance.Currency, "USD")
	}
	if balance.Cash != 1000.5 {
		t.Fatalf("GetAccountBalance() Cash = %v, want %v", balance.Cash, 1000.5)
	}
	if balance.BuyingPower != 900.25 {
		t.Fatalf("GetAccountBalance() BuyingPower = %v, want %v", balance.BuyingPower, 900.25)
	}
	if balance.Equity != 1251.25 {
		t.Fatalf("GetAccountBalance() Equity = %v, want %v", balance.Equity, 1251.25)
	}

	select {
	case path := <-requests:
		if path != "/v1/account/balances" {
			t.Fatalf("request path = %s, want %s", path, "/v1/account/balances")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
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

func TestBrokerSubmitOrder_RespectsBreaker(t *testing.T) {
	br := &fakeBreaker{allowErr: fmt.Errorf("%w: %s", risk.ErrBreakerTripped, "global (halted)")}
	client := newTestClient()
	client.SetAPIBaseURL("http://127.0.0.1:1")
	broker := &Broker{client: client, Breaker: br}
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "btc-100k-2025", Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeLimit, Quantity: 1, LimitPrice: floatPtr(0.5)})
	if !errors.Is(err, risk.ErrBreakerTripped) {
		t.Fatalf("SubmitOrder() err = %v", err)
	}
	if br.allowCalls != 1 {
		t.Fatalf("allowCalls=%d want 1", br.allowCalls)
	}
}

func TestBrokerSubmitOrder_NilBreakerNoEffect(t *testing.T) {
	requests := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests <- struct{}{}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"poly-order-3"}`))
	}))
	defer server.Close()
	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	broker := &Broker{client: client}
	_, err := broker.SubmitOrder(context.Background(), &domain.Order{Ticker: "btc-100k-2025", Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeLimit, Quantity: 1, LimitPrice: floatPtr(0.5)})
	if err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	select {
	case <-requests:
	default:
		t.Fatal("expected HTTP call")
	}
}

func TestBrokerSubmitOrder_StrategyScopeBreaker(t *testing.T) {
	strategyID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	br := &fakeBreaker{}
	client := newTestClient()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"poly-order-1"}`))
	}))
	defer server.Close()
	client.SetAPIBaseURL(server.URL)
	broker := &Broker{client: client, Breaker: br}
	if _, err := broker.SubmitOrder(context.Background(), &domain.Order{StrategyID: &strategyID, Ticker: "btc-100k-2025", Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeLimit, Quantity: 1, LimitPrice: floatPtr(0.5)}); err != nil {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if br.allowCalls != 2 {
		t.Fatalf("allowCalls=%d want 2", br.allowCalls)
	}
}

func floatPtr(value float64) *float64 {
	return &value
}
