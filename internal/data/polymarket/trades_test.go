package polymarket

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDataAPIBaseURLFromCLOB(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantURL string
	}{
		{name: "default clob host", input: "https://clob.polymarket.com", wantURL: "https://data-api.polymarket.com"},
		{name: "custom local stub", input: "http://127.0.0.1:9000/base", wantURL: "http://127.0.0.1:9000"},
		{name: "empty", input: "", wantURL: defaultDataAPIBaseURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DataAPIBaseURLFromCLOB(tt.input); got != tt.wantURL {
				t.Fatalf("DataAPIBaseURLFromCLOB() = %q, want %q", got, tt.wantURL)
			}
		})
	}
}

func TestFetchRecentTrades(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trades" {
			t.Fatalf("request path = %s, want /trades", r.URL.Path)
		}
		if got := r.URL.Query().Get("limit"); got != "2" {
			t.Fatalf("limit = %q, want 2", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"proxyWallet":"0xabc","slug":"will-it-rain","outcome":"Yes","price":0.61,"size":42.5,"timestamp":1710000000},
			{"proxyWallet":"0xdef","slug":"btc-above-100k","outcome":"No","price":0.39,"size":100,"timestamp":1710000600}
		]`))
	}))
	defer server.Close()

	trades, err := FetchRecentTrades(context.Background(), server.URL, 2)
	if err != nil {
		t.Fatalf("FetchRecentTrades() error = %v", err)
	}
	if len(trades) != 2 {
		t.Fatalf("len(trades) = %d, want 2", len(trades))
	}
	if trades[0].Address != "0xabc" {
		t.Fatalf("trades[0].Address = %q, want 0xabc", trades[0].Address)
	}
	if trades[0].MarketSlug != "will-it-rain" {
		t.Fatalf("trades[0].MarketSlug = %q, want will-it-rain", trades[0].MarketSlug)
	}
	if trades[0].Price != 0.61 {
		t.Fatalf("trades[0].Price = %v, want 0.61", trades[0].Price)
	}
	if trades[0].Size != 42.5 {
		t.Fatalf("trades[0].Size = %v, want 42.5", trades[0].Size)
	}
}
