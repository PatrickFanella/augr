package polymarket

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestBroker_DryRunAppendsQueryParam(t *testing.T) {
	t.Parallel()

	if got := withDryRunQuery("/v1/orders"); got != "/v1/orders?dry=1" {
		t.Fatalf("withDryRunQuery() = %q, want %q", got, "/v1/orders?dry=1")
	}
	if got := withDryRunQuery("/v1/orders?foo=bar"); !strings.Contains(got, "foo=bar") || !strings.Contains(got, "dry=1") {
		t.Fatalf("withDryRunQuery() = %q, want preserved query with dry=1", got)
	}
}

func TestClassifyDryRunError_NSF(t *testing.T) {
	t.Parallel()
	tests := []error{errors.New("insufficient balance"), errors.New("nsf"), errors.New("balance low")}
	for _, err := range tests {
		if got, ok := ClassifyDryRunError(err); !ok || got != "nsf" {
			t.Fatalf("ClassifyDryRunError(%q) = %q,%v", err, got, ok)
		}
	}
}

func TestClassifyDryRunError_Timeout(t *testing.T) {
	t.Parallel()
	if got, ok := ClassifyDryRunError(context.DeadlineExceeded); !ok || got != "timeout" {
		t.Fatalf("ClassifyDryRunError(timeout) = %q,%v", got, ok)
	}
	if got, ok := ClassifyDryRunError(errors.New("request timeout")); !ok || got != "timeout" {
		t.Fatalf("ClassifyDryRunError(request timeout) = %q,%v", got, ok)
	}
}

func TestClassifyDryRunError_GhostFill(t *testing.T) {
	t.Parallel()
	if got, ok := ClassifyDryRunError(errors.New("ghost_fill detected")); !ok || got != "ghost_fill" {
		t.Fatalf("ClassifyDryRunError(ghost_fill) = %q,%v", got, ok)
	}
}

func TestClassifyDryRunError_Other(t *testing.T) {
	t.Parallel()
	if got, ok := ClassifyDryRunError(errors.New("rejected for policy")); !ok || got != "other" {
		t.Fatalf("ClassifyDryRunError(other) = %q,%v", got, ok)
	}
}

func TestBroker_DryRunReturnsRejectedError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("insufficient balance"))
	}))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	b := NewBroker(client)
	b.DryRun = true

	_, err := b.SubmitOrder(context.Background(), &domain.Order{Ticker: "btc-100k", Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeLimit, Quantity: 2, LimitPrice: floatPtr(0.5)})
	var rejected *DryRunRejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("SubmitOrder() error = %v, want DryRunRejectedError", err)
	}
	if rejected.Observation.Kind != "nsf" || rejected.Observation.Slug != "btc-100k" || rejected.Observation.Side == "" || rejected.Observation.Price != 0.5 || rejected.Observation.Size != 2 {
		t.Fatalf("observation = %+v", rejected.Observation)
	}
}

func TestBroker_DryRunHonorsBreaker(t *testing.T) {
	t.Parallel()

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { hits++; t.Fatal("server should not be hit") }))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	b := NewBroker(client)
	b.DryRun = true
	b.Breaker = &fakeBreaker{allowErr: errors.New("breaker tripped")}

	_, err := b.SubmitOrder(context.Background(), &domain.Order{Ticker: "btc-100k", Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeLimit, Quantity: 1, LimitPrice: floatPtr(0.5)})
	if err == nil || err.Error() != "breaker tripped" {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
	if hits != 0 {
		t.Fatalf("hits = %d, want 0", hits)
	}
}

func TestBroker_DryRunRespectsContextTimeout(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { time.Sleep(10 * time.Millisecond) }))
	defer server.Close()

	client := newTestClient()
	client.SetAPIBaseURL(server.URL)
	b := NewBroker(client)
	b.DryRun = true

	ctx, cancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer cancel()
	_, err := b.SubmitOrder(ctx, &domain.Order{Ticker: "btc-100k", Side: domain.OrderSideBuy, PredictionSide: "YES", OrderType: domain.OrderTypeLimit, Quantity: 1, LimitPrice: floatPtr(0.5)})
	var rejected *DryRunRejectedError
	if !errors.As(err, &rejected) || rejected.Observation.Kind != "timeout" {
		t.Fatalf("SubmitOrder() error = %v", err)
	}
}
