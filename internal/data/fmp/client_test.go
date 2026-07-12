package fmp

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
		if got := r.URL.Query().Get("apikey"); got != "secret" {
			t.Errorf("apikey = %q", got)
		}
		if got := r.URL.Query().Get("limit"); got != "5" {
			t.Errorf("limit = %q", got)
		}
		_, _ = w.Write([]byte(`[{"symbol":"AAPL"}]`))
	}))
	defer server.Close()
	client := NewClient(" secret ", nil)
	client.baseURL = server.URL
	body, err := client.Get(context.Background(), "/profile/AAPL", url.Values{"limit": {"5"}})
	if err != nil || string(body) != `[{"symbol":"AAPL"}]` {
		t.Fatalf("Get() = %q, %v", body, err)
	}
}

func TestClientReturnsTypedProviderError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"Error Message":"plan does not include endpoint"}`))
	}))
	defer server.Close()
	client := NewClient("secret", nil)
	client.baseURL = server.URL
	_, err := client.Get(context.Background(), "/profile/AAPL", nil)
	var responseErr *ErrorResponse
	if !errors.As(err, &responseErr) || responseErr.StatusCode() != http.StatusForbidden || !strings.Contains(err.Error(), "plan does not include endpoint") {
		t.Fatalf("Get() error = %v", err)
	}
}

func TestClientRejectsMissingAPIKey(t *testing.T) {
	t.Parallel()
	if _, err := NewClient(" ", nil).Get(context.Background(), "/profile/AAPL", nil); err == nil || !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("Get() error = %v", err)
	}
}
