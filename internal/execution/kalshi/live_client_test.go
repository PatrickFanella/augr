package kalshi

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
)

func TestHTTPClientCreateOrder_MapsNoSideLimitBuy(t *testing.T) {
	t.Parallel()

	client := &fakeSignedClient{
		postResp: []byte(`{"order":{"order_id":"ord-123"}}`),
	}
	adapter, err := NewLiveHTTPClient(client)
	if err != nil {
		t.Fatalf("NewLiveHTTPClient() error = %v", err)
	}

	noPrice := int64(58)
	resp, err := adapter.CreateOrder(context.Background(), CreateOrderRequest{
		Ticker:        "KX-EXAMPLE",
		Side:          "no",
		Action:        "buy",
		Count:         2,
		Type:          "limit",
		NoPrice:       &noPrice,
		ClientOrderID: "order-123",
	})
	if err != nil {
		t.Fatalf("CreateOrder() error = %v", err)
	}
	if resp.OrderID != "ord-123" {
		t.Fatalf("CreateOrder() order id = %q", resp.OrderID)
	}
	if client.postPath != "/portfolio/events/orders" {
		t.Fatalf("postPath = %q", client.postPath)
	}
	var payload map[string]any
	if err := json.Unmarshal(client.postBody, &payload); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if payload["ticker"] != "KX-EXAMPLE" || payload["side"] != "ask" || payload["count"] != "2.00" || payload["price"] != "0.42" {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestHTTPClientCancelOrder_UsesDelete(t *testing.T) {
	t.Parallel()

	client := &fakeSignedClient{}
	adapter, err := NewLiveHTTPClient(client)
	if err != nil {
		t.Fatalf("NewLiveHTTPClient() error = %v", err)
	}
	if err := adapter.CancelOrder(context.Background(), "ord-123"); err != nil {
		t.Fatalf("CancelOrder() error = %v", err)
	}
	if client.deletePath != "/portfolio/events/orders/ord-123" {
		t.Fatalf("deletePath = %q", client.deletePath)
	}
}

func TestHTTPClientCreateOrderRejectsMarketOrders(t *testing.T) {
	t.Parallel()

	adapter, err := NewLiveHTTPClient(&fakeSignedClient{})
	if err != nil {
		t.Fatalf("NewLiveHTTPClient() error = %v", err)
	}
	_, err = adapter.CreateOrder(context.Background(), CreateOrderRequest{Ticker: "KX-EXAMPLE", Side: "yes", Action: "buy", Count: 1, Type: "market"})
	if err == nil {
		t.Fatal("CreateOrder() error = nil, want market order disabled error")
	}
}

func TestHTTPClientGetOrder_InfersExecutedWhenRemainingZero(t *testing.T) {
	t.Parallel()

	client := &fakeSignedClient{getResp: []byte(`{"order":{"order_id":"ord-123","remaining_count_fp":"0.00"}}`)}
	adapter, err := NewLiveHTTPClient(client)
	if err != nil {
		t.Fatalf("NewLiveHTTPClient() error = %v", err)
	}

	resp, err := adapter.GetOrder(context.Background(), "ord-123")
	if err != nil {
		t.Fatalf("GetOrder() error = %v", err)
	}
	if resp.OrderID != "ord-123" || resp.Status != "executed" {
		t.Fatalf("GetOrder() = %#v, want executed order", resp)
	}
	if client.getPath != "/portfolio/orders/ord-123" {
		t.Fatalf("getPath = %q", client.getPath)
	}
}

func TestHTTPClientListPositions_PaginatesAndMapsPositions(t *testing.T) {
	t.Parallel()

	client := &fakeSignedClient{}
	client.getHandler = func(path string, query map[string]string) ([]byte, error) {
		if path != "/portfolio/positions" {
			return nil, errors.New("unexpected path: " + path)
		}
		if query["limit"] != "1000" || query["count_filter"] != "position" {
			return nil, errors.New("unexpected query")
		}
		if query["cursor"] == "" {
			return []byte(`{"market_positions":[{"ticker":"KX-YES","position_fp":"2.00","market_exposure_dollars":"1.50"}],"next_cursor":"page2"}`), nil
		}
		if query["cursor"] != "page2" {
			return nil, errors.New("unexpected cursor")
		}
		return []byte(`{"market_positions":[{"ticker":"KX-NO","position_fp":"-1.00","market_exposure_dollars":"0.75"}]}`), nil
	}
	adapter, err := NewLiveHTTPClient(client)
	if err != nil {
		t.Fatalf("NewLiveHTTPClient() error = %v", err)
	}

	positions, err := adapter.ListPositions(context.Background())
	if err != nil {
		t.Fatalf("ListPositions() error = %v", err)
	}
	if len(positions) != 2 {
		t.Fatalf("ListPositions() len = %d, want 2", len(positions))
	}
	if positions[0].Ticker != "KX-YES" || positions[0].Side != "yes" || positions[0].Count != 2 || positions[0].ValueCents != 150 {
		t.Fatalf("positions[0] = %#v", positions[0])
	}
	if positions[1].Ticker != "KX-NO" || positions[1].Side != "no" || positions[1].Count != 1 || positions[1].ValueCents != 75 {
		t.Fatalf("positions[1] = %#v", positions[1])
	}
	if len(client.getQueries) != 2 || client.getQueries[0]["cursor"] != "" || client.getQueries[1]["cursor"] != "page2" {
		t.Fatalf("getQueries = %#v", client.getQueries)
	}
}

func TestHTTPClientGetBalance_MapsCents(t *testing.T) {
	t.Parallel()

	client := &fakeSignedClient{getResp: []byte(`{"balance":12345,"portfolio_value":67890}`)}
	adapter, err := NewLiveHTTPClient(client)
	if err != nil {
		t.Fatalf("NewLiveHTTPClient() error = %v", err)
	}

	resp, err := adapter.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("GetBalance() error = %v", err)
	}
	if resp.CashCents != 12345 || resp.BuyingPowerCents != 12345 || resp.EquityCents != 67890 {
		t.Fatalf("GetBalance() = %#v", resp)
	}
	if client.getPath != "/portfolio/balance" {
		t.Fatalf("getPath = %q", client.getPath)
	}
}

type fakeSignedClient struct {
	postPath    string
	postBody    []byte
	postResp    []byte
	deletePath  string
	deleteQuery map[string]string
	getPath     string
	getQueries  []map[string]string
	getResp     []byte
	getHandler  func(path string, query map[string]string) ([]byte, error)
}

func (f *fakeSignedClient) Post(_ context.Context, path string, body any) ([]byte, error) {
	f.postPath = path
	if payload, ok := body.(map[string]any); ok {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		f.postBody = encoded
	} else {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		f.postBody = encoded
	}
	return f.postResp, nil
}

func (f *fakeSignedClient) Delete(_ context.Context, path string, query url.Values) ([]byte, error) {
	f.deletePath = path
	f.deleteQuery = map[string]string{}
	for key := range query {
		f.deleteQuery[key] = query.Get(key)
	}
	return []byte(`{}`), nil
}

func (f *fakeSignedClient) Get(_ context.Context, path string, query url.Values, _ bool) ([]byte, error) {
	f.getPath = path
	q := map[string]string{}
	for key := range query {
		q[key] = query.Get(key)
	}
	f.getQueries = append(f.getQueries, q)
	if f.getHandler != nil {
		return f.getHandler(path, q)
	}
	return f.getResp, nil
}
