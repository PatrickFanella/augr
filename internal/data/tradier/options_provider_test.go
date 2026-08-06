package tradier

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

type stubRequestLimiter struct {
	calls int
	err   error
}

func (s *stubRequestLimiter) Wait(context.Context) error {
	s.calls++
	return s.err
}

func TestOptionsProviderMapsAndFiltersChain(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.URL.Query().Get("symbol"); got != "AAPL" {
			t.Errorf("symbol = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"options":{"option":[{"symbol":"AAPL270115C00150000","strike":150,"bid":2,"ask":4,"last":2.5,"volume":12,"open_interest":30,"contract_size":100,"option_type":"call","expiration_date":"2027-01-15","greeks":{"delta":0.4,"gamma":0.02,"theta":-0.1,"vega":0.2,"rho":0.03,"mid_iv":0.25}},{"symbol":"AAPL270115P00150000","strike":150,"bid":3,"ask":5,"last":3.5,"option_type":"put","expiration_date":"2027-01-15"}]}}`))
	}))
	defer server.Close()

	provider := NewOptionsProvider(" test-token ", true, slog.Default())
	provider.baseURL = server.URL
	chain, err := provider.GetOptionsChain(context.Background(), " aapl ", time.Date(2027, 1, 15, 0, 0, 0, 0, time.UTC), domain.OptionTypeCall)
	if err != nil {
		t.Fatalf("GetOptionsChain() error = %v", err)
	}
	if len(chain) != 1 {
		t.Fatalf("chain length = %d, want 1", len(chain))
	}
	got := chain[0]
	if got.Mid != 3 || got.Greeks.Delta != 0.4 || got.Contract.Multiplier != 100 || got.Contract.Underlying != "AAPL" {
		t.Fatalf("mapped snapshot = %#v", got)
	}
}

func TestOptionsProviderRejectsMissingTokenAndUnsupportedHistory(t *testing.T) {
	t.Parallel()

	provider := NewOptionsProvider("  ", true, nil)
	if _, err := provider.GetOptionsChain(context.Background(), "AAPL", time.Now(), ""); err == nil || !strings.Contains(err.Error(), "token is required") {
		t.Fatalf("missing-token error = %v", err)
	}
	if _, err := provider.GetOptionsOHLCV(context.Background(), "contract", data.Timeframe1d, time.Time{}, time.Time{}); !errors.Is(err, data.ErrNotImplemented) {
		t.Fatalf("GetOptionsOHLCV() error = %v, want ErrNotImplemented", err)
	}
}

func TestOptionsProviderUsesEnvironmentMarketDataQuota(t *testing.T) {
	t.Parallel()

	if got := NewOptionsProvider("token", true, nil).rateLimitPerMinute; got != sandboxMarketDataPerMinute {
		t.Fatalf("sandbox rate limit = %d, want %d", got, sandboxMarketDataPerMinute)
	}
	if got := NewOptionsProvider("token", false, nil).rateLimitPerMinute; got != productionMarketDataPerMinute {
		t.Fatalf("production rate limit = %d, want %d", got, productionMarketDataPerMinute)
	}
}

func TestOptionsProviderWaitsForQuotaBeforeRequest(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		_, _ = w.Write([]byte(`{"options":null}`))
	}))
	defer server.Close()

	limiter := &stubRequestLimiter{err: context.Canceled}
	provider := NewOptionsProvider("token", true, nil)
	provider.baseURL = server.URL
	provider.limiter = limiter

	_, err := provider.GetOptionsChain(context.Background(), "AAPL", time.Now(), "")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("GetOptionsChain() error = %v, want context.Canceled", err)
	}
	if limiter.calls != 1 {
		t.Fatalf("limiter calls = %d, want 1", limiter.calls)
	}
	if requests != 0 {
		t.Fatalf("HTTP requests = %d, want 0", requests)
	}
}

func TestNearestExpiryRejectsMalformedFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"expirations":{"date":["not-a-date"]}}`))
	}))
	defer server.Close()
	provider := NewOptionsProvider("token", true, nil)
	provider.baseURL = server.URL
	if _, err := provider.nearestExpiry(context.Background(), "AAPL"); err == nil || !strings.Contains(err.Error(), "invalid expiration") {
		t.Fatalf("nearestExpiry() error = %v", err)
	}
}
