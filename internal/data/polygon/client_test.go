package polygon

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestClientGetSuccess(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method string
		path   string
		apiKey string
		ticker string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method: r.Method,
			path:   r.URL.Path,
			apiKey: r.URL.Query().Get("apiKey"),
			ticker: r.URL.Query().Get("ticker"),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK","results":[{"ticker":"AAPL"}]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL
	client.SetTimeout(time.Second)

	body, err := client.Get(context.Background(), "/v3/reference/tickers", url.Values{
		"ticker": []string{"AAPL"},
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got := string(body); got != `{"status":"OK","results":[{"ticker":"AAPL"}]}` {
		t.Fatalf("Get() body = %q, want successful payload", got)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/v3/reference/tickers" {
			t.Fatalf("request path = %s, want %s", request.path, "/v3/reference/tickers")
		}
		if request.apiKey != "test-key" {
			t.Fatalf("apiKey query = %q, want %q", request.apiKey, "test-key")
		}
		if request.ticker != "AAPL" {
			t.Fatalf("ticker query = %q, want %q", request.ticker, "AAPL")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientGetReturnsAuthError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"ERROR","request_id":"req-auth","error":"forbidden"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL

	_, err := client.Get(context.Background(), "/v1/marketstatus/now", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get() error type = %T, want *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusForbidden {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusForbidden)
	}
	if apiErr.RequestID != "req-auth" {
		t.Fatalf("RequestID = %q, want %q", apiErr.RequestID, "req-auth")
	}
	if !strings.Contains(err.Error(), "forbidden") {
		t.Fatalf("Get() error = %q, want auth message", err.Error())
	}
}

func TestClientGetReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"status":"ERROR","request_id":"req-rate","error":"rate limit exceeded"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL

	_, err := client.Get(context.Background(), "/v2/aggs/ticker/AAPL/range/1/day/2024-01-01/2024-01-02", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get() error type = %T, want *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusTooManyRequests)
	}
	if !strings.Contains(err.Error(), "rate limit exceeded") {
		t.Fatalf("Get() error = %q, want rate limit message", err.Error())
	}
}

func TestClientGetReturnsServerError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"status":"ERROR","request_id":"req-500","message":"internal error"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL

	_, err := client.Get(context.Background(), "/v2/reference/news", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get() error type = %T, want *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusInternalServerError {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusInternalServerError)
	}
	if !strings.Contains(err.Error(), "internal error") {
		t.Fatalf("Get() error = %q, want server message", err.Error())
	}
}

func TestClientSetTimeoutIgnoresNonPositiveValues(t *testing.T) {
	t.Parallel()

	client := NewClient("test-key", discardLogger())
	initialTimeout := client.httpClient.Timeout

	client.SetTimeout(0)
	if client.httpClient.Timeout != initialTimeout {
		t.Fatalf("timeout after zero value = %s, want %s", client.httpClient.Timeout, initialTimeout)
	}

	client.SetTimeout(-1 * time.Second)
	if client.httpClient.Timeout != initialTimeout {
		t.Fatalf("timeout after negative value = %s, want %s", client.httpClient.Timeout, initialTimeout)
	}
}
