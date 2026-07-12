package finnhub

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestClientAuthenticatesAndReturnsBody(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("token"); got != "secret" {
			t.Errorf("token = %q", got)
		}
		if got := r.URL.Query().Get("symbol"); got != "AAPL" {
			t.Errorf("symbol = %q", got)
		}
		_, _ = w.Write([]byte(`{"s":"ok"}`))
	}))
	defer server.Close()
	client := NewClient(" secret ", nil)
	client.baseURL = server.URL
	body, err := client.Get(context.Background(), "/stock/candle", url.Values{"symbol": {"AAPL"}})
	if err != nil || string(body) != `{"s":"ok"}` {
		t.Fatalf("Get() = %q, %v", body, err)
	}
}

func TestClientStartsProviderCooldownOnRateLimit(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"slow down"}`))
	}))
	defer server.Close()
	client := NewClient("secret", nil)
	client.baseURL = server.URL
	_, err := client.Get(context.Background(), "/stock/candle", nil)
	var responseErr *ErrorResponse
	if !errors.As(err, &responseErr) || responseErr.StatusCode() != http.StatusTooManyRequests {
		t.Fatalf("first error = %v", err)
	}
	_, err = client.Get(context.Background(), "/stock/candle", nil)
	if err == nil || !strings.Contains(err.Error(), "cooldown active") {
		t.Fatalf("second error = %v", err)
	}
}

func TestClientRejectsMissingAPIKey(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(" ", nil).Get(context.Background(), "/quote", nil); err == nil || !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("Get() error = %v", err)
	}
}
