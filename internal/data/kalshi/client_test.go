package kalshi

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNewClient_DefaultBaseURL(t *testing.T) {
	t.Parallel()

	client, err := NewClient("", "", "", discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.baseURL != defaultBaseURL {
		t.Fatalf("baseURL = %q, want %q", client.baseURL, defaultBaseURL)
	}
}

func TestClientPublicGet_OmitsAuthHeaders(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method    string
		path      string
		query     url.Values
		body      string
		accessID  string
		timestamp string
		signature string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- requestDetails{
			method:    r.Method,
			path:      r.URL.Path,
			query:     r.URL.Query(),
			body:      string(body),
			accessID:  r.Header.Get("KALSHI-ACCESS-KEY"),
			timestamp: r.Header.Get("KALSHI-ACCESS-TIMESTAMP"),
			signature: r.Header.Get("KALSHI-ACCESS-SIGNATURE"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/trade-api/v2", "", "", discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())

	body, err := client.Get(context.Background(), "/markets", url.Values{"limit": []string{"10"}}, false)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := string(body); got != `{"ok":true}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"ok":true}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/trade-api/v2/markets" {
			t.Fatalf("path = %s, want %s", request.path, "/trade-api/v2/markets")
		}
		if request.query.Get("limit") != "10" {
			t.Fatalf("limit query = %q, want %q", request.query.Get("limit"), "10")
		}
		if request.body != "" {
			t.Fatalf("GET body = %q, want empty", request.body)
		}
		if request.accessID != "" || request.timestamp != "" || request.signature != "" {
			t.Fatalf("auth headers present on public request: %+v", request)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientAuthenticatedGet_SendsHeadersAndExcludesQueryFromSignature(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, privateKey := testPrivateKeyPEMB64(t)
	timestamp := time.UnixMilli(1712000000123)
	requestPath := "/markets"
	signedPath := "/trade-api/v2/markets"

	type requestDetails struct {
		method    string
		path      string
		query     url.Values
		body      string
		accessID  string
		timestamp string
		signature string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		requests <- requestDetails{
			method:    r.Method,
			path:      r.URL.Path,
			query:     r.URL.Query(),
			body:      string(body),
			accessID:  r.Header.Get("KALSHI-ACCESS-KEY"),
			timestamp: r.Header.Get("KALSHI-ACCESS-TIMESTAMP"),
			signature: r.Header.Get("KALSHI-ACCESS-SIGNATURE"),
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())
	client.setNowFunc(func() time.Time { return timestamp })

	body, err := client.Get(context.Background(), requestPath, url.Values{"limit": []string{"10"}}, true)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := string(body); got != `{"ok":true}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"ok":true}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != signedPath {
			t.Fatalf("path = %s, want %s", request.path, signedPath)
		}
		if request.accessID != "test-key-id" {
			t.Fatalf("KALSHI-ACCESS-KEY = %q, want %q", request.accessID, "test-key-id")
		}
		if request.timestamp != "1712000000123" {
			t.Fatalf("KALSHI-ACCESS-TIMESTAMP = %q, want %q", request.timestamp, "1712000000123")
		}
		if request.query.Get("limit") != "10" {
			t.Fatalf("limit query = %q, want %q", request.query.Get("limit"), "10")
		}
		if request.body != "" {
			t.Fatalf("GET body = %q, want empty", request.body)
		}
		if request.signature == "" {
			t.Fatal("KALSHI-ACCESS-SIGNATURE = empty, want non-empty")
		}

		sig, err := base64.StdEncoding.DecodeString(request.signature)
		if err != nil {
			t.Fatalf("DecodeString() error = %v", err)
		}

		message := signingMessage(request.timestamp, request.method, signedPath)
		hash := sha256.Sum256([]byte(message))
		if err := rsa.VerifyPSS(&privateKey.PublicKey, crypto.SHA256, hash[:], sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256}); err != nil {
			t.Fatalf("VerifyPSS() error = %v", err)
		}

		wrongMessage := signingMessage(request.timestamp, request.method, signedPath+"?limit=10")
		wrongHash := sha256.Sum256([]byte(wrongMessage))
		if err := rsa.VerifyPSS(&privateKey.PublicKey, crypto.SHA256, wrongHash[:], sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256}); err == nil {
			t.Fatal("VerifyPSS() with query-included message = nil, want error")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientMissingCredentials_RejectsAuthenticatedCallsButNotPublicCalls(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/trade-api/v2", "", "", discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())

	if _, err := client.Get(context.Background(), "/markets", nil, false); err != nil {
		t.Fatalf("public Get() error = %v", err)
	}

	if _, err := client.Get(context.Background(), "/markets", nil, true); err == nil {
		t.Fatal("authenticated Get() error = nil, want non-nil")
	}

	if _, err := client.Post(context.Background(), "/orders", map[string]any{"ticker": "TST"}); err == nil {
		t.Fatal("Post() error = nil, want non-nil")
	}
}

func TestNewClient_BadPrivateKeyReturnsError(t *testing.T) {
	t.Parallel()

	if _, err := NewClient("", "test-key-id", base64.StdEncoding.EncodeToString([]byte("not-a-key")), discardLogger()); err == nil {
		t.Fatal("NewClient() error = nil, want non-nil")
	}
}

func TestClientPost_SendsJSONBody(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)

	type requestDetails struct {
		method      string
		contentType string
		body        map[string]any
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
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

	client, err := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())
	client.setNowFunc(func() time.Time { return time.UnixMilli(1712000000123) })

	body, err := client.Post(context.Background(), "/orders", map[string]any{
		"marketTicker": "TST-1",
		"action":       "buy",
	})
	if err != nil {
		t.Fatalf("Post() error = %v", err)
	}
	if got := string(body); got != `{"id":"order-1"}` {
		t.Fatalf("Post() body = %q, want %q", got, `{"id":"order-1"}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodPost {
			t.Fatalf("method = %s, want %s", request.method, http.MethodPost)
		}
		if request.contentType != "application/json" {
			t.Fatalf("Content-Type = %q, want %q", request.contentType, "application/json")
		}
		if request.body["marketTicker"] != "TST-1" {
			t.Fatalf("marketTicker = %v, want %q", request.body["marketTicker"], "TST-1")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestQuoteCentsToProbability(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   float64
		want    float64
		wantErr bool
	}{
		{name: "one cent", input: 1, want: 0.01},
		{name: "ninety-nine cents", input: 99, want: 0.99},
		{name: "one hundred cents", input: 100, want: 1},
		{name: "zero", input: 0, want: 0},
		{name: "negative", input: -1, wantErr: true},
		{name: "over max", input: 100.0001, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := QuoteCentsToProbability(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("QuoteCentsToProbability(%v) error = nil, want non-nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("QuoteCentsToProbability(%v) error = %v", tt.input, err)
			}
			if got != tt.want {
				t.Fatalf("QuoteCentsToProbability(%v) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}

	if _, err := QuoteCentsToProbability(math.Inf(1)); err == nil {
		t.Fatal("QuoteCentsToProbability(+Inf) error = nil, want non-nil")
	}
	if _, err := QuoteCentsToProbability(math.NaN()); err == nil {
		t.Fatal("QuoteCentsToProbability(NaN) error = nil, want non-nil")
	}
}

func testPrivateKeyPEMB64(t *testing.T) (string, *rsa.PrivateKey) {
	t.Helper()

	privateKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}

	der := x509.MarshalPKCS1PrivateKey(privateKey)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der})
	return base64.StdEncoding.EncodeToString(pemBytes), privateKey
}
