package automation

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	alpacaexec "github.com/PatrickFanella/get-rich-quick/internal/execution/alpaca"
)

func TestAlpacaClientAdapterListOrders_MapsBrokerOrders(t *testing.T) {
	t.Parallel()

	requests := make(chan *url.URL, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/orders" {
			t.Fatalf("request path = %s, want %s", r.URL.Path, "/v2/orders")
		}
		requests <- r.URL
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{
				"id": "order-1",
				"symbol": "SNAL",
				"side": "buy",
				"type": "limit",
				"qty": "200",
				"limit_price": "0.92",
				"filled_qty": "186",
				"filled_avg_price": "0.92",
				"status": "partially_filled",
				"submitted_at": "2026-04-15T19:20:02.451351Z",
				"filled_at": "2026-04-15T19:20:04.943982Z"
			}
		]`))
	}))
	defer server.Close()

	client := alpacaexec.NewClient("test-key", "test-secret", true, slog.New(slog.NewTextHandler(testDiscardWriter{}, nil)))
	client.SetBaseURL(server.URL)

	adapter := NewAlpacaClientAdapter(client)
	orders, err := adapter.ListOrders(context.Background())
	if err != nil {
		t.Fatalf("ListOrders() error = %v", err)
	}
	if len(orders) != 1 {
		t.Fatalf("len(ListOrders()) = %d, want 1", len(orders))
	}
	if orders[0].ExternalID != "order-1" {
		t.Fatalf("ExternalID = %q, want order-1", orders[0].ExternalID)
	}
	if orders[0].Ticker != "SNAL" {
		t.Fatalf("Ticker = %q, want SNAL", orders[0].Ticker)
	}
	if orders[0].Side != domain.OrderSideBuy {
		t.Fatalf("Side = %q, want buy", orders[0].Side)
	}
	if orders[0].OrderType != domain.OrderTypeLimit {
		t.Fatalf("OrderType = %q, want limit", orders[0].OrderType)
	}
	if orders[0].Status != domain.OrderStatusPartial {
		t.Fatalf("Status = %q, want partial", orders[0].Status)
	}
	if orders[0].FilledQuantity != 186 {
		t.Fatalf("FilledQuantity = %v, want 186", orders[0].FilledQuantity)
	}
	if orders[0].FilledAvgPrice == nil || *orders[0].FilledAvgPrice != 0.92 {
		t.Fatalf("FilledAvgPrice = %v, want 0.92", orders[0].FilledAvgPrice)
	}
	if orders[0].SubmittedAt == nil || !orders[0].SubmittedAt.Equal(time.Date(2026, 4, 15, 19, 20, 2, 451351000, time.UTC)) {
		t.Fatalf("SubmittedAt = %v, want expected timestamp", orders[0].SubmittedAt)
	}
	if orders[0].FilledAt == nil || !orders[0].FilledAt.Equal(time.Date(2026, 4, 15, 19, 20, 4, 943982000, time.UTC)) {
		t.Fatalf("FilledAt = %v, want expected timestamp", orders[0].FilledAt)
	}

	select {
	case reqURL := <-requests:
		if got := reqURL.Query().Get("status"); got != "all" {
			t.Fatalf("status query = %q, want all", got)
		}
		if got := reqURL.Query().Get("limit"); got != "500" {
			t.Fatalf("limit query = %q, want 500", got)
		}
		if got := reqURL.Query().Get("direction"); got != "desc" {
			t.Fatalf("direction query = %q, want desc", got)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestAlpacaClientAdapterListFills_PaginatesActivities(t *testing.T) {
	t.Parallel()

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", r.Method, http.MethodGet)
		}
		if r.URL.Path != "/v2/account/activities/FILL" {
			t.Fatalf("request path = %s, want %s", r.URL.Path, "/v2/account/activities/FILL")
		}
		requestCount++
		w.Header().Set("Content-Type", "application/json")
		switch requestCount {
		case 1:
			if got := r.URL.Query().Get("page_token"); got != "" {
				t.Fatalf("first page_token = %q, want empty", got)
			}
			for i := 0; i < alpacaActivitiesPageSize; i++ {
				activityID := "fill-1"
				qty := "186"
				transactionTime := "2026-04-15T19:20:02.66268Z"
				orderStatus := "partially_filled"
				if i == 0 {
					activityID = "fill-2"
					qty = "14"
					transactionTime = "2026-04-15T19:20:04.943982Z"
					orderStatus = "filled"
				}
				if i > 1 {
					activityID = activityID + "-extra"
				}
				if i == 0 {
					_, _ = w.Write([]byte("["))
				} else {
					_, _ = w.Write([]byte(","))
				}
				_, _ = w.Write([]byte(`{
					"activity_type": "FILL",
					"id": "` + activityID + `",
					"order_id": "order-1",
					"symbol": "SNAL",
					"side": "buy",
					"qty": "` + qty + `",
					"price": "0.92",
					"transaction_time": "` + transactionTime + `",
					"order_status": "` + orderStatus + `"
				}`))
			}
			_, _ = w.Write([]byte("]"))
		case 2:
			if got := r.URL.Query().Get("page_token"); got != "fill-1-extra" {
				t.Fatalf("second page_token = %q, want fill-1-extra", got)
			}
			_, _ = w.Write([]byte(`[
				{
					"activity_type": "FILL",
					"id": "fill-3",
					"order_id": "order-1",
					"symbol": "SNAL",
					"side": "buy",
					"qty": "1",
					"price": "0.93",
					"transaction_time": "2026-04-15T19:21:04.943982Z",
					"order_status": "filled"
				}
			]`))
		default:
			t.Fatalf("unexpected request count %d", requestCount)
		}
	}))
	defer server.Close()

	client := alpacaexec.NewClient("test-key", "test-secret", true, slog.New(slog.NewTextHandler(testDiscardWriter{}, nil)))
	client.SetBaseURL(server.URL)

	adapter := NewAlpacaClientAdapter(client)
	fills, err := adapter.ListFills(context.Background())
	if err != nil {
		t.Fatalf("ListFills() error = %v", err)
	}
	if requestCount != 2 {
		t.Fatalf("requestCount = %d, want 2", requestCount)
	}
	if len(fills) != alpacaActivitiesPageSize+1 {
		t.Fatalf("len(ListFills()) = %d, want %d", len(fills), alpacaActivitiesPageSize+1)
	}
	if fills[0].ActivityID != "fill-2" {
		t.Fatalf("fills[0].ActivityID = %q, want fill-2", fills[0].ActivityID)
	}
	if fills[0].OrderStatus != domain.OrderStatusFilled {
		t.Fatalf("fills[0].OrderStatus = %q, want filled", fills[0].OrderStatus)
	}
	if fills[1].ActivityID != "fill-1" {
		t.Fatalf("fills[1].ActivityID = %q, want fill-1", fills[1].ActivityID)
	}
	if fills[1].OrderStatus != domain.OrderStatusPartial {
		t.Fatalf("fills[1].OrderStatus = %q, want partial", fills[1].OrderStatus)
	}
	if fills[len(fills)-1].ActivityID != "fill-3" {
		t.Fatalf("last fill ActivityID = %q, want fill-3", fills[len(fills)-1].ActivityID)
	}
}

type testDiscardWriter struct{}

func (testDiscardWriter) Write(p []byte) (int, error) { return len(p), nil }
