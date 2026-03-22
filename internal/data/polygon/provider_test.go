package polygon

import (
	"context"
	"errors"
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

	const expectedRequestCount = 2

	type requestDetails struct {
		path   string
		query  url.Values
		method string
	}

	from := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC)
	expectedFirstBarTimestamp := time.Date(2024, time.January, 1, 14, 30, 0, 0, time.UTC)
	expectedSecondBarTimestamp := time.Date(2024, time.January, 2, 14, 30, 0, 0, time.UTC)

	requests := make(chan requestDetails, expectedRequestCount)
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
			Timestamp: expectedFirstBarTimestamp,
			Open:      100.5,
			High:      110.25,
			Low:       99.5,
			Close:     105.75,
			Volume:    1234,
		},
		{
			Timestamp: expectedSecondBarTimestamp,
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
	for range expectedRequestCount {
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
	if secondRequest.query.Get("adjusted") != "true" {
		t.Fatalf("second request adjusted = %q, want true", secondRequest.query.Get("adjusted"))
	}
	if secondRequest.query.Get("sort") != "asc" {
		t.Fatalf("second request sort = %q, want asc", secondRequest.query.Get("sort"))
	}
	if secondRequest.query.Get("limit") != "50000" {
		t.Fatalf("second request limit = %q, want 50000", secondRequest.query.Get("limit"))
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

func TestProviderGetOHLCVReturnsErrorForNilClient(t *testing.T) {
	t.Parallel()

	provider := NewProvider(nil)

	_, err := provider.GetOHLCV(
		context.Background(),
		"AAPL",
		data.Timeframe1d,
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("GetOHLCV() error = nil, want non-nil")
	}
	if err.Error() != "polygon: client is nil" {
		t.Fatalf("GetOHLCV() error = %q, want %q", err.Error(), "polygon: client is nil")
	}
}

func TestProviderGetNews(t *testing.T) {
	t.Parallel()

	const expectedRequestCount = 2

	type requestDetails struct {
		path   string
		query  url.Values
		method string
	}

	from := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 2, 23, 59, 59, 0, time.UTC)

	requests := make(chan requestDetails, expectedRequestCount)
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
					{
						"title":"Apple jumps on earnings",
						"description":"Apple reported strong quarterly revenue.",
						"article_url":"https://example.com/aapl-1",
						"published_utc":"2024-01-01T14:30:00Z",
						"publisher":{"name":"Reuters"},
						"insights":[
							{"ticker":"AAPL","sentiment":"positive"},
							{"ticker":"MSFT","sentiment":"neutral"}
						]
					}
				],
				"next_url":"` + serverURL + `/v2/reference/news?cursor=page-2"
			}`))
		case "page-2":
			_, _ = w.Write([]byte(`{
				"results":[
					{
						"title":"Apple supply chain update",
						"description":"Suppliers expect steady demand.",
						"article_url":"https://example.com/aapl-2",
						"published_utc":"2024-01-02T15:45:00Z",
						"publisher":{"name":"Bloomberg"},
						"insights":[
							{"ticker":"AAPL","sentiment":"negative"}
						]
					}
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

	got, err := provider.GetNews(context.Background(), "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}

	want := []data.NewsArticle{
		{
			Title:       "Apple jumps on earnings",
			Summary:     "Apple reported strong quarterly revenue.",
			URL:         "https://example.com/aapl-1",
			Source:      "Reuters",
			PublishedAt: time.Date(2024, time.January, 1, 14, 30, 0, 0, time.UTC),
			Sentiment:   1,
		},
		{
			Title:       "Apple supply chain update",
			Summary:     "Suppliers expect steady demand.",
			URL:         "https://example.com/aapl-2",
			Source:      "Bloomberg",
			PublishedAt: time.Date(2024, time.January, 2, 15, 45, 0, 0, time.UTC),
			Sentiment:   -1,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetNews() = %#v, want %#v", got, want)
	}

	var captured []requestDetails
	for range expectedRequestCount {
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
	if firstRequest.path != "/v2/reference/news" {
		t.Fatalf("first request path = %s, want news endpoint", firstRequest.path)
	}
	if firstRequest.query.Get("ticker") != "AAPL" {
		t.Fatalf("first request ticker = %q, want AAPL", firstRequest.query.Get("ticker"))
	}
	if firstRequest.query.Get("published_utc.gte") != from.Format(time.RFC3339Nano) {
		t.Fatalf("first request published_utc.gte = %q, want %q", firstRequest.query.Get("published_utc.gte"), from.Format(time.RFC3339Nano))
	}
	if firstRequest.query.Get("published_utc.lte") != to.Format(time.RFC3339Nano) {
		t.Fatalf("first request published_utc.lte = %q, want %q", firstRequest.query.Get("published_utc.lte"), to.Format(time.RFC3339Nano))
	}
	if firstRequest.query.Get("sort") != "published_utc" {
		t.Fatalf("first request sort = %q, want published_utc", firstRequest.query.Get("sort"))
	}
	if firstRequest.query.Get("order") != "asc" {
		t.Fatalf("first request order = %q, want asc", firstRequest.query.Get("order"))
	}
	if firstRequest.query.Get("limit") != "1000" {
		t.Fatalf("first request limit = %q, want 1000", firstRequest.query.Get("limit"))
	}
	if firstRequest.query.Get("apiKey") != "test-key" {
		t.Fatalf("first request apiKey = %q, want test-key", firstRequest.query.Get("apiKey"))
	}

	secondRequest := captured[1]
	if secondRequest.path != "/v2/reference/news" {
		t.Fatalf("second request path = %s, want same news endpoint", secondRequest.path)
	}
	if secondRequest.query.Get("cursor") != "page-2" {
		t.Fatalf("second request cursor = %q, want page-2", secondRequest.query.Get("cursor"))
	}
	if secondRequest.query.Get("ticker") != "AAPL" {
		t.Fatalf("second request ticker = %q, want AAPL", secondRequest.query.Get("ticker"))
	}
	if secondRequest.query.Get("published_utc.gte") != from.Format(time.RFC3339Nano) {
		t.Fatalf("second request published_utc.gte = %q, want %q", secondRequest.query.Get("published_utc.gte"), from.Format(time.RFC3339Nano))
	}
	if secondRequest.query.Get("published_utc.lte") != to.Format(time.RFC3339Nano) {
		t.Fatalf("second request published_utc.lte = %q, want %q", secondRequest.query.Get("published_utc.lte"), to.Format(time.RFC3339Nano))
	}
	if secondRequest.query.Get("sort") != "published_utc" {
		t.Fatalf("second request sort = %q, want published_utc", secondRequest.query.Get("sort"))
	}
	if secondRequest.query.Get("order") != "asc" {
		t.Fatalf("second request order = %q, want asc", secondRequest.query.Get("order"))
	}
	if secondRequest.query.Get("limit") != "1000" {
		t.Fatalf("second request limit = %q, want 1000", secondRequest.query.Get("limit"))
	}
	if secondRequest.query.Get("apiKey") != "test-key" {
		t.Fatalf("second request apiKey = %q, want test-key", secondRequest.query.Get("apiKey"))
	}
}

func TestProviderGetNewsReturnsErrorForNilClient(t *testing.T) {
	t.Parallel()

	provider := NewProvider(nil)

	_, err := provider.GetNews(
		context.Background(),
		"AAPL",
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
	)
	if err == nil {
		t.Fatal("GetNews() error = nil, want non-nil")
	}
	if err.Error() != "polygon: client is nil" {
		t.Fatalf("GetNews() error = %q, want %q", err.Error(), "polygon: client is nil")
	}
}

func TestProviderUnsupportedMethodsReturnErrNotImplemented(t *testing.T) {
	t.Parallel()

	provider := NewProvider(&Client{})

	_, fundamentalsErr := provider.GetFundamentals(context.Background(), "AAPL")
	if !errors.Is(fundamentalsErr, data.ErrNotImplemented) {
		t.Fatalf("GetFundamentals() error = %v, want ErrNotImplemented", fundamentalsErr)
	}

	_, socialErr := provider.GetSocialSentiment(context.Background(), "AAPL")
	if !errors.Is(socialErr, data.ErrNotImplemented) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNotImplemented", socialErr)
	}
}
