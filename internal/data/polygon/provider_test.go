package polygon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestProviderGetOHLCV(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		path   string
		query  url.Values
		method string
	}

	from := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC)
	firstBarTime := time.Date(2024, time.January, 1, 14, 30, 0, 0, time.UTC)
	secondBarTime := time.Date(2024, time.January, 2, 14, 30, 0, 0, time.UTC)

	requests := make(chan requestDetails, 2)
	var serverURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			path:   r.URL.Path,
			query:  r.URL.Query(),
			method: r.Method,
		}

		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("cursor") {
		case "":
			_, _ = w.Write([]byte(`{
				"results":[
					{"o":100.5,"h":110.25,"l":99.5,"c":105.75,"v":1234,"t":1704119400000}
				],
				"next_url":"` + serverURL + `/v2/aggs/ticker/AAPL/range/1/day/1704067200000/1704240000000?cursor=page-2"
			}`))
		case "page-2":
			_, _ = w.Write([]byte(`{
				"results":[
					{"o":106,"h":112,"l":104.5,"c":111.25,"v":2345,"t":1704205800000}
				]
			}`))
		default:
			t.Fatalf("unexpected cursor = %q", r.URL.Query().Get("cursor"))
		}
	}))
	serverURL = server.URL
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL
	provider := NewProvider(client)

	got, err := provider.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, from, to)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}

	want := []domain.OHLCV{
		{
			Timestamp: firstBarTime,
			Open:      100.5,
			High:      110.25,
			Low:       99.5,
			Close:     105.75,
			Volume:    1234,
		},
		{
			Timestamp: secondBarTime,
			Open:      106,
			High:      112,
			Low:       104.5,
			Close:     111.25,
			Volume:    2345,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetOHLCV() = %#v, want %#v", got, want)
	}

	var captured []requestDetails
	for range 2 {
		select {
		case request := <-requests:
			captured = append(captured, request)
		case <-time.After(time.Second):
			t.Fatal("request details were not captured")
		}
	}

	slices.SortFunc(captured, func(a, b requestDetails) int {
		switch {
		case a.query.Get("cursor") < b.query.Get("cursor"):
			return -1
		case a.query.Get("cursor") > b.query.Get("cursor"):
			return 1
		default:
			return 0
		}
	})

	firstRequest := captured[0]
	if firstRequest.method != http.MethodGet {
		t.Fatalf("first request method = %s, want %s", firstRequest.method, http.MethodGet)
	}
	if firstRequest.path != "/v2/aggs/ticker/AAPL/range/1/day/1704067200000/1704240000000" {
		t.Fatalf("first request path = %s, want aggregates endpoint", firstRequest.path)
	}
	if firstRequest.query.Get("adjusted") != "true" {
		t.Fatalf("first request adjusted = %q, want true", firstRequest.query.Get("adjusted"))
	}
	if firstRequest.query.Get("sort") != "asc" {
		t.Fatalf("first request sort = %q, want asc", firstRequest.query.Get("sort"))
	}
	if firstRequest.query.Get("limit") != "50000" {
		t.Fatalf("first request limit = %q, want 50000", firstRequest.query.Get("limit"))
	}
	if firstRequest.query.Get("apiKey") != "test-key" {
		t.Fatalf("first request apiKey = %q, want test-key", firstRequest.query.Get("apiKey"))
	}

	secondRequest := captured[1]
	if secondRequest.path != "/v2/aggs/ticker/AAPL/range/1/day/1704067200000/1704240000000" {
		t.Fatalf("second request path = %s, want same aggregates endpoint", secondRequest.path)
	}
	if secondRequest.query.Get("cursor") != "page-2" {
		t.Fatalf("second request cursor = %q, want page-2", secondRequest.query.Get("cursor"))
	}
	if secondRequest.query.Get("apiKey") != "test-key" {
		t.Fatalf("second request apiKey = %q, want test-key", secondRequest.query.Get("apiKey"))
	}
}

func TestMapTimeframe(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		timeframe  data.Timeframe
		want       timeframeMapping
		wantErrMsg string
	}{
		{name: "1m", timeframe: data.Timeframe1m, want: timeframeMapping{multiplier: 1, timespan: "minute"}},
		{name: "5m", timeframe: data.Timeframe5m, want: timeframeMapping{multiplier: 5, timespan: "minute"}},
		{name: "15m", timeframe: data.Timeframe15m, want: timeframeMapping{multiplier: 15, timespan: "minute"}},
		{name: "1h", timeframe: data.Timeframe1h, want: timeframeMapping{multiplier: 1, timespan: "hour"}},
		{name: "1d", timeframe: data.Timeframe1d, want: timeframeMapping{multiplier: 1, timespan: "day"}},
		{name: "unsupported", timeframe: data.Timeframe("2h"), wantErrMsg: `polygon: unsupported timeframe "2h"`},
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
				t.Fatalf("mapTimeframe() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestProviderGetOHLCVEmptyResults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL
	provider := NewProvider(client)

	got, err := provider.GetOHLCV(
		context.Background(),
		"AAPL",
		data.Timeframe1h,
		time.Date(2024, time.January, 1, 14, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 1, 16, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}
	if got == nil {
		t.Fatal("GetOHLCV() = nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("GetOHLCV() len = %d, want 0", len(got))
	}
}
