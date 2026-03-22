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

func TestProviderUnsupportedMethodsReturnErrNotImplemented(t *testing.T) {
	t.Parallel()

	provider := NewProvider(discardLogger())

	_, fundamentalsErr := provider.GetFundamentals(context.Background(), "AAPL")
	if !errors.Is(fundamentalsErr, data.ErrNotImplemented) {
		t.Fatalf("GetFundamentals() error = %v, want ErrNotImplemented", fundamentalsErr)
	}

	_, newsErr := provider.GetNews(context.Background(), "AAPL", time.Now(), time.Now())
	if !errors.Is(newsErr, data.ErrNotImplemented) {
		t.Fatalf("GetNews() error = %v, want ErrNotImplemented", newsErr)
	}

	_, socialErr := provider.GetSocialSentiment(context.Background(), "AAPL")
	if !errors.Is(socialErr, data.ErrNotImplemented) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNotImplemented", socialErr)
	}
}
