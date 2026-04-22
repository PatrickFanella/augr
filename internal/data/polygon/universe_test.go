package polygon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestListActiveTickersRespectsFreeTierRateLimit(t *testing.T) {
	var firstRequestAt time.Time
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("cursor") {
		case "":
			firstRequestAt = time.Now()
			_, _ = fmt.Fprintf(w, `{"results":[{"ticker":"AAA","name":"Alpha","primary_exchange":"XNAS","type":"CS","active":true}],"next_url":"%s/v3/reference/tickers?cursor=page-2"}`,
				serverURL,
			)
		case "page-2":
			if time.Since(firstRequestAt) < 11*time.Second {
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"status":"ERROR","request_id":"req-rate","error":"rate limit exceeded"}`))
				return
			}
			_, _ = w.Write([]byte(`{"results":[{"ticker":"BBB","name":"Beta","primary_exchange":"XNYS","type":"CS","active":true}]}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL

	tickers, err := client.ListActiveTickers(context.Background(), "stocks", "CS")
	if err != nil {
		t.Fatalf("ListActiveTickers() error = %v", err)
	}
	if len(tickers) != 2 {
		t.Fatalf("ListActiveTickers() count = %d, want 2", len(tickers))
	}
	if tickers[0].Ticker != "AAA" || tickers[1].Ticker != "BBB" {
		t.Fatalf("ListActiveTickers() tickers = %#v, want AAA then BBB", tickers)
	}
}
