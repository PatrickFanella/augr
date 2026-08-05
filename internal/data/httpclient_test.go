package data

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestAPIClientGet_Success(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method string
		path   string
		query  url.Values
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	client := NewAPIClient(APIClientConfig{
		BaseURL: server.URL,
		Auth:    AuthConfig{Style: AuthStyleNone},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	body, status, err := client.Get(context.Background(), "/v1/data", url.Values{
		"symbol": []string{"AAPL"},
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if status != http.StatusOK {
		t.Fatalf("Get() status = %d, want %d", status, http.StatusOK)
	}
	if got := string(body); got != `{"status":"OK"}` {
		t.Fatalf("Get() body = %q, want %q", got, `{"status":"OK"}`)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/v1/data" {
			t.Fatalf("request path = %s, want %s", request.path, "/v1/data")
		}
		if request.query.Get("symbol") != "AAPL" {
			t.Fatalf("symbol query = %q, want %q", request.query.Get("symbol"), "AAPL")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestAPIClientGet_AuthQueryParam(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		apiKey string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			apiKey: r.URL.Query().Get("apiKey"),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	client := NewAPIClient(APIClientConfig{
		BaseURL: server.URL,
		Auth: AuthConfig{
			Style:     AuthStyleQueryParam,
			ParamName: "apiKey",
			Value:     "test-key-123",
		},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	select {
	case request := <-requests:
		if request.apiKey != "test-key-123" {
			t.Fatalf("apiKey query = %q, want %q", request.apiKey, "test-key-123")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestAPIClientGet_TransportErrorRedactsQueryCredentials(t *testing.T) {
	t.Parallel()

	const secret = "provider-secret-that-must-not-leak"
	client := NewAPIClient(APIClientConfig{
		BaseURL: "https://provider.invalid",
		Auth: AuthConfig{
			Style:     AuthStyleQueryParam,
			ParamName: "apiKey",
			Value:     secret,
		},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})
	client.SetHTTPClient(&http.Client{Transport: roundTripperError{err: errors.New("dns unavailable")}})

	_, _, err := client.Get(context.Background(), "/v1/data", url.Values{"symbol": {"AAPL"}})
	if err == nil {
		t.Fatal("Get() error = nil, want transport error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Get() error leaked query credential: %q", err)
	}
	if strings.Contains(err.Error(), "apiKey=") || strings.Contains(err.Error(), "symbol=") {
		t.Fatalf("Get() error leaked query string: %q", err)
	}
	if !strings.Contains(err.Error(), "provider.invalid/v1/data") {
		t.Fatalf("Get() error = %q, want redacted request path", err)
	}
}

func TestAPIClientGet_NonSuccessBodyRedactsConfiguredCredential(t *testing.T) {
	t.Parallel()

	const secret = "provider-secret-that-must-not-leak"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"provider detected API key ` + secret + `"}`))
	}))
	defer server.Close()

	client := NewAPIClient(APIClientConfig{
		BaseURL: server.URL,
		Auth: AuthConfig{
			Style:     AuthStyleQueryParam,
			ParamName: "apiKey",
			Value:     secret,
		},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-success error")
	}
	if strings.Contains(err.Error(), secret) {
		t.Fatalf("Get() error leaked configured credential: %q", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("Get() error = %q, want redaction marker", err)
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Get() error type = %T, want *APIError", err)
	}
	if strings.Contains(string(apiErr.Body), secret) {
		t.Fatalf("APIError body leaked configured credential: %q", apiErr.Body)
	}
}

type roundTripperError struct{ err error }

func (r roundTripperError) RoundTrip(*http.Request) (*http.Response, error) { return nil, r.err }

func TestAPIClientGet_AuthHeader(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		apiKey string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			apiKey: r.Header.Get("X-Api-Key"),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	client := NewAPIClient(APIClientConfig{
		BaseURL: server.URL,
		Auth: AuthConfig{
			Style:      AuthStyleHeader,
			HeaderName: "X-Api-Key",
			Value:      "header-key-456",
		},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	select {
	case request := <-requests:
		if request.apiKey != "header-key-456" {
			t.Fatalf("X-Api-Key header = %q, want %q", request.apiKey, "header-key-456")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestAPIClientGet_AuthNone(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		hasAPIKeyQuery  bool
		hasAPIKeyHeader bool
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			hasAPIKeyQuery:  r.URL.Query().Get("apiKey") != "",
			hasAPIKeyHeader: r.Header.Get("X-Api-Key") != "",
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	client := NewAPIClient(APIClientConfig{
		BaseURL: server.URL,
		Auth:    AuthConfig{Style: AuthStyleNone},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	select {
	case request := <-requests:
		if request.hasAPIKeyQuery {
			t.Fatal("apiKey query present, want absent for AuthStyleNone")
		}
		if request.hasAPIKeyHeader {
			t.Fatal("X-Api-Key header present, want absent for AuthStyleNone")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestAPIClientGet_Non2xxReturnsAPIError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		statusCode int
		body       string
	}{
		{
			name:       "bad request",
			statusCode: http.StatusBadRequest,
			body:       `{"error":"invalid parameter"}`,
		},
		{
			name:       "unauthorized",
			statusCode: http.StatusUnauthorized,
			body:       `{"error":"invalid api key"}`,
		},
		{
			name:       "rate limited",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":"rate limit exceeded"}`,
		},
		{
			name:       "server error",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":"internal error"}`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			client := NewAPIClient(APIClientConfig{
				BaseURL: server.URL,
				Auth:    AuthConfig{Style: AuthStyleNone},
				Timeout: time.Second,
				Logger:  discardLogger(),
				Prefix:  "test",
			})

			_, status, err := client.Get(context.Background(), "/v1/data", nil)
			if err == nil {
				t.Fatal("Get() error = nil, want non-nil")
			}
			if status != tt.statusCode {
				t.Fatalf("Get() status = %d, want %d", status, tt.statusCode)
			}

			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("Get() error type = %T, want *APIError", err)
			}
			if apiErr.StatusCode != tt.statusCode {
				t.Fatalf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, tt.statusCode)
			}
			if string(apiErr.Body) != tt.body {
				t.Fatalf("APIError.Body = %q, want %q", string(apiErr.Body), tt.body)
			}
		})
	}
}

func TestAPIClientGet_ContextCancelled(t *testing.T) {
	t.Parallel()

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	client := NewAPIClient(APIClientConfig{
		BaseURL: server.URL,
		Auth:    AuthConfig{Style: AuthStyleNone},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := client.Get(ctx, "/v1/data", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Get() error = %v, want context.Canceled", err)
	}
	if serverHits != 0 {
		t.Fatalf("server hit count = %d, want %d", serverHits, 0)
	}
}

func TestAPIClientGet_WithRateLimiter(t *testing.T) {
	t.Parallel()

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	limiter := NewRateLimiter(1, time.Hour)

	client := NewAPIClient(APIClientConfig{
		BaseURL:     server.URL,
		Auth:        AuthConfig{Style: AuthStyleNone},
		Timeout:     time.Second,
		RateLimiter: limiter,
		Logger:      discardLogger(),
		Prefix:      "test",
	})

	// First request should succeed immediately (bucket starts full).
	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	if serverHits != 1 {
		t.Fatalf("server hit count after first request = %d, want %d", serverHits, 1)
	}

	// Second request should block and time out (bucket is empty, refill in 1 hour).
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, _, err = client.Get(ctx, "/v1/data", nil)
	if err == nil {
		t.Fatal("second Get() error = nil, want non-nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Get() error = %v, want context.DeadlineExceeded", err)
	}
	if serverHits != 1 {
		t.Fatalf("server hit count after second request = %d, want %d", serverHits, 1)
	}
}

func TestAPIClientGet_WithRateLimiterWaitsBeforeRequest(t *testing.T) {
	t.Parallel()

	const (
		limiterInterval = 80 * time.Millisecond
		minimumWait     = 60 * time.Millisecond
	)

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"OK"}`))
	}))
	defer server.Close()

	limiter := NewRateLimiter(1, limiterInterval)
	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true for initial token drain")
	}

	client := NewAPIClient(APIClientConfig{
		BaseURL:     server.URL,
		Auth:        AuthConfig{Style: AuthStyleNone},
		Timeout:     time.Second,
		RateLimiter: limiter,
		Logger:      discardLogger(),
		Prefix:      "test",
	})

	start := time.Now()
	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if elapsed := time.Since(start); elapsed < minimumWait {
		t.Fatalf("Get() elapsed = %v, want at least %v to show rate limiter gated the request", elapsed, minimumWait)
	}
	if serverHits != 1 {
		t.Fatalf("server hit count = %d, want %d", serverHits, 1)
	}
}

func TestAPIError_Error(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		err     *APIError
		wantMsg string
	}{
		{
			name:    "with body",
			err:     &APIError{StatusCode: 400, Body: []byte("bad request")},
			wantMsg: "api: bad request (status=400)",
		},
		{
			name:    "empty body uses status text",
			err:     &APIError{StatusCode: 404, Body: nil},
			wantMsg: "api: Not Found (status=404)",
		},
		{
			name:    "nil error",
			err:     nil,
			wantMsg: "api: request failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := tt.err.Error()
			if got != tt.wantMsg {
				t.Fatalf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestAPIClientGet_BuildURLError(t *testing.T) {
	t.Parallel()

	client := NewAPIClient(APIClientConfig{
		BaseURL: "://bad-url",
		Auth:    AuthConfig{Style: AuthStyleNone},
		Timeout: time.Second,
		Logger:  discardLogger(),
		Prefix:  "test",
	})

	_, _, err := client.Get(context.Background(), "/v1/data", nil)
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "parse base url") {
		t.Fatalf("Get() error = %q, want parse base url error", err.Error())
	}
}
