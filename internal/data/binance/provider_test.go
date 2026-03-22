package binance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
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
		_, _ = w.Write([]byte(`[
			[1704119400000, "42000.10", "42050.20", "41980.00", "42025.50", "12.34", 1704119459999, "0", 0, "0", "0", "0"],
			[1704119460000, "42025.50", "42080.00", "42010.10", "42075.90", "8.76", 1704119519999, "0", 0, "0", "0", "0"],
			[1704119520000, "42075.90", "42110.00", "42070.40", "42100.30", "10.01", 1704119579999, "0", 0, "0", "0", "0"]
		]`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()

	got, err := provider.GetOHLCV(context.Background(), "btcusdt", data.Timeframe1m, from, to)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}

	want := []domain.OHLCV{
		{
			Timestamp: time.UnixMilli(1704119400000).UTC(),
			Open:      42000.10,
			High:      42050.20,
			Low:       41980.00,
			Close:     42025.50,
			Volume:    12.34,
		},
		{
			Timestamp: time.UnixMilli(1704119460000).UTC(),
			Open:      42025.50,
			High:      42080.00,
			Low:       42010.10,
			Close:     42075.90,
			Volume:    8.76,
		},
		{
			Timestamp: time.UnixMilli(1704119520000).UTC(),
			Open:      42075.90,
			High:      42110.00,
			Low:       42070.40,
			Close:     42100.30,
			Volume:    10.01,
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
		if request.path != "/api/v3/klines" {
			t.Fatalf("request path = %s, want %s", request.path, "/api/v3/klines")
		}
		if request.query.Get("symbol") != "BTCUSDT" {
			t.Fatalf("symbol = %q, want %q", request.query.Get("symbol"), "BTCUSDT")
		}
		if request.query.Get("interval") != "1m" {
			t.Fatalf("interval = %q, want %q", request.query.Get("interval"), "1m")
		}
		if request.query.Get("startTime") != strconv.FormatInt(from.UnixMilli(), 10) {
			t.Fatalf("startTime = %q, want %q", request.query.Get("startTime"), strconv.FormatInt(from.UnixMilli(), 10))
		}
		if request.query.Get("endTime") != strconv.FormatInt(to.UnixMilli(), 10) {
			t.Fatalf("endTime = %q, want %q", request.query.Get("endTime"), strconv.FormatInt(to.UnixMilli(), 10))
		}
		if request.query.Get("limit") != strconv.Itoa(maxKlinesPerRequest) {
			t.Fatalf("limit = %q, want %q", request.query.Get("limit"), strconv.Itoa(maxKlinesPerRequest))
		}
		if request.userAgent != defaultUA {
			t.Fatalf("User-Agent = %q, want %q", request.userAgent, defaultUA)
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestProviderGetOHLCVPaginates(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		startTime string
	}

	from := time.Date(2024, time.January, 1, 0, 0, 0, 0, time.UTC)
	to := from.Add(1001 * time.Minute)

	requests := make(chan requestDetails, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{startTime: r.URL.Query().Get("startTime")}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("startTime") {
		case strconv.FormatInt(from.UnixMilli(), 10):
			payload, err := marshalKlines(from, maxKlinesPerRequest, time.Minute)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(payload)
		case strconv.FormatInt(from.Add(1000*time.Minute).UnixMilli(), 10):
			payload, err := marshalKlines(from.Add(1000*time.Minute), 2, time.Minute)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			_, _ = w.Write(payload)
		default:
			http.Error(w, "unexpected startTime", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()

	got, err := provider.GetOHLCV(context.Background(), "BTCUSDT", data.Timeframe1m, from, to)
	if err != nil {
		t.Fatalf("GetOHLCV() error = %v", err)
	}

	if len(got) != 1002 {
		t.Fatalf("GetOHLCV() len = %d, want %d", len(got), 1002)
	}
	if got[0].Timestamp != from {
		t.Fatalf("first timestamp = %v, want %v", got[0].Timestamp, from)
	}
	if got[len(got)-1].Timestamp != to {
		t.Fatalf("last timestamp = %v, want %v", got[len(got)-1].Timestamp, to)
	}

	var captured []requestDetails
	for i := 0; i < 2; i++ {
		select {
		case request := <-requests:
			captured = append(captured, request)
		case <-time.After(time.Second):
			t.Fatal("request details were not captured")
		}
	}

	wantStarts := []string{
		strconv.FormatInt(from.UnixMilli(), 10),
		strconv.FormatInt(from.Add(1000*time.Minute).UnixMilli(), 10),
	}
	for i, request := range captured {
		if request.startTime != wantStarts[i] {
			t.Fatalf("request %d startTime = %q, want %q", i, request.startTime, wantStarts[i])
		}
	}
}

func TestProviderGetOHLCVEmptyResults(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()

	got, err := provider.GetOHLCV(
		context.Background(),
		"BTCUSDT",
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
			responseBody:   `{"code":-1003,"msg":"Too many requests queued."}`,
			wantErrMessage: "binance: request failed with status 429: Too many requests queued.",
		},
		{
			name:           "invalid json",
			statusCode:     http.StatusOK,
			responseBody:   `[`,
			wantErrMessage: "binance: decode klines response: unexpected end of JSON input",
		},
		{
			name:           "malformed kline",
			statusCode:     http.StatusOK,
			responseBody:   `[[1704067200000,"42000.1"]]`,
			wantErrMessage: "binance: decode klines response: kline 0 has 2 fields, want at least 6",
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

			_, err := provider.GetOHLCV(context.Background(), "BTCUSDT", data.Timeframe1d, from, to)
			if err == nil {
				t.Fatal("GetOHLCV() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErrMessage {
				t.Fatalf("GetOHLCV() error = %q, want %q", err.Error(), tt.wantErrMessage)
			}
		})
	}
}

func TestProviderGetOHLCVRespectsRateLimiter(t *testing.T) {
	t.Parallel()

	serverHits := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		serverHits++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer server.Close()

	provider := NewProvider(discardLogger())
	provider.baseURL = server.URL
	provider.httpClient = server.Client()
	provider.rateLimiter = data.NewRateLimiter(1, time.Hour)

	if !provider.rateLimiter.TryAcquire() {
		t.Fatal("TryAcquire() = false, want true for initial token")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	_, err := provider.GetOHLCV(ctx, "BTCUSDT", data.Timeframe1m, time.Now().UTC(), time.Now().UTC())
	if err == nil {
		t.Fatal("GetOHLCV() error = nil, want non-nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("GetOHLCV() error = %v, want context deadline exceeded", err)
	}
	if serverHits != 0 {
		t.Fatalf("server hit count = %d, want %d", serverHits, 0)
	}
}

func TestProviderUnsupportedMethodsReturnErrNotImplemented(t *testing.T) {
	t.Parallel()

	provider := NewProvider(discardLogger())

	_, fundamentalsErr := provider.GetFundamentals(context.Background(), "BTCUSDT")
	if !errors.Is(fundamentalsErr, data.ErrNotImplemented) {
		t.Fatalf("GetFundamentals() error = %v, want ErrNotImplemented", fundamentalsErr)
	}

	_, newsErr := provider.GetNews(context.Background(), "BTCUSDT", time.Now(), time.Now())
	if !errors.Is(newsErr, data.ErrNotImplemented) {
		t.Fatalf("GetNews() error = %v, want ErrNotImplemented", newsErr)
	}

	_, socialErr := provider.GetSocialSentiment(context.Background(), "BTCUSDT")
	if !errors.Is(socialErr, data.ErrNotImplemented) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNotImplemented", socialErr)
	}
}

func marshalKlines(start time.Time, count int, step time.Duration) ([]byte, error) {
	rows := make([][]any, 0, count)
	for i := 0; i < count; i++ {
		timestamp := start.Add(time.Duration(i) * step).UnixMilli()
		price := 42000.0 + float64(i)
		rows = append(rows, []any{
			timestamp,
			fmt.Sprintf("%.2f", price),
			fmt.Sprintf("%.2f", price+10),
			fmt.Sprintf("%.2f", price-10),
			fmt.Sprintf("%.2f", price+5),
			fmt.Sprintf("%.2f", 1.0+float64(i)/100),
			timestamp + step.Milliseconds() - 1,
			"0",
			0,
			"0",
			"0",
			"0",
		})
	}

	return json.Marshal(rows)
}
