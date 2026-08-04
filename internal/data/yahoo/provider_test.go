package yahoo

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProviderGetOHLCV(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method    string
		path      string
		query     url.Values
		userAgent string
	}

	from := time.Date(2024, time.January, 1, 14, 30, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 1, 14, 32, 0, 0, time.UTC)

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method:    r.Method,
			path:      r.URL.Path,
			query:     r.URL.Query(),
			userAgent: r.UserAgent(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"chart": {
				"result": [
					{
						"timestamp": [1704119400, 1704119460, 1704119520],
						"indicators": {
							"quote": [
								{
									"open": [100.5, null, 101.2],
									"high": [101.0, null, 102.1],
									"low": [100.0, null, 100.9],
									"close": [100.8, null, 101.9],
									"volume": [1200, null, 1500]
								}
							]
						}
					}
				],
				"error": null
			}
		}`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()

	got, err := provider.GetOHLCV(context.Background(), "AAPL", data.Timeframe1m, from, to)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}

	want := []domain.OHLCV{
		{
			Timestamp: time.Unix(1704119400, 0).UTC(),
			Open:      100.5,
			High:      101.0,
			Low:       100.0,
			Close:     100.8,
			Volume:    1200,
		},
		{
			Timestamp: time.Unix(1704119520, 0).UTC(),
			Open:      101.2,
			High:      102.1,
			Low:       100.9,
			Close:     101.9,
			Volume:    1500,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetOHLCV() = %#v, want %#v", got, want)
	}

	select {
	case request := <-requests:
		if request.method != http.MethodGet {
			t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
		}
		if request.path != "/v8/finance/chart/AAPL" {
			t.Fatalf("request path = %s, want %s", request.path, "/v8/finance/chart/AAPL")
		}
		if request.query.Get("interval") != "1m" {
			t.Fatalf("interval = %q, want %q", request.query.Get("interval"), "1m")
		}
		if request.query.Get("includePrePost") != "false" {
			t.Fatalf("includePrePost = %q, want %q", request.query.Get("includePrePost"), "false")
		}
		if request.query.Get("period1") != "1704119400" {
			t.Fatalf("period1 = %q, want %q", request.query.Get("period1"), "1704119400")
		}
		if request.query.Get("period2") != "1704119580" {
			t.Fatalf("period2 = %q, want %q", request.query.Get("period2"), "1704119580")
		}
		if request.userAgent != defaultUA {
			t.Fatalf("User-Agent = %q, want %q", request.userAgent, defaultUA)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestProviderGetOHLCVNormalizesDotShareTickersForChartPath(t *testing.T) {
	t.Parallel()

	requests := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chart":{"result":[],"error":null}}`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()

	from := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)

	if _, err := provider.GetOHLCV(context.Background(), "BRK.A", data.Timeframe1d, from, to); err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}

	select {
	case path := <-requests:
		if path != "/v8/finance/chart/BRK-A" {
			t.Fatalf("request path = %s, want %s", path, "/v8/finance/chart/BRK-A")
		}
	case <-time.After(time.Second):
		t.Fatal("request path was not captured")
	}
}

func TestProviderGetOHLCVEmptyResults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"chart":{"result":[],"error":null}}`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()

	got, err := provider.GetOHLCV(
		context.Background(),
		"AAPL",
		data.Timeframe1d,
		time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
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

func TestProviderGetOHLCVErrorResponses(t *testing.T) {
	t.Parallel()

	from := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		statusCode     int
		responseBody   string
		wantErrMessage string
	}{
		{
			name:           "non-2xx status",
			statusCode:     http.StatusTooManyRequests,
			responseBody:   `rate limit exceeded`,
			wantErrMessage: "yahoo: request failed with status 429: rate limit exceeded",
		},
		{
			name:       "chart error response",
			statusCode: http.StatusOK,
			responseBody: `{
				"chart": {
					"result": null,
					"error": {
						"code": "Not Found",
						"description": "No data found, symbol may be delisted"
					}
				}
			}`,
			wantErrMessage: "yahoo: No data found, symbol may be delisted",
		},
		{
			name:           "invalid json",
			statusCode:     http.StatusOK,
			responseBody:   `{"chart":`,
			wantErrMessage: "yahoo: decode chart response: unexpected end of JSON input",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			provider := NewProvider(discardLogger())
			provider.baseURL = server.URL
			provider.httpClient = server.Client()

			_, err := provider.GetOHLCV(context.Background(), "AAPL", data.Timeframe1d, from, to)
			if err == nil {
				t.Fatal("GetOHLCV() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErrMessage {
				t.Fatalf("GetOHLCV() error = %q, want %q", err.Error(), tt.wantErrMessage)
			}
		})
	}
}

func TestProviderUnsupportedMethodsReturnErrNotImplemented(t *testing.T) {
	t.Parallel()

	provider := NewProvider(discardLogger())

	_, fundamentalsErr := provider.GetFundamentals(context.Background(), "AAPL")
	if !errors.Is(fundamentalsErr, data.ErrNotImplemented) {
		t.Fatalf("GetFundamentals() error = %v, want ErrNotImplemented", fundamentalsErr)
	}

	_, socialErr := provider.GetSocialSentiment(context.Background(), "AAPL", time.Now().Add(-time.Hour), time.Now())
	if !errors.Is(socialErr, data.ErrNotImplemented) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNotImplemented", socialErr)
	}
}

func TestProviderGetNews(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, time.April, 15, 0, 0, 0, 0, time.UTC)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/finance/search" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query().Get("q")
		if q != "AAPL" {
			t.Fatalf("expected q=AAPL, got %s", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"news": [
				{
					"title": "Apple earnings beat",
					"link": "https://example.com/1",
					"publisher": "Reuters",
					"providerPublishTime": 1776038400,
					"relatedTickers": ["AAPL"]
				},
				{
					"title": "Old article",
					"link": "https://example.com/2",
					"publisher": "Bloomberg",
					"providerPublishTime": 1644502400
				}
			]
		}`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL

	articles, err := provider.GetNews(context.Background(), "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}

	// Only the article within the time range should be returned.
	if len(articles) != 1 {
		t.Fatalf("expected 1 article, got %d", len(articles))
	}
	if len(articles[0].RelatedTickers) != 1 || articles[0].RelatedTickers[0] != "AAPL" {
		t.Fatalf("RelatedTickers = %#v, want [AAPL]", articles[0].RelatedTickers)
	}
	if articles[0].Title != "Apple earnings beat" {
		t.Fatalf("expected title 'Apple earnings beat', got %q", articles[0].Title)
	}
	if articles[0].Source != "Reuters" {
		t.Fatalf("expected source 'Reuters', got %q", articles[0].Source)
	}
	if articles[0].URL != "https://example.com/1" {
		t.Fatalf("expected URL 'https://example.com/1', got %q", articles[0].URL)
	}
}

func TestProviderGetNewsEmptyTicker(t *testing.T) {
	t.Parallel()
	provider := NewProvider(discardLogger())
	_, err := provider.GetNews(context.Background(), "", time.Now(), time.Now())
	if err == nil || !strings.Contains(err.Error(), "ticker is required") {
		t.Fatalf("expected ticker required error, got %v", err)
	}
}
