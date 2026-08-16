package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/PatrickFanella/get-rich-quick/internal/execution/lifecycle"
	"github.com/PatrickFanella/get-rich-quick/internal/execution/venue"
	"github.com/PatrickFanella/get-rich-quick/internal/instrument"
)

func TestMapCommonOrderRequestProjectsEveryReviewedCapabilityExactly(t *testing.T) {
	policy, err := venue.ReviewedPolicy(venue.ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	for _, capability := range policy.Capabilities() {
		capability := capability
		name := strings.Join([]string{
			string(capability.AssetClass), string(capability.OrderType), string(capability.TimeInForce),
		}, "/")
		t.Run(name, func(t *testing.T) {
			order, primary, contract := commonLifecycleOrderFixture(capability)
			request, mapErr := MapCommonOrderRequest(policy, primary, contract, order)
			if mapErr != nil {
				t.Fatalf("MapCommonOrderRequest() error = %v", mapErr)
			}
			if request.ClientOrderID != order.ClientOrderID || request.Symbol != contract.ContractID ||
				request.Quantity != order.Quantity.String() || request.Side != string(order.Side) ||
				request.Type != string(order.OrderType) || request.TimeInForce != string(order.TimeInForce) {
				t.Fatalf("request projection = %#v", request)
			}
			if request.Quantity != "1.23456789" {
				t.Fatalf("quantity = %q, want exact decimal", request.Quantity)
			}
			switch order.OrderType {
			case lifecycle.OrderMarket:
				if request.LimitPrice != "" || request.StopPrice != "" {
					t.Fatalf("market request gained a price: %#v", request)
				}
			case lifecycle.OrderLimit:
				if request.LimitPrice != "123.45" || request.StopPrice != "" {
					t.Fatalf("limit request prices = %#v", request)
				}
			case lifecycle.OrderStop:
				if request.LimitPrice != "" || request.StopPrice != "120.05" {
					t.Fatalf("stop request prices = %#v", request)
				}
			case lifecycle.OrderStopLimit:
				if request.LimitPrice != "123.45" || request.StopPrice != "120.05" {
					t.Fatalf("stop-limit request prices = %#v", request)
				}
			}

			encoded, marshalErr := json.Marshal(request)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for _, forbidden := range []string{
				"extended_hours", "notional", "trail_price", "trail_percent", "order_class", "legs",
			} {
				if strings.Contains(string(encoded), forbidden) {
					t.Fatalf("request contains forbidden mechanic %q: %s", forbidden, encoded)
				}
			}
		})
	}
}

func TestMapCommonOrderRequestRejectsUnsupportedOrContradictoryInputs(t *testing.T) {
	policy, err := venue.ReviewedPolicy(venue.ProviderAlpaca)
	if err != nil {
		t.Fatal(err)
	}
	capability := venue.Capability{
		AssetClass:  instrument.AssetClassEquity,
		OrderType:   lifecycle.OrderLimit,
		TimeInForce: lifecycle.TimeInForceDay,
	}
	order, primary, contract := commonLifecycleOrderFixture(capability)

	for name, mutate := range map[string]func(*lifecycle.Order, *instrument.Instrument, *instrument.VenueContract){
		"unsupported asset": func(_ *lifecycle.Order, primary *instrument.Instrument, _ *instrument.VenueContract) {
			primary.AssetClass = instrument.AssetClassOption
		},
		"unsupported tif": func(order *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract) {
			order.TimeInForce = lifecycle.TimeInForceGTD
		},
		"market to limit": func(order *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract) {
			order.OrderType = lifecycle.OrderMarket
		},
		"symbol mismatch": func(_ *lifecycle.Order, _ *instrument.Instrument, contract *instrument.VenueContract) {
			contract.InstrumentID = uuid.New()
		},
		"venue mismatch": func(order *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract) {
			order.Venue = "elsewhere"
		},
		"client id mismatch": func(order *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract) {
			order.ClientOrderID = uuid.NewString()
		},
		"off lot": func(order *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract) {
			order.Quantity = decimal.RequireFromString("1.234567891")
		},
		"off tick": func(order *lifecycle.Order, _ *instrument.Instrument, _ *instrument.VenueContract) {
			price := decimal.RequireFromString("123.456")
			order.LimitPrice = &price
		},
	} {
		t.Run(name, func(t *testing.T) {
			candidateOrder := *order
			candidateInstrument := *primary
			candidateContract := *contract
			mutate(&candidateOrder, &candidateInstrument, &candidateContract)
			if _, err := MapCommonOrderRequest(policy, &candidateInstrument, &candidateContract, &candidateOrder); err == nil {
				t.Fatal("unsupported request unexpectedly mapped")
			}
		})
	}
}

func TestCommonLifecycleClientUsesExactEndpointsAndPreservesEvidence(t *testing.T) {
	type requestFact struct {
		method string
		path   string
		query  url.Values
		body   string
	}
	var (
		mu       sync.Mutex
		requests []requestFact
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := readTestRequestBody(t, r)
		mu.Lock()
		requests = append(requests, requestFact{r.Method, r.URL.Path, r.URL.Query(), body})
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v2/orders":
			_, _ = w.Write([]byte(`{"id":"alpaca-1","client_order_id":"client-1","symbol":"AAPL","side":"buy","type":"limit","time_in_force":"day","qty":"1","filled_qty":"0","status":"new","updated_at":"2026-08-15T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/orders:by_client_order_id":
			_, _ = w.Write([]byte(`{"id":"alpaca-1","client_order_id":"client-1","symbol":"AAPL","side":"buy","type":"limit","time_in_force":"day","qty":"1","filled_qty":"0","status":"new","updated_at":"2026-08-15T12:00:00Z"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v2/orders/alpaca-1":
			_, _ = w.Write([]byte(`{"id":"alpaca-1","client_order_id":"client-1","symbol":"AAPL","side":"buy","type":"limit","time_in_force":"day","qty":"1","filled_qty":"0","status":"new","updated_at":"2026-08-15T12:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v2/orders/alpaca-1":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	transport := newLoopbackCommonLifecycleClient(t, server.URL)
	request := CommonOrderRequest{
		Symbol: "AAPL", Quantity: "1", Side: "buy", Type: "limit", TimeInForce: "day",
		ClientOrderID: "client-1", LimitPrice: "123.45",
	}
	submitted, err := transport.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if submitted.Order.ID != "alpaca-1" || string(submitted.RawPayload) == "" {
		t.Fatalf("submit result = %#v", submitted)
	}
	byClient, err := transport.GetByClientOrderID(context.Background(), "client-1")
	if err != nil || byClient.Order.ClientOrderID != "client-1" {
		t.Fatalf("client lookup = %#v, %v", byClient, err)
	}
	byExternal, err := transport.GetByExternalOrderID(context.Background(), "alpaca-1")
	if err != nil || byExternal.Order.ID != "alpaca-1" {
		t.Fatalf("external lookup = %#v, %v", byExternal, err)
	}
	if err := transport.Cancel(context.Background(), "alpaca-1"); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(requests) != 4 {
		t.Fatalf("requests = %#v", requests)
	}
	if requests[0].body != `{"symbol":"AAPL","qty":"1","side":"buy","type":"limit","time_in_force":"day","client_order_id":"client-1","limit_price":"123.45"}` {
		t.Fatalf("submit body = %s", requests[0].body)
	}
	if requests[1].query.Get("client_order_id") != "client-1" || requests[2].path != "/v2/orders/alpaca-1" ||
		requests[3].method != http.MethodDelete {
		t.Fatalf("endpoint requests = %#v", requests)
	}
}

func TestCommonLifecycleClientPaginatesOrderFilteredFillsAscending(t *testing.T) {
	var tokens []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/account/activities/FILL" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("order_id") != "alpaca-1" || r.URL.Query().Get("direction") != "asc" ||
			r.URL.Query().Get("page_size") != "2" {
			t.Errorf("fill query = %s", r.URL.RawQuery)
		}
		token := r.URL.Query().Get("page_token")
		tokens = append(tokens, token)
		w.Header().Set("Content-Type", "application/json")
		if token == "" {
			_, _ = w.Write([]byte(`[{"id":"fill-1","activity_type":"FILL","order_id":"alpaca-1","qty":"0.4","price":"100.01","side":"buy","symbol":"AAPL","transaction_time":"2026-08-15T12:00:01Z"},{"id":"fill-2","activity_type":"FILL","order_id":"alpaca-1","qty":"0.3","price":"100.02","side":"buy","symbol":"AAPL","transaction_time":"2026-08-15T12:00:02Z"}]`))
			return
		}
		_, _ = w.Write([]byte(`[{"id":"fill-3","activity_type":"FILL","order_id":"alpaca-1","qty":"0.3","price":"100.03","side":"buy","symbol":"AAPL","transaction_time":"2026-08-15T12:00:03Z"}]`))
	}))
	defer server.Close()

	transport := newLoopbackCommonLifecycleClient(t, server.URL)
	activities, err := transport.ListFillActivities(context.Background(), "alpaca-1", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(activities) != 3 || activities[0].Activity.ID != "fill-1" || activities[2].Activity.ID != "fill-3" {
		t.Fatalf("activities = %#v", activities)
	}
	if fmt.Sprint(tokens) != "[ fill-2]" {
		t.Fatalf("page tokens = %v, want empty then fill-2", tokens)
	}
}

func TestCommonLifecycleClientFailsClosed(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"code":404,"message":"missing"}`, http.StatusNotFound)
		}))
		defer server.Close()
		_, err := newLoopbackCommonLifecycleClient(t, server.URL).GetByExternalOrderID(context.Background(), "missing")
		if !errors.Is(err, ErrOrderNotFound) {
			t.Fatalf("error = %v, want ErrOrderNotFound", err)
		}
	})

	t.Run("duplicate submit", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":409,"message":"client_order_id already exists"}`))
		}))
		defer server.Close()
		_, err := newLoopbackCommonLifecycleClient(t, server.URL).Submit(context.Background(), CommonOrderRequest{
			Symbol: "AAPL", Quantity: "1", Side: "buy", Type: "market", TimeInForce: "day", ClientOrderID: "client-1",
		})
		if !errors.Is(err, ErrDuplicateOrder) {
			t.Fatalf("error = %v, want ErrDuplicateOrder", err)
		}
	})

	t.Run("malformed json", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"id":`))
		}))
		defer server.Close()
		if _, err := newLoopbackCommonLifecycleClient(t, server.URL).GetByExternalOrderID(context.Background(), "alpaca-1"); err == nil {
			t.Fatal("malformed response unexpectedly accepted")
		}
	})

	t.Run("repeated page cursor", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"id":"same","activity_type":"FILL","order_id":"alpaca-1","qty":"1","price":"1","side":"buy","symbol":"AAPL","transaction_time":"2026-08-15T12:00:01Z"}]`))
		}))
		defer server.Close()
		if _, err := newLoopbackCommonLifecycleClient(t, server.URL).ListFillActivities(context.Background(), "alpaca-1", 1); err == nil {
			t.Fatal("repeated cursor unexpectedly accepted")
		}
	})

	t.Run("context cancellation", func(t *testing.T) {
		started := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			close(started)
			<-r.Context().Done()
		}))
		defer server.Close()
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() {
			_, err := newLoopbackCommonLifecycleClient(t, server.URL).GetByExternalOrderID(ctx, "alpaca-1")
			done <- err
		}()
		<-started
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
}

func TestCommonLifecycleClientRecoversAmbiguousOrDuplicateSubmitByExactClientID(t *testing.T) {
	for _, submitStatus := range []int{http.StatusConflict, http.StatusInternalServerError} {
		t.Run(http.StatusText(submitStatus), func(t *testing.T) {
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls = append(calls, r.Method+" "+r.URL.RequestURI())
				if r.Method == http.MethodPost {
					w.WriteHeader(submitStatus)
					_, _ = w.Write([]byte(`{"code":409,"message":"client_order_id already exists"}`))
					return
				}
				_, _ = w.Write([]byte(`{"id":"alpaca-1","client_order_id":"client-1","symbol":"AAPL","side":"buy","type":"market","time_in_force":"day","qty":"1","filled_qty":"0","status":"new","updated_at":"2026-08-15T12:00:00Z"}`))
			}))
			defer server.Close()

			result, err := newLoopbackCommonLifecycleClient(t, server.URL).SubmitOrLookup(
				context.Background(), CommonOrderRequest{
					Symbol: "AAPL", Quantity: "1", Side: "buy", Type: "market",
					TimeInForce: "day", ClientOrderID: "client-1",
				},
			)
			if err != nil || result.Order.ID != "alpaca-1" {
				t.Fatalf("SubmitOrLookup() = %#v, %v", result, err)
			}
			if len(calls) != 2 || calls[1] != "GET /v2/orders:by_client_order_id?client_order_id=client-1" {
				t.Fatalf("recovery calls = %v", calls)
			}
		})
	}
}

func commonLifecycleOrderFixture(capability venue.Capability) (*lifecycle.Order, *instrument.Instrument, *instrument.VenueContract) {
	now := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	policy, err := venue.ReviewedPolicy(venue.ProviderAlpaca)
	if err != nil {
		panic(err)
	}
	instrumentID := uuid.New()
	orderID := uuid.New()
	lotSize := decimal.RequireFromString("0.00000001")
	primary := &instrument.Instrument{
		ID: instrumentID, IdentityKey: "test:" + uuid.NewString(), AssetClass: capability.AssetClass,
		PrimaryVenue: "alpaca", Currency: "USD", TickSize: decimal.RequireFromString("0.01"),
		LotSize: lotSize, Multiplier: decimal.NewFromInt(1), SettlementMethod: instrument.SettlementPhysical,
		Status: instrument.StatusActive, Metadata: json.RawMessage(`{}`), CreatedAt: now,
	}
	if capability.AssetClass == instrument.AssetClassCryptoSpot {
		primary.SettlementMethod = instrument.SettlementCrypto
	}
	contract := &instrument.VenueContract{
		ID: uuid.New(), InstrumentID: instrumentID, Venue: "alpaca", ContractID: "AAPL",
		Currency: "USD", TickSize: decimal.RequireFromString("0.01"), LotSize: lotSize,
		Multiplier: decimal.NewFromInt(1), SettlementMethod: primary.SettlementMethod,
		ValidFrom: now.Add(-time.Hour), Metadata: json.RawMessage(`{}`), CreatedAt: now,
	}
	order := &lifecycle.Order{
		ID: orderID, IntentID: uuid.New(), AccountID: uuid.New(), InstrumentID: instrumentID,
		ClientOrderID: orderID.String(), Side: lifecycle.SideBuy, OrderType: capability.OrderType,
		TimeInForce: capability.TimeInForce, Quantity: decimal.RequireFromString("1.23456789"),
		Venue: "alpaca", VenueContractID: contract.ID, PolicyKind: lifecycle.PolicyVenue,
		PolicyVersion: policy.Version(),
	}
	limitPrice := decimal.RequireFromString("123.45")
	stopPrice := decimal.RequireFromString("120.05")
	switch capability.OrderType {
	case lifecycle.OrderLimit:
		order.LimitPrice = &limitPrice
	case lifecycle.OrderStop:
		order.StopPrice = &stopPrice
	case lifecycle.OrderStopLimit:
		order.LimitPrice = &limitPrice
		order.StopPrice = &stopPrice
	}
	return order, primary, contract
}

func newLoopbackCommonLifecycleClient(t *testing.T, baseURL string) *CommonLifecycleClient {
	t.Helper()
	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(baseURL)
	transport, err := NewCommonLifecycleClient(client)
	if err != nil {
		t.Fatal(err)
	}
	return transport
}

func readTestRequestBody(t *testing.T, request *http.Request) string {
	t.Helper()
	defer func() {
		if err := request.Body.Close(); err != nil {
			t.Errorf("close request body: %v", err)
		}
	}()
	var body json.RawMessage
	if request.Body == http.NoBody {
		return ""
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return ""
	}
	return string(body)
}
