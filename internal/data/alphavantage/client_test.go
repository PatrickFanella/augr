package alphavantage

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

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestClientGetSuccess(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method   string
		path     string
		apiKey   string
		function string
		symbol   string
	}

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method:   r.Method,
			path:     r.URL.Path,
			apiKey:   r.URL.Query().Get("apikey"),
			function: r.URL.Query().Get("function"),
			symbol:   r.URL.Query().Get("symbol"),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Meta Data":{"1. Information":"Daily Prices"}}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL + "/query"
	client.SetTimeout(time.Second)

	body, err := client.Get(context.Background(), url.Values{
		"function": []string{"TIME_SERIES_DAILY"},
		"symbol":   []string{"AAPL"},
	})
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if got := string(body); got != `{"Meta Data":{"1. Information":"Daily Prices"}}` {
		t.Fatalf("Get() body = %q, want successful payload", got)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/query" {
			t.Fatalf("request path = %s, want %s", request.path, "/query")
		}
		if request.apiKey != "test-key" {
			t.Fatalf("apikey query = %q, want %q", request.apiKey, "test-key")
		}
		if request.function != "TIME_SERIES_DAILY" {
			t.Fatalf("function query = %q, want %q", request.function, "TIME_SERIES_DAILY")
		}
		if request.symbol != "AAPL" {
			t.Fatalf("symbol query = %q, want %q", request.symbol, "AAPL")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestClientGetReturnsInvalidAPIKeyError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Error Message":"the parameter apikey is invalid or missing. Please claim your free API key."}`))
	}))
	defer server.Close()

	client := NewClient("bad-key", discardLogger())
	client.baseURL = server.URL + "/query"

	_, err := client.Get(context.Background(), url.Values{
		"function": []string{"TIME_SERIES_DAILY"},
	})
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
	if !strings.Contains(strings.ToLower(err.Error()), "apikey") {
		t.Fatalf("Get() error = %q, want invalid api key message", err.Error())
	}
}

func TestClientGetReturnsRateLimitError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Note":"Thank you for using Alpha Vantage! Our standard API rate limit is 25 requests per day."}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL + "/query"

	_, err := client.Get(context.Background(), url.Values{
		"function": []string{"TIME_SERIES_DAILY"},
	})
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
	if !strings.Contains(strings.ToLower(err.Error()), "rate limit") {
		t.Fatalf("Get() error = %q, want rate limit message", err.Error())
	}
}

func TestClientGetDoesNotConsumeRateLimitQuotaWhenURLBuildFails(t *testing.T) {
	t.Parallel()

	limiter := data.NewRateLimiter(1, time.Hour)
	client := NewClient("test-key", discardLogger(), limiter)
	client.baseURL = "://bad-url"

	_, err := client.Get(context.Background(), url.Values{
		"function": []string{"TIME_SERIES_DAILY"},
	})
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "parse base url") {
		t.Fatalf("Get() error = %q, want parse base url error", err.Error())
	}
	if !limiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want limiter token preserved after URL build failure")
	}
}

func TestClientGetWaitsForAllRateLimiters(t *testing.T) {
	t.Parallel()

	const (
		firstInterval  = 60 * time.Millisecond
		secondInterval = 120 * time.Millisecond
		minimumWait    = 100 * time.Millisecond
	)

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Global Quote":{"01. symbol":"AAPL"}}`))
	}))
	defer server.Close()

	firstLimiter := data.NewRateLimiter(1, firstInterval)
	secondLimiter := data.NewRateLimiter(1, secondInterval)
	if !firstLimiter.TryAcquire() {
		t.Fatal("firstLimiter.TryAcquire() = false, want true for initial token")
	}
	if !secondLimiter.TryAcquire() {
		t.Fatal("secondLimiter.TryAcquire() = false, want true for initial token")
	}

	client := NewClient("test-key", discardLogger(), firstLimiter, secondLimiter)
	client.baseURL = server.URL + "/query"

	start := time.Now()
	if _, err := client.Get(context.Background(), url.Values{
		"function": []string{"GLOBAL_QUOTE"},
		"symbol":   []string{"AAPL"},
	}); err != nil {
		t.Fatalf("Get() error = %v, want nil", err)
	}

	if elapsed := time.Since(start); elapsed < minimumWait {
		t.Fatalf("Get() elapsed = %v, want at least %v to show both limiters gated the request", elapsed, minimumWait)
	}
	if serverHits != 1 {
		t.Fatalf("server hit count = %d, want %d", serverHits, 1)
	}
}

func TestClientGetRespectsContextCancellationDuringRateLimiting(t *testing.T) {
	t.Parallel()

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Global Quote":{"01. symbol":"AAPL"}}`))
	}))
	defer server.Close()

	firstLimiter := data.NewRateLimiter(1, time.Hour)
	secondLimiter := data.NewRateLimiter(1, time.Hour)
	if !secondLimiter.TryAcquire() {
		t.Fatal("secondLimiter.TryAcquire() = false, want true for initial token")
	}

	client := NewClient("test-key", discardLogger(), firstLimiter, secondLimiter)
	client.baseURL = server.URL + "/query"

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.Get(ctx, url.Values{
		"function": []string{"GLOBAL_QUOTE"},
		"symbol":   []string{"AAPL"},
	})
	if err == nil {
		t.Fatal("Get() error = nil, want non-nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Get() error = %v, want context deadline exceeded", err)
	}
	if serverHits != 0 {
		t.Fatalf("server hit count = %d, want %d", serverHits, 0)
	}
	if !firstLimiter.TryAcquire() {
		t.Fatal("firstLimiter.TryAcquire() = false, want token returned after cancellation while waiting on second limiter")
	}
}

func TestClientGetRespectsContextCancellationDuringSingleLimiterRateLimiting(t *testing.T) {
	t.Parallel()

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"Global Quote":{"01. symbol":"AAPL"}}`))
	}))
	defer server.Close()

	limiter := data.NewRateLimiter(1, time.Hour)
	client := NewClient("test-key", discardLogger(), limiter)
	client.baseURL = server.URL + "/query"

	if _, err := client.Get(context.Background(), url.Values{
		"function": []string{"GLOBAL_QUOTE"},
		"symbol":   []string{"AAPL"},
	}); err != nil {
		t.Fatalf("first Get() error = %v, want nil", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := client.Get(ctx, url.Values{
		"function": []string{"GLOBAL_QUOTE"},
		"symbol":   []string{"AAPL"},
	})
	if err == nil {
		t.Fatal("second Get() error = nil, want non-nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("second Get() error = %v, want context deadline exceeded", err)
	}
	if serverHits != 1 {
		t.Fatalf("server hit count = %d, want %d", serverHits, 1)
	}
}
