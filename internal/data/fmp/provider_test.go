package fmp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetFundamentalsUsesStableProfileEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stable/profile" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("symbol"); got != "AAPL" {
			t.Errorf("symbol = %q", got)
		}
		_, _ = w.Write([]byte(`[{"symbol":"AAPL","marketCap":12345}]`))
	}))
	defer server.Close()

	client := NewClient("secret", nil)
	client.baseURL = server.URL
	got, err := NewProvider(client).GetFundamentals(context.Background(), "AAPL")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}
	if got.MarketCap != 12345 {
		t.Fatalf("MarketCap = %v, want 12345", got.MarketCap)
	}
}
