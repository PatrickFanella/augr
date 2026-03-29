package polymarket

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

func TestNewClient_SetsBaseURL(t *testing.T) {
	t.Parallel()

	client := NewClient("key", "secret", "pass", discardLogger())
	if client.baseURL != clobBaseURL {
		t.Fatalf("baseURL = %q, want %q", client.baseURL, clobBaseURL)
	}
}

func TestClientGet_SendsAuthHeaders(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method     string
		path       string
		address    string
		signature  string
		passphrase string
		query      url.Values
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method:     r.Method,
			path:       r.URL.Path,
			address:    r.Header.Get("POLY-ADDRESS"),
			signature:  r.Header.Get("POLY-SIGNATURE"),
			passphrase: r.Header.Get("POLY-PASSPHRASE"),
			query:      r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	body, err := client.Get(context.Background(), "/balance", url.Values{
		"asset": []string{"USDC"},
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
		if request.path != "/balance" {
			t.Fatalf("request path = %s, want %s", request.path, "/balance")
		}
		if request.address != "test-address" {
			t.Fatalf("POLY-ADDRESS = %q, want %q", request.address, "test-address")
		}
		if request.signature != "test-signature" {
			t.Fatalf("POLY-SIGNATURE = %q, want %q", request.signature, "test-signature")
		}
		if request.passphrase != "test-pass" {
			t.Fatalf("POLY-PASSPHRASE = %q, want %q", request.passphrase, "test-pass")
		}
		if request.query.Get("asset") != "USDC" {
			t.Fatalf("asset query = %q, want %q", request.query.Get("asset"), "USDC")
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
		_, _ = w.Write([]byte(`{"orderID":"order-1"}`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	body, err := client.Post(context.Background(), "/order", map[string]any{
		"tokenID": "token-123",
		"price":   "0.55",
		"size":    "10",
		"side":    "BUY",
	})
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if got := string(body); got != `{"orderID":"order-1"}` {
		t.Fatalf("Post() body = %q, want %q", got, `{"orderID":"order-1"}`)
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
		if request.body["tokenID"] != "token-123" {
			t.Fatalf("tokenID = %v, want %q", request.body["tokenID"], "token-123")
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

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	body, err := client.Delete(context.Background(), "/order", map[string]any{
		"orderID": "order-1",
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
		if request.body["orderID"] != "order-1" {
			t.Fatalf("orderID = %v, want %q", request.body["orderID"], "order-1")
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
		_, _ = w.Write([]byte(`{"error":"invalid credentials"}`))
	}))
	defer server.Close()

	client := NewClient("test-address", "test-signature", "test-pass", discardLogger())
	client.SetBaseURL(server.URL)

	_, err := client.Get(context.Background(), "/balance", nil)
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
	if apiErr.Message != "invalid credentials" {
		t.Fatalf("Message = %q, want %q", apiErr.Message, "invalid credentials")
	}
}

func TestClientGet_RejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	client := NewClient("", "", "", discardLogger())

	_, err := client.Get(context.Background(), "/balance", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
	if err.Error() != "polymarket: api key is required" {
		t.Fatalf("Get() error = %v, want api key validation", err)
	}
}

func TestClientGet_UsesDefaultHTTPClientWhenUnset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := &Client{
		apiKey:    "test-address",
		apiSecret: "test-signature",
		baseURL:   server.URL,
	}

	body, err := client.Get(context.Background(), "/balance", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := string(body); got != `{"status":"ok"}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"status":"ok"}`)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
