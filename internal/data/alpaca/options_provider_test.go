package alpaca

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestGetOptionsChain(t *testing.T) {
	t.Parallel()

	resp := snapshotsResponse{
		Snapshots: map[string]optionSnapshot{
			"AAPL241220C00150000": {
				LatestTrade:       &optionTrade{Price: 5.25, Size: 10},
				LatestQuote:       &optionQuote{AskPrice: 5.30, AskSize: 10, BidPrice: 5.20, BidSize: 5},
				ImpliedVolatility: ptrFloat(0.35),
				Greeks:            &optionGreeks{Delta: 0.65, Gamma: 0.03, Rho: 0.01, Theta: -0.05, Vega: 0.15},
			},
			"AAPL241220P00150000": {
				LatestTrade:       &optionTrade{Price: 2.10, Size: 5},
				LatestQuote:       &optionQuote{AskPrice: 2.15, AskSize: 8, BidPrice: 2.05, BidSize: 3},
				ImpliedVolatility: ptrFloat(0.30),
				Greeks:            &optionGreeks{Delta: -0.35, Gamma: 0.02, Rho: -0.01, Theta: -0.04, Vega: 0.12},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("APCA-API-KEY-ID") != "test-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	t.Cleanup(server.Close)

	provider := NewOptionsDataProvider("test-key", "test-secret", nil)
	provider.SetBaseURL(server.URL)

	t.Run("all contracts", func(t *testing.T) {
		t.Parallel()
		snaps, err := provider.GetOptionsChain(context.Background(), "AAPL", time.Time{}, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(snaps) != 2 {
			t.Fatalf("got %d snapshots, want 2", len(snaps))
		}
	})

	t.Run("filter by option type", func(t *testing.T) {
		t.Parallel()
		snaps, err := provider.GetOptionsChain(context.Background(), "AAPL", time.Time{}, domain.OptionTypeCall)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(snaps) != 1 {
			t.Fatalf("got %d snapshots, want 1", len(snaps))
		}
		if snaps[0].Contract.OptionType != domain.OptionTypeCall {
			t.Fatalf("got option type %q, want %q", snaps[0].Contract.OptionType, domain.OptionTypeCall)
		}
	})

	t.Run("filter by expiry", func(t *testing.T) {
		t.Parallel()
		expiry := time.Date(2024, 12, 20, 0, 0, 0, 0, time.UTC)
		snaps, err := provider.GetOptionsChain(context.Background(), "AAPL", expiry, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(snaps) != 2 {
			t.Fatalf("got %d snapshots, want 2", len(snaps))
		}

		noMatch := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		snaps, err = provider.GetOptionsChain(context.Background(), "AAPL", noMatch, "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(snaps) != 0 {
			t.Fatalf("got %d snapshots, want 0", len(snaps))
		}
	})

	t.Run("greeks and prices mapped", func(t *testing.T) {
		t.Parallel()
		snaps, err := provider.GetOptionsChain(context.Background(), "AAPL", time.Time{}, domain.OptionTypeCall)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(snaps) != 1 {
			t.Fatalf("got %d snapshots, want 1", len(snaps))
		}
		s := snaps[0]
		if s.Greeks.Delta != 0.65 {
			t.Errorf("delta = %f, want 0.65", s.Greeks.Delta)
		}
		if s.Greeks.IV != 0.35 {
			t.Errorf("IV = %f, want 0.35", s.Greeks.IV)
		}
		if s.Greeks.Rho != 0.01 {
			t.Errorf("Rho = %f, want 0.01", s.Greeks.Rho)
		}
		if s.Bid != 5.20 {
			t.Errorf("bid = %f, want 5.20", s.Bid)
		}
		if s.Ask != 5.30 {
			t.Errorf("ask = %f, want 5.30", s.Ask)
		}
		if s.Last != 5.25 {
			t.Errorf("last = %f, want 5.25", s.Last)
		}
		expectedMid := (5.20 + 5.30) / 2
		if s.Mid != expectedMid {
			t.Errorf("mid = %f, want %f", s.Mid, expectedMid)
		}
	})
}

func TestGetOptionsChainEmptyUnderlying(t *testing.T) {
	t.Parallel()
	provider := NewOptionsDataProvider("key", "secret", nil)
	_, err := provider.GetOptionsChain(context.Background(), "", time.Time{}, "")
	if err == nil {
		t.Fatal("expected error for empty underlying")
	}
}

func TestGetOptionsOHLCV(t *testing.T) {
	t.Parallel()

	resp := barsResponse{
		Bars: map[string][]optionBar{
			"AAPL241220C00150000": {
				{Timestamp: "2024-01-02T05:00:00Z", Open: 5.0, High: 5.5, Low: 4.8, Close: 5.25, Volume: 1200},
				{Timestamp: "2024-01-03T05:00:00Z", Open: 5.3, High: 5.6, Low: 5.1, Close: 5.4, Volume: 800},
			},
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	provider := NewOptionsDataProvider("test-key", "test-secret", nil)
	provider.SetBaseURL(server.URL)

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC)

	bars, err := provider.GetOptionsOHLCV(context.Background(), "AAPL241220C00150000", data.Timeframe1d, from, to)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bars) != 2 {
		t.Fatalf("got %d bars, want 2", len(bars))
	}
	if bars[0].Open != 5.0 {
		t.Errorf("bar[0].Open = %f, want 5.0", bars[0].Open)
	}
	if bars[1].Close != 5.4 {
		t.Errorf("bar[1].Close = %f, want 5.4", bars[1].Close)
	}
}

func TestGetOptionsOHLCVEmptySymbol(t *testing.T) {
	t.Parallel()
	provider := NewOptionsDataProvider("key", "secret", nil)
	_, err := provider.GetOptionsOHLCV(context.Background(), "", data.Timeframe1d, time.Now(), time.Now())
	if err == nil {
		t.Fatal("expected error for empty symbol")
	}
}

func TestMapTimeframe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		timeframe  data.Timeframe
		want       string
		wantErrMsg string
	}{
		{name: "1m", timeframe: data.Timeframe1m, want: "1Min"},
		{name: "5m", timeframe: data.Timeframe5m, want: "5Min"},
		{name: "15m", timeframe: data.Timeframe15m, want: "15Min"},
		{name: "1h", timeframe: data.Timeframe1h, want: "1Hour"},
		{name: "1d", timeframe: data.Timeframe1d, want: "1Day"},
		{name: "unsupported", timeframe: data.Timeframe("2h"), wantErrMsg: `alpaca/options: unsupported timeframe "2h"`},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := mapTimeframe(tt.timeframe)
			if tt.wantErrMsg != "" {
				if err == nil {
					t.Fatal("mapTimeframe() error = nil, want non-nil")
				}
				if err.Error() != tt.wantErrMsg {
					t.Fatalf("mapTimeframe() error = %q, want %q", err.Error(), tt.wantErrMsg)
				}
				return
			}

			if err != nil {
				t.Fatalf("mapTimeframe() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("mapTimeframe() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"message":"forbidden"}`, http.StatusForbidden)
	}))
	defer server.Close()

	provider := NewOptionsDataProvider("bad-key", "bad-secret", nil)
	provider.SetBaseURL(server.URL)

	_, err := provider.GetOptionsChain(context.Background(), "AAPL", time.Time{}, "")
	if err == nil {
		t.Fatal("expected error for HTTP 403")
	}
}

func ptrFloat(v float64) *float64 {
	return &v
}
