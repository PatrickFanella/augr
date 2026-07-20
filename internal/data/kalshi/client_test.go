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
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/metrics"
	provgov "github.com/PatrickFanella/get-rich-quick/internal/providergovernor"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

func TestClientDelete_SendsAuthHeadersAndOmitsContentType(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, privateKey := testPrivateKeyPEMB64(t)
	timestamp := time.UnixMilli(1712000000456)

	type requestDetails struct {
		method      string
		path        string
		query       url.Values
		contentType string
		accessID    string
		timestamp   string
		signature   string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method:      r.Method,
			path:        r.URL.Path,
			query:       r.URL.Query(),
			contentType: r.Header.Get("Content-Type"),
			accessID:    r.Header.Get("KALSHI-ACCESS-KEY"),
			timestamp:   r.Header.Get("KALSHI-ACCESS-TIMESTAMP"),
			signature:   r.Header.Get("KALSHI-ACCESS-SIGNATURE"),
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

	body, err := client.Delete(context.Background(), "/orders/abc", url.Values{"reason": []string{"user_cancel"}})
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if got := string(body); got != `{"ok":true}` {
		t.Fatalf("Delete() body = %q, want %q", got, `{"ok":true}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodDelete {
			t.Fatalf("method = %s, want %s", request.method, http.MethodDelete)
		}
		if request.path != "/trade-api/v2/orders/abc" {
			t.Fatalf("path = %s, want %s", request.path, "/trade-api/v2/orders/abc")
		}
		if request.query.Get("reason") != "user_cancel" {
			t.Fatalf("reason query = %q, want %q", request.query.Get("reason"), "user_cancel")
		}
		if request.contentType != "" {
			t.Fatalf("Content-Type = %q, want empty", request.contentType)
		}
		if request.accessID != "test-key-id" {
			t.Fatalf("KALSHI-ACCESS-KEY = %q, want %q", request.accessID, "test-key-id")
		}
		if request.timestamp != "1712000000456" {
			t.Fatalf("KALSHI-ACCESS-TIMESTAMP = %q, want %q", request.timestamp, "1712000000456")
		}
		if request.signature == "" {
			t.Fatal("KALSHI-ACCESS-SIGNATURE = empty, want non-empty")
		}

		sig, err := base64.StdEncoding.DecodeString(request.signature)
		if err != nil {
			t.Fatalf("DecodeString() error = %v", err)
		}
		message := signingMessage(request.timestamp, request.method, "/trade-api/v2/orders/abc")
		hash := sha256.Sum256([]byte(message))
		if err := rsa.VerifyPSS(&privateKey.PublicKey, crypto.SHA256, hash[:], sig, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash, Hash: crypto.SHA256}); err != nil {
			t.Fatalf("VerifyPSS() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientPost_429ReturnsTypedErrorWithoutRetry(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limit"}`))
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	m := metrics.New()
	client.SetHTTPClient(server.Client())
	client.SetMetrics(m, "execution")
	client.SetRetryPolicy(3, 10*time.Millisecond, time.Second, 0)
	gov := &countingGovernor{}
	client.SetGovernor(gov)

	_, err = client.Post(context.Background(), "/orders", map[string]any{"ticker": "TST"})
	if err == nil {
		t.Fatal("Post() error = nil, want typed 429")
	}
	var rl *provgov.RateLimitError
	if !errors.As(err, &rl) {
		t.Fatalf("error type = %T, want *RateLimitError", err)
	}
	if rl.StatusCode != http.StatusTooManyRequests || rl.RetryAfter != 7*time.Second || calls != 1 {
		t.Fatalf("rate limit error = %+v calls=%d", rl, calls)
	}
	if got := testutil.ToFloat64(m.KalshiRateLimitTotal.WithLabelValues("kalshi", "execution", http.MethodPost)); got != 1 {
		t.Fatalf("kalshi rate limit metric = %v, want 1", got)
	}
	if gov.reserves != 1 {
		t.Fatalf("reserve calls = %d, want 1", gov.reserves)
	}
}

func TestClientPost_429PersistsCooldownAcrossClientsAndRestart(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	store := &fakeCooldownStore{}
	client, err := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, 10*time.Millisecond, time.Second, 0)
	client.SetGovernor(&provgov.ProviderGovernor{Provider: "kalshi", Limiter: noOpLimiter{}, Cooldown: store, Clock: time.Now})
	if _, err := client.Post(context.Background(), "/orders", map[string]any{"ticker": "TST"}); err == nil {
		t.Fatal("Post() error = nil, want typed 429")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
	client2, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client2.SetHTTPClient(server.Client())
	client2.SetRetryPolicy(3, 10*time.Millisecond, time.Second, 0)
	client2.SetGovernor(&provgov.ProviderGovernor{Provider: "kalshi", Limiter: noOpLimiter{}, Cooldown: store, Clock: time.Now})
	if _, err := client2.Post(context.Background(), "/orders", map[string]any{"ticker": "TST"}); err == nil {
		t.Fatal("Post() error = nil, want durable cooldown")
	}
	if calls != 1 {
		t.Fatalf("calls after cooldown = %d, want 1", calls)
	}
}

func TestClientPostCooldownPersistenceErrorFailsClosed(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	store := &fakeCooldownStore{setErr: errors.New("persist failed")}
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, 10*time.Millisecond, time.Second, 0)
	client.SetGovernor(&provgov.ProviderGovernor{Provider: "kalshi", Limiter: noOpLimiter{}, Cooldown: store, Clock: time.Now})
	if _, err := client.Post(context.Background(), "/orders", map[string]any{"ticker": "TST"}); err == nil || !strings.Contains(err.Error(), "persist failed") {
		t.Fatalf("Post() error = %v, want persist failure", err)
	}
}

func TestProviderGovernorReserveHonorsCooldownAndContextCancellation(t *testing.T) {
	t.Parallel()
	store := &fakeCooldownStore{cooldown: map[string]time.Time{"kalshi": time.Now().Add(time.Hour)}}
	gov := &provgov.ProviderGovernor{Provider: "kalshi", Limiter: noOpLimiter{}, Cooldown: store, Clock: time.Now}
	if err := gov.Reserve(context.Background()); err == nil {
		t.Fatal("Reserve() error = nil, want cooldown")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store2 := &fakeCooldownStore{getErr: context.Canceled}
	gov2 := &provgov.ProviderGovernor{Provider: "kalshi", Limiter: noOpLimiter{}, Cooldown: store2, Clock: time.Now}
	if err := gov2.Reserve(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reserve() error = %v, want context.Canceled", err)
	}
}

func TestParseRetryAfterSupportsSecondsAndHTTPDate(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 19, 12, 0, 0, 0, time.UTC)
	if got := provgov.ParseRetryAfter("5", func() time.Time { return now }); got != 5*time.Second {
		t.Fatalf("seconds retry-after = %s", got)
	}
	date := now.Add(10 * time.Second).Format(http.TimeFormat)
	if got := provgov.ParseRetryAfter(date, func() time.Time { return now }); got != 10*time.Second {
		t.Fatalf("date retry-after = %s", got)
	}
}

func TestClientGetRetriesWithCapAndCancellation(t *testing.T) {
	t.Parallel()
	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`too many`))
	}))
	defer server.Close()
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(2, time.Millisecond, time.Second, 0)
	client.SetMetrics(nil, "data")
	gov := &countingGovernor{}
	client.SetGovernor(gov)
	_, err := client.Get(context.Background(), "/markets", nil, true)
	if err == nil {
		t.Fatal("Get() error = nil, want retry failure")
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if gov.reserves != 2 {
		t.Fatalf("reserve calls = %d, want 2", gov.reserves)
	}
}

func TestClientGetRecordsRetryMetricsFor5xxAnd429(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		switch calls {
		case 1:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`bad gateway`))
		case 2:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`rate limit`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
		}
	}))
	defer server.Close()

	m := metrics.New()
	client, err := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())
	client.SetMetrics(m, "data")
	client.SetRetryPolicy(3, 10*time.Millisecond, time.Second, 0)
	client.SetHooks(nil, func() float64 { return 0.0 }, func(context.Context, time.Duration) error { return nil })

	if _, err := client.Get(context.Background(), "/markets", nil, true); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got := testutil.ToFloat64(m.KalshiRetryAttemptsTotal.WithLabelValues("kalshi", "data", http.MethodGet)); got != 2 {
		t.Fatalf("retry attempts = %v, want 2", got)
	}
	if got := testutil.CollectAndCount(m.KalshiRetryWaitSeconds); got == 0 {
		t.Fatal("retry wait histogram was not observed")
	}
	if got := testutil.ToFloat64(m.KalshiRateLimitTotal.WithLabelValues("kalshi", "data", http.MethodGet)); got != 1 {
		t.Fatalf("rate limit count = %v, want 1", got)
	}
}

func TestClientGetCancelsOnSleepError(t *testing.T) {
	t.Parallel()
	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, time.Second, time.Second, 0)
	wants := errors.New("sleep failed")
	client.SetHooks(nil, nil, func(context.Context, time.Duration) error { return wants })
	_, err := client.Get(context.Background(), "/markets", nil, true)
	if !errors.Is(err, wants) {
		t.Fatalf("Get() error = %v, want sleep error", err)
	}
}

func TestClientGetHonorsContextCancellation(t *testing.T) {
	t.Parallel()
	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, time.Second, time.Second, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Get(ctx, "/markets", nil, true)
	if err == nil {
		t.Fatal("Get() error = nil, want context error")
	}
}

func TestClientGetUsesDeterministicMaxBackoffAndJitter(t *testing.T) {
	t.Parallel()
	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var waits []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, 2*time.Second, 1500*time.Millisecond, 0.5)
	client.SetHooks(nil, func() float64 { return 0.0 }, func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	_, _ = client.Get(context.Background(), "/markets", nil, true)
	if len(waits) == 0 || waits[0] != 1500*time.Millisecond {
		t.Fatalf("waits = %#v, want capped 1500ms", waits)
	}
}

func TestClientGetHonorsRetryAfterFloor(t *testing.T) {
	t.Parallel()
	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var waits []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, time.Millisecond, time.Second, 0.5)
	client.SetHooks(nil, func() float64 { return 0.0 }, func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	_, _ = client.Get(context.Background(), "/markets", nil, true)
	if len(waits) == 0 || waits[0] < time.Second {
		t.Fatalf("waits = %#v, want >= Retry-After", waits)
	}
}

func TestClientGetStopsImmediatelyWhenRetryAfterExceedsMaxBackoff(t *testing.T) {
	t.Parallel()
	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	var waits []time.Duration
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	client, _ := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(3, time.Millisecond, time.Second, 0.5)
	client.SetHooks(nil, func() float64 { return 0.0 }, func(ctx context.Context, d time.Duration) error { waits = append(waits, d); return nil })
	_, err := client.Get(context.Background(), "/markets", nil, true)
	if err == nil {
		t.Fatal("Get() error = nil, want 429")
	}
	if len(waits) != 0 {
		t.Fatalf("waits = %#v, want no sleep", waits)
	}
}

func TestClientGetConcurrentRetriesAreRaceSafe(t *testing.T) {
	t.Parallel()

	privateKeyPEMB64, _ := testPrivateKeyPEMB64(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trade-api/v2/markets/ok" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	client, err := NewClient(server.URL+"/trade-api/v2", "test-key-id", privateKeyPEMB64, discardLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	client.SetHTTPClient(server.Client())
	client.SetRetryPolicy(2, time.Millisecond, time.Second, 0.5)
	client.SetHooks(nil, func() float64 { return 0.25 }, func(context.Context, time.Duration) error { return nil })
	client.SetMetrics(nil, "data")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.Get(context.Background(), "/markets/ok", nil, true)
		}()
	}
	wg.Wait()
}

type countingGovernor struct{ reserves int }

func (g *countingGovernor) Reserve(context.Context) error              { g.reserves++; return nil }
func (g *countingGovernor) Sleep(context.Context, time.Duration) error { return nil }

type noOpLimiter struct{}

func (noOpLimiter) Wait(context.Context) error { return nil }

type fakeCooldownStore struct {
	mu       sync.Mutex
	cooldown map[string]time.Time
	setErr   error
	getErr   error
}

func (s *fakeCooldownStore) GetProviderCooldown(context.Context, string) (time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.getErr != nil {
		return time.Time{}, s.getErr
	}
	if s.cooldown == nil {
		return time.Time{}, nil
	}
	return s.cooldown["kalshi"], nil
}
func (s *fakeCooldownStore) SetProviderCooldown(_ context.Context, _ string, until time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setErr != nil {
		return s.setErr
	}
	if s.cooldown == nil {
		s.cooldown = map[string]time.Time{}
	}
	s.cooldown["kalshi"] = until
	return nil
}
func (s *fakeCooldownStore) CompareAndClearProviderCooldown(context.Context, string, time.Time) (bool, error) {
	return true, nil
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
