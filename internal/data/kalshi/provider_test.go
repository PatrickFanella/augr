package kalshi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
)

func TestProviderLoadSnapshotMapsMarketPayload(t *testing.T) {
	t.Parallel()

	const ticker = "KXTEST-YESNO"
	now := time.Date(2026, time.June, 15, 9, 30, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trade-api/v2/markets/" + ticker:
			_, _ = fmt.Fprint(w, `{"market":{"ticker":"KXTEST-YESNO","title":"Will test happen?","status":"active","yes_bid":45,"yes_ask":47,"no_bid":53,"no_ask":55,"volume":1000,"open_interest":500,"close_time":"2026-12-31T23:59:59Z"}}`)
		case "/trade-api/v2/markets/" + ticker + "/orderbook":
			t.Fatal("orderbook endpoint should not be needed when market payload has full book")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(server.URL+"/trade-api/v2", slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.client.SetHTTPClient(server.Client())
	provider.client.setNowFunc(func() time.Time { return now })

	snapshot, err := provider.LoadSnapshot(context.Background(), ticker)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}

	if snapshot.Ticker != ticker || snapshot.Title != "Will test happen?" || snapshot.Status != "active" {
		t.Fatalf("unexpected identity fields: %+v", snapshot)
	}
	if snapshot.BestBidYes != 0.45 || snapshot.BestAskYes != 0.47 || snapshot.BestBidNo != 0.53 || snapshot.BestAskNo != 0.55 {
		t.Fatalf("unexpected book mapping: %+v", snapshot)
	}
	if snapshot.Volume != 1000 || snapshot.OpenInterest != 500 {
		t.Fatalf("unexpected liquidity mapping: %+v", snapshot)
	}
	if !snapshot.CloseTime.Equal(time.Date(2026, time.December, 31, 23, 59, 59, 0, time.UTC)) {
		t.Fatalf("CloseTime = %s, want parsed RFC3339 time", snapshot.CloseTime)
	}
	if !snapshot.FetchedAt.Equal(now) {
		t.Fatalf("FetchedAt = %s, want %s", snapshot.FetchedAt, now)
	}
}

func TestProviderUnsupportedMethodsReturnErrNotImplemented(t *testing.T) {
	t.Parallel()

	provider := NewProvider("", slog.New(slog.NewTextHandler(io.Discard, nil)))
	if _, err := provider.GetOHLCV(context.Background(), "KXTEST-YESNO", data.Timeframe1d, time.Time{}, time.Time{}); !errors.Is(err, data.ErrNotImplemented) {
		t.Fatalf("GetOHLCV() error = %v, want ErrNotImplemented", err)
	}
	if _, err := provider.GetFundamentals(context.Background(), "KXTEST-YESNO"); !errors.Is(err, data.ErrNotImplemented) {
		t.Fatalf("GetFundamentals() error = %v, want ErrNotImplemented", err)
	}
	if _, err := provider.GetNews(context.Background(), "KXTEST-YESNO", time.Time{}, time.Time{}); !errors.Is(err, data.ErrNotImplemented) {
		t.Fatalf("GetNews() error = %v, want ErrNotImplemented", err)
	}
	if _, err := provider.GetSocialSentiment(context.Background(), "KXTEST-YESNO", time.Time{}, time.Time{}); !errors.Is(err, data.ErrNotImplemented) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNotImplemented", err)
	}
}

func TestProviderLoadSnapshotMapsCentQuoteEdges(t *testing.T) {
	t.Parallel()

	const ticker = "KXEDGE-YESNO"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trade-api/v2/markets/" + ticker:
			_, _ = fmt.Fprint(w, `{"market":{"ticker":"KXEDGE-YESNO","title":"Edge cents?","status":"active","yes_bid":1,"yes_ask":99,"no_bid":99,"no_ask":100,"volume":1000,"open_interest":500,"close_time":"2026-12-31T23:59:59Z"}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(server.URL+"/trade-api/v2", slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.client.SetHTTPClient(server.Client())

	snapshot, err := provider.LoadSnapshot(context.Background(), ticker)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if snapshot.BestBidYes != 0.01 || snapshot.BestAskYes != 0.99 || snapshot.BestBidNo != 0.99 || snapshot.BestAskNo != 1 {
		t.Fatalf("unexpected cent quote mapping: %+v", snapshot)
	}
}

func TestProviderLoadSnapshotTreatsLegacyZeroQuotesAsPresent(t *testing.T) {
	t.Parallel()

	const ticker = "KXZERO-YESNO"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trade-api/v2/markets/" + ticker:
			_, _ = fmt.Fprint(w, `{"market":{"ticker":"KXZERO-YESNO","title":"Zero quotes?","status":"active","yes_bid":0,"yes_ask":1,"no_bid":99,"no_ask":100,"volume":1000,"open_interest":500,"close_time":"2026-12-31T23:59:59Z"}}`)
		case "/trade-api/v2/markets/" + ticker + "/orderbook":
			t.Fatal("orderbook endpoint should not be needed when legacy zero quote fields are present")
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(server.URL+"/trade-api/v2", slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.client.SetHTTPClient(server.Client())

	snapshot, err := provider.LoadSnapshot(context.Background(), ticker)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if snapshot.BestBidYes != 0 || snapshot.BestAskYes != 0.01 || snapshot.BestBidNo != 0.99 || snapshot.BestAskNo != 1 {
		t.Fatalf("unexpected zero quote mapping: %+v", snapshot)
	}
}

func TestProviderLoadSnapshotMapsCurrentDollarFields(t *testing.T) {
	t.Parallel()

	const ticker = "KXDOLLAR-YESNO"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trade-api/v2/markets/" + ticker:
			_, _ = fmt.Fprint(w, `{"market":{"ticker":"KXDOLLAR-YESNO","title":"Dollar fields?","status":"active","yes_bid_dollars":"0.12","yes_ask_dollars":"0.14","no_bid_dollars":"0.86","no_ask_dollars":"0.88","volume_fp":"123.45","open_interest_fp":"678.9","close_time":"2026-12-31T23:59:59Z"}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(server.URL+"/trade-api/v2", slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.client.SetHTTPClient(server.Client())

	snapshot, err := provider.LoadSnapshot(context.Background(), ticker)
	if err != nil {
		t.Fatalf("LoadSnapshot() error = %v", err)
	}
	if snapshot.BestBidYes != 0.12 || snapshot.BestAskYes != 0.14 || snapshot.BestBidNo != 0.86 || snapshot.BestAskNo != 0.88 {
		t.Fatalf("unexpected dollar quote mapping: %+v", snapshot)
	}
	if snapshot.Volume != 123.45 || snapshot.OpenInterest != 678.9 {
		t.Fatalf("unexpected fp liquidity mapping: %+v", snapshot)
	}
}

func TestProviderLoadSnapshotRejectsOutOfRangeQuoteCents(t *testing.T) {
	t.Parallel()

	const ticker = "KXBADCENTS-YESNO"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/trade-api/v2/markets/" + ticker:
			_, _ = fmt.Fprint(w, `{"market":{"ticker":"KXBADCENTS-YESNO","title":"Bad cents?","status":"active","yes_bid":101,"yes_ask":99,"no_bid":1,"no_ask":100,"volume":1000,"open_interest":500,"close_time":"2026-12-31T23:59:59Z"}}`)
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	provider := NewProvider(server.URL+"/trade-api/v2", slog.New(slog.NewTextHandler(io.Discard, nil)))
	provider.client.SetHTTPClient(server.Client())

	_, err := provider.LoadSnapshot(context.Background(), ticker)
	if err == nil || !strings.Contains(err.Error(), "yes_bid") || !strings.Contains(err.Error(), "out of range") {
		t.Fatalf("LoadSnapshot() error = %v, want yes_bid out-of-range error", err)
	}
}
