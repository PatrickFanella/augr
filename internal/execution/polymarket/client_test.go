package polymarket

import (
	"context"
	"encoding/base64"
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

func TestNewClient_SetsBaseURLs(t *testing.T) {
	t.Parallel()

	client := newTestClient()
	if client.apiBaseURL != defaultAPIBaseURL {
		t.Fatalf("apiBaseURL = %q, want %q", client.apiBaseURL, defaultAPIBaseURL)
	}
	if client.gatewayBaseURL != defaultGatewayBaseURL {
		t.Fatalf("gatewayBaseURL = %q, want %q", client.gatewayBaseURL, defaultGatewayBaseURL)
	}
}

func TestClientGet_SendsCLOBL2AuthHeaders(t *testing.T) {
	t.Parallel()

	timestamp := time.UnixMilli(1712000000123)
	type requestDetails struct {
		method     string
		path       string
		apiKey     string
		address    string
		passphrase string
		timestamp  string
		signature  string
		query      url.Values
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method:     r.Method,
			path:       r.URL.Path,
			apiKey:     r.Header.Get("POLY_API_KEY"),
			address:    r.Header.Get("POLY_ADDRESS"),
			passphrase: r.Header.Get("POLY_PASSPHRASE"),
			timestamp:  r.Header.Get("POLY_TIMESTAMP"),
			signature:  r.Header.Get("POLY_SIGNATURE"),
			query:      r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	client.setNowFunc(func() time.Time { return timestamp })

	body, err := client.Get(context.Background(), "/v1/account/balances", url.Values{"market": []string{"btc-100k"}})
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
		if request.path != "/v1/account/balances" {
			t.Fatalf("request path = %s, want %s", request.path, "/v1/account/balances")
		}
		if request.apiKey != "test-key-id" {
			t.Fatalf("POLY_API_KEY = %q, want %q", request.apiKey, "test-key-id")
		}
		if request.address != "0x0000000000000000000000000000000000000001" {
			t.Fatalf("POLY_ADDRESS = %q", request.address)
		}
		if request.passphrase != "test-passphrase" {
			t.Fatalf("POLY_PASSPHRASE = %q", request.passphrase)
		}
		if request.timestamp != "1712000000" {
			t.Fatalf("POLY_TIMESTAMP = %q, want %q", request.timestamp, "1712000000")
		}
		if request.signature == "" {
			t.Fatal("POLY_SIGNATURE = empty, want non-empty signature")
		}
		if request.query.Get("market") != "btc-100k" {
			t.Fatalf("market query = %q, want %q", request.query.Get("market"), "btc-100k")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientGetPublic_OmitsAuthHeaders(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method string
		path   string
		apiKey string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method: r.Method,
			path:   r.URL.Path,
			apiKey: r.Header.Get("POLY_API_KEY"),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetGatewayBaseURL(server.URL)

	body, err := client.GetPublic(context.Background(), "/v1/market/slug/btc-100k", nil)
	if err != nil {
		t.Fatalf("GetPublic() error = %v", err)
	}
	if got := string(body); got != `{"status":"ok"}` {
		t.Fatalf("GetPublic() body = %q, want %q", got, `{"status":"ok"}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/v1/market/slug/btc-100k" {
			t.Fatalf("request path = %s, want %s", request.path, "/v1/market/slug/btc-100k")
		}
		if request.apiKey != "" {
			t.Fatalf("POLY_API_KEY = %q, want empty", request.apiKey)
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

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)

	body, err := client.Post(context.Background(), "/v1/orders", map[string]any{
		"marketSlug": "btc-100k",
		"intent":     "ORDER_INTENT_BUY_LONG",
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
		if request.body["marketSlug"] != "btc-100k" {
			t.Fatalf("marketSlug = %v, want %q", request.body["marketSlug"], "btc-100k")
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

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)

	_, err := client.Get(context.Background(), "/v1/account/balances", nil)
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

	client := NewClient("", "", discardLogger())

	_, err := client.Get(context.Background(), "/v1/account/balances", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
	if err.Error() != "polymarket: address is required" {
		t.Fatalf("Get() error = %v, want address validation", err)
	}
}

func TestPolyL2Signature_OfficialVector(t *testing.T) {
	sig, err := polyL2Signature("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", "1000000", "test-sign", "/orders", []byte(`{"hash": "0x123"}`))
	if err != nil {
		t.Fatalf("polyL2Signature() error = %v", err)
	}
	if sig != "ZwAdJKvoYRlEKDkNMwd5BuwNNtg93kNaR_oU2HrfVvc=" {
		t.Fatalf("signature = %q", sig)
	}
}

func TestClientGet_UsesDefaultHTTPClientWhenUnset(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := &Client{
		address:    "0x0000000000000000000000000000000000000001",
		keyID:      "test-key-id",
		secretKey:  validSecretKeyBase64(),
		passphrase: "test-passphrase",
		apiBaseURL: server.URL,
	}

	body, err := client.Get(context.Background(), "/v1/account/balances", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := string(body); got != `{"status":"ok"}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"status":"ok"}`)
	}
}

func validSecretKeyBase64() string {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return base64.StdEncoding.EncodeToString(seed)
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestClient() *Client {
	client := NewClient("test-key-id", validSecretKeyBase64(), discardLogger())
	client.SetL2Auth("0x0000000000000000000000000000000000000001", "test-key-id", validSecretKeyBase64(), "test-passphrase")
	return client
}
