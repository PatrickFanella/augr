package kalshidiscovery

import (
	"context"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	dataKalshi "github.com/PatrickFanella/get-rich-quick/internal/data/kalshi"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
)

func testDiscoveryLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestClientListMarkets_DecodesWrappedResponse(t *testing.T) {
	t.Parallel()

	requests := make(chan url.Values, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[{"ticker":"KAL-1","event_ticker":"EVT-1","title":"Will it rain?","category":"weather","status":"open","yes_bid":1,"yes_ask":99,"no_bid":100,"no_ask":0,"volume":123.5,"open_interest":456.75,"close_time":"2026-06-18T12:30:00Z"}],"cursor":"next-page"}`))
	}))
	defer server.Close()

	api, err := dataKalshi.NewClient(server.URL+"/trade-api/v2", "", "", testDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	api.SetHTTPClient(server.Client())
	client := NewClient(api)

	markets, cursor, err := client.ListMarkets(context.Background(), ListOptions{Limit: 500, Status: "open", Category: "weather"})
	if err != nil {
		t.Fatalf("ListMarkets() error = %v", err)
	}
	if cursor != "next-page" {
		t.Fatalf("cursor = %q, want %q", cursor, "next-page")
	}
	if len(markets) != 1 {
		t.Fatalf("len(markets) = %d, want 1", len(markets))
	}
	market := markets[0]
	if market.Ticker != "KAL-1" || market.EventTicker != "EVT-1" || market.Title != "Will it rain?" {
		t.Fatalf("market = %#v, want parsed fields", market)
	}
	if market.YesBid != 0.01 || market.YesAsk != 0.99 || market.NoBid != 1 || market.NoAsk != 0 {
		t.Fatalf("market quotes = %#v, want converted cents", market)
	}
	if market.Volume != 123.5 || market.OpenInterest != 456.75 {
		t.Fatalf("market liquidity = %#v, want parsed floats", market)
	}
	if market.CloseTime == nil || market.CloseTime.UTC().Format(time.RFC3339) != "2026-06-18T12:30:00Z" {
		t.Fatalf("CloseTime = %#v, want parsed UTC time", market.CloseTime)
	}
	if string(market.Raw) == "" {
		t.Fatal("Raw = empty, want original market JSON")
	}

	select {
	case query := <-requests:
		if got := query.Get("limit"); got != "100" {
			t.Fatalf("limit = %q, want 100", got)
		}
		if got := query.Get("status"); got != "open" {
			t.Fatalf("status = %q, want open", got)
		}
		if got := query.Get("category"); got != "" {
			t.Fatalf("category = %q, want empty unsupported query", got)
		}
	case <-time.After(time.Second):
		t.Fatal("request query not captured")
	}
}

func TestClientListMarkets_DecodesArrayResponse(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"ticker":"KAL-2","yes_bid":100,"yes_ask":1,"no_bid":0,"no_ask":99,"volume":1,"open_interest":2}]`))
	}))
	defer server.Close()

	api, err := dataKalshi.NewClient(server.URL+"/trade-api/v2", "", "", testDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	api.SetHTTPClient(server.Client())
	client := NewClient(api)

	markets, cursor, err := client.ListMarkets(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("ListMarkets() error = %v", err)
	}
	if cursor != "" {
		t.Fatalf("cursor = %q, want empty", cursor)
	}
	if len(markets) != 1 || markets[0].YesBid != 1 || markets[0].YesAsk != 0.01 || markets[0].NoAsk != 0.99 {
		t.Fatalf("markets = %#v, want parsed array response", markets)
	}
}

func TestClientSyncCatalog_PaginatesAndPersists(t *testing.T) {
	t.Parallel()

	requests := make(chan url.Values, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{"markets":[{"ticker":"KAL-1","event_ticker":"EVT-1","title":"First","status":"open","yes_bid":1,"yes_ask":99,"no_bid":100,"no_ask":0,"volume":10,"open_interest":20,"close_time":"2026-06-18T12:30:00Z"}],"cursor":"page-2"}`))
		case "page-2":
			_, _ = w.Write([]byte(`[{"ticker":"KAL-2","event_ticker":"EVT-2","title":"Second","status":"open","yes_bid":99,"yes_ask":100,"no_bid":1,"no_ask":0,"volume":30,"open_interest":40,"close_time":"2026-06-19T12:30:00Z"}]`))
		default:
			t.Fatalf("unexpected cursor %q", r.URL.Query().Get("cursor"))
		}
	}))
	defer server.Close()

	api, err := dataKalshi.NewClient(server.URL+"/trade-api/v2", "", "", testDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	api.SetHTTPClient(server.Client())
	client := NewClient(api)

	snapshots := &fakeSnapshotRepo{}
	watched := &fakeWatchedRepo{}
	selected := func(candidate MarketCandidate) bool { return candidate.Ticker == "KAL-2" }

	result, err := client.SyncCatalog(context.Background(), snapshots, watched, ListOptions{Limit: 500}, selected)
	if err != nil {
		t.Fatalf("SyncCatalog() error = %v", err)
	}
	if result.Fetched != 2 || result.SnapshotsPersisted != 2 || result.WatchedUpserted != 1 {
		t.Fatalf("result = %#v, want two snapshots and one watched upsert", result)
	}
	if len(snapshots.created) != 2 {
		t.Fatalf("created snapshots = %d, want 2", len(snapshots.created))
	}
	if len(watched.upserts) != 1 || watched.upserts[0].Ticker != "KAL-2" {
		t.Fatalf("watched upserts = %#v, want only selected candidate", watched.upserts)
	}

	select {
	case first := <-requests:
		if got := first.Get("limit"); got != "100" {
			t.Fatalf("first limit = %q, want 100", got)
		}
		if got := first.Get("status"); got != "open" {
			t.Fatalf("first status = %q, want open", got)
		}
	case <-time.After(time.Second):
		t.Fatal("first request not captured")
	}
	select {
	case second := <-requests:
		if got := second.Get("cursor"); got != "page-2" {
			t.Fatalf("second cursor = %q, want page-2", got)
		}
	case <-time.After(time.Second):
		t.Fatal("second request not captured")
	}
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
			got, err := dataKalshi.QuoteCentsToProbability(tt.input)
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

	if _, err := dataKalshi.QuoteCentsToProbability(math.Inf(1)); err == nil {
		t.Fatal("QuoteCentsToProbability(+Inf) error = nil, want non-nil")
	}
	if _, err := dataKalshi.QuoteCentsToProbability(math.NaN()); err == nil {
		t.Fatal("QuoteCentsToProbability(NaN) error = nil, want non-nil")
	}
}

func TestClientListMarkets_DecodesCurrentDollarFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[{"ticker":"KAL-DOLLAR","event_ticker":"EVT-DOLLAR","title":"Dollar fields?","status":"settled","result":"yes","yes_bid_dollars":"0.12","yes_ask_dollars":"0.14","no_bid_dollars":"0.86","no_ask_dollars":"0.88","volume_fp":"123.45","open_interest_fp":"678.9","close_time":"2026-06-18T12:30:00Z"}]}`))
	}))
	defer server.Close()

	api, err := dataKalshi.NewClient(server.URL+"/trade-api/v2", "", "", testDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	api.SetHTTPClient(server.Client())
	client := NewClient(api)

	markets, _, err := client.ListMarkets(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("ListMarkets() error = %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("len(markets) = %d, want 1", len(markets))
	}
	market := markets[0]
	if market.Result != "yes" {
		t.Fatalf("market result = %q, want yes", market.Result)
	}
	if market.YesBid != 0.12 || market.YesAsk != 0.14 || market.NoBid != 0.86 || market.NoAsk != 0.88 {
		t.Fatalf("market quotes = %#v, want dollar fields parsed as probabilities", market)
	}
	if market.Volume != 123.45 || market.OpenInterest != 678.9 {
		t.Fatalf("market liquidity = %#v, want *_fp fields", market)
	}
}

func TestClientListMarkets_PrefersCurrentLiquidityFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[{"ticker":"KAL-LIQ","yes_bid":50,"yes_ask":51,"no_bid":49,"no_ask":50,"volume":1,"volume_fp":"123.45","open_interest":2,"open_interest_fp":"678.9"}]}`))
	}))
	defer server.Close()

	api, err := dataKalshi.NewClient(server.URL+"/trade-api/v2", "", "", testDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	api.SetHTTPClient(server.Client())
	client := NewClient(api)

	markets, _, err := client.ListMarkets(context.Background(), ListOptions{})
	if err != nil {
		t.Fatalf("ListMarkets() error = %v", err)
	}
	if len(markets) != 1 {
		t.Fatalf("len(markets) = %d, want 1", len(markets))
	}
	if markets[0].Volume != 123.45 || markets[0].OpenInterest != 678.9 {
		t.Fatalf("liquidity = volume %v open_interest %v, want current *_fp fields", markets[0].Volume, markets[0].OpenInterest)
	}
}

func TestClientListMarkets_RejectsOutOfRangeDollarFields(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[{"ticker":"KAL-BAD","yes_bid_dollars":"1.01"}]}`))
	}))
	defer server.Close()

	api, err := dataKalshi.NewClient(server.URL+"/trade-api/v2", "", "", testDiscoveryLogger())
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	api.SetHTTPClient(server.Client())
	client := NewClient(api)

	_, _, err = client.ListMarkets(context.Background(), ListOptions{})
	if err == nil || !strings.Contains(err.Error(), "quote dollars") {
		t.Fatalf("ListMarkets() error = %v, want dollar range error", err)
	}
}

type fakeSnapshotRepo struct {
	created []*domain.KalshiMarketSnapshot
}

func (r *fakeSnapshotRepo) Create(_ context.Context, snapshot *domain.KalshiMarketSnapshot) error {
	r.created = append(r.created, snapshot)
	return nil
}

func (r *fakeSnapshotRepo) ListLatestByTicker(context.Context, string, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

func (r *fakeSnapshotRepo) ListRecent(context.Context, int) ([]domain.KalshiMarketSnapshot, error) {
	return nil, nil
}

type fakeWatchedRepo struct{ upserts []*domain.KalshiWatchedMarket }

func (r *fakeWatchedRepo) Upsert(_ context.Context, market *domain.KalshiWatchedMarket) error {
	r.upserts = append(r.upserts, market)
	return nil
}

func (r *fakeWatchedRepo) SetEnabled(context.Context, string, bool) error { return nil }

func (r *fakeWatchedRepo) ListEnabled(context.Context) ([]domain.KalshiWatchedMarket, error) {
	return nil, nil
}

var _ repository.KalshiMarketSnapshotsRepository = (*fakeSnapshotRepo)(nil)
var _ repository.KalshiWatchedMarketsRepository = (*fakeWatchedRepo)(nil)
