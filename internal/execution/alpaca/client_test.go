package alpaca

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestNewClient_SelectsBaseURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		isPaper  bool
		expected string
	}{
		{
			name:     "paper trading",
			isPaper:  true,
			expected: paperBaseURL,
		},
		{
			name:     "live trading",
			isPaper:  false,
			expected: liveBaseURL,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := NewClient("key", "secret", tt.isPaper, discardLogger())
			if client.baseURL != tt.expected {
				t.Fatalf("baseURL = %q, want %q", client.baseURL, tt.expected)
			}
		})
	}
}

func TestClientGet_SendsAuthHeaders(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method    string
		path      string
		keyID     string
		secretKey string
		query     url.Values
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method:    r.Method,
			path:      r.URL.Path,
			keyID:     r.Header.Get("APCA-API-KEY-ID"),
			secretKey: r.Header.Get("APCA-API-SECRET-KEY"),
			query:     r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	body, err := client.Get(context.Background(), "/v2/account", url.Values{
		"status": []string{"active"},
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := string(body); got != `{"status":"ok"}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"status":"ok"}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/v2/account" {
			t.Fatalf("request path = %s, want %s", request.path, "/v2/account")
		}
		if request.keyID != "test-key" {
			t.Fatalf("APCA-API-KEY-ID = %q, want %q", request.keyID, "test-key")
		}
		if request.secretKey != "test-secret" {
			t.Fatalf("APCA-API-SECRET-KEY = %q, want %q", request.secretKey, "test-secret")
		}
		if request.query.Get("status") != "active" {
			t.Fatalf("status query = %q, want %q", request.query.Get("status"), "active")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientPost_SendsJSONBody(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method      string
		contentType string
		body        map[string]any
		decodeErr   error
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			requests <- requestDetails{decodeErr: err}
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		requests <- requestDetails{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			body:        payload,
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"order-1"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	body, err := client.Post(context.Background(), "/v2/orders", map[string]any{
		"symbol": "AAPL",
		"qty":    1,
		"side":   "buy",
	})
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if got := string(body); got != `{"id":"order-1"}` {
		t.Fatalf("Post() body = %q, want %q", got, `{"id":"order-1"}`)
	}

	select {
	case request := <-requests:
		if request.decodeErr != nil {
			t.Fatalf("Decode() error = %v", request.decodeErr)
		}
		if request.method != http.MethodPost {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodPost)
		}
		if request.contentType != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", request.contentType, "application/json")
		}
		if request.body["symbol"] != "AAPL" {
			t.Fatalf("symbol = %v, want %q", request.body["symbol"], "AAPL")
		}
		if request.body["side"] != "buy" {
			t.Fatalf("side = %v, want %q", request.body["side"], "buy")
		}
		if request.body["qty"] != float64(1) {
			t.Fatalf("qty = %v, want %v", request.body["qty"], float64(1))
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientDelete_SendsJSONBody(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method    string
		body      map[string]any
		decodeErr error
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := make(map[string]any)
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			requests <- requestDetails{decodeErr: err}
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		requests <- requestDetails{
			method: r.Method,
			body:   payload,
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	body, err := client.Delete(context.Background(), "/v2/orders/order-1", map[string]any{
		"cancel_orders": true,
	})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if len(body) != 0 {
		t.Fatalf("Delete() body length = %d, want 0", len(body))
	}

	select {
	case request := <-requests:
		if request.decodeErr != nil {
			t.Fatalf("Decode() error = %v", request.decodeErr)
		}
		if request.method != http.MethodDelete {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodDelete)
		}
		if request.body["cancel_orders"] != true {
			t.Fatalf("cancel_orders = %v, want true", request.body["cancel_orders"])
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientGet_ParsesErrorResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"code":40110000,"message":"invalid credentials"}`))
	}))
	defer server.Close()

	client := NewClient("test-key", "test-secret", true, discardLogger())
	client.SetBaseURL(server.URL)

	_, err := client.Get(context.Background(), "/v2/account", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get() error type = %T, want *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusUnauthorized)
	}
	if apiErr.Code != 40110000 {
		t.Fatalf("Code = %d, want %d", apiErr.Code, 40110000)
	}
	if apiErr.Message != "invalid credentials" {
		t.Fatalf("Message = %q, want %q", apiErr.Message, "invalid credentials")
	}
}

func TestClientGet_TreatsRedirectAsError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", "/v2/account")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	client := &Client{
		apiKey:    "test-key",
		apiSecret: "test-secret",
		baseURL:   server.URL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		logger: discardLogger(),
	}

	_, err := client.Get(context.Background(), "/v2/account", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get() error type = %T, want *ErrorResponse", err)
	}
	if apiErr.StatusCode() != http.StatusFound {
		t.Fatalf("StatusCode() = %d, want %d", apiErr.StatusCode(), http.StatusFound)
	}
}

func TestClientGet_UsesDefaultHTTPClientWhenUnset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := &Client{
		apiKey:    "test-key",
		apiSecret: "test-secret",
		baseURL:   server.URL,
	}

	body, err := client.Get(context.Background(), "/v2/account", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := string(body); got != `{"status":"ok"}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"status":"ok"}`)
	}
}

func TestClientDelete_RejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	client := NewClient("", "", true, discardLogger())

	_, err := client.Delete(context.Background(), "/v2/orders/order-1", nil)
	if err == nil {
		t.Fatal("Delete() error = nil, want non-nil")
	}
	if err.Error() != "alpaca: api key is required" {
		t.Fatalf("Delete() error = %v, want api key validation", err)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
