package polygon

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

func TestListActiveTickersRespectsFreeTierRateLimit(t *testing.T) {
	t.Parallel()

	var requestedCursors []string
	var sleepCalls []time.Duration
	var serverURL string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		requestedCursors = append(requestedCursors, r.URL.Query().Get("cursor"))

		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = fmt.Fprintf(w, `{"results":[{"ticker":"AAA","name":"Alpha","primary_exchange":"XNAS","type":"CS","active":true}],"next_url":"%s/v3/reference/tickers?cursor=page-2"}`,
				serverURL,
			)
		case "page-2":
			_, _ = w.Write([]byte(`{"results":[{"ticker":"BBB","name":"Beta","primary_exchange":"XNYS","type":"CS","active":true}]}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
		}
	}))
	defer server.Close()
	serverURL = server.URL

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL
	client.SetTickerPageDelay(12 * time.Second)
	client.SetSleeper(func(_ context.Context, d time.Duration) error {
		sleepCalls = append(sleepCalls, d)
		return nil
	})

	tickers, err := client.ListActiveTickers(context.Background(), "stocks", "CS")
	if err != nil {
		t.Fatalf("ListActiveTickers() error = %v", err)
	}
	if len(tickers) != 2 {
		t.Fatalf("ListActiveTickers() count = %d, want 2", len(tickers))
	}
	if tickers[0].Ticker != "AAA" || tickers[1].Ticker != "BBB" {
		t.Fatalf("ListActiveTickers() tickers = %#v, want AAA then BBB", tickers)
	}
	if !reflect.DeepEqual(requestedCursors, []string{"", "page-2"}) {
		t.Fatalf("requested cursors = %#v, want first page then page-2", requestedCursors)
	}
	if !reflect.DeepEqual(sleepCalls, []time.Duration{12 * time.Second}) {
		t.Fatalf("sleep calls = %#v, want [12s]", sleepCalls)
	}
}

func TestTickerSnapshotUpdatedAtAcceptsProviderTimestampUnits(t *testing.T) {
	t.Parallel()

	want := time.Date(2026, time.August, 6, 12, 0, 0, 0, time.UTC)
	for name, value := range map[string]int64{
		"seconds":      want.Unix(),
		"milliseconds": want.UnixMilli(),
		"microseconds": want.UnixMicro(),
		"nanoseconds":  want.UnixNano(),
	} {
		t.Run(name, func(t *testing.T) {
			if got := (TickerSnapshot{Updated: value}).UpdatedAt(); !got.Equal(want) {
				t.Fatalf("UpdatedAt() = %s, want %s", got, want)
			}
		})
	}
	if got := (TickerSnapshot{}).UpdatedAt(); !got.IsZero() {
		t.Fatalf("zero UpdatedAt() = %s", got)
	}
}
