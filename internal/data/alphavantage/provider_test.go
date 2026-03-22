package alphavantage

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestProviderGetOHLCV(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method string
		path   string
		query  url.Values
	}

	tests := []struct {
		name         string
		timeframe    data.Timeframe
		from         time.Time
		to           time.Time
		responseBody string
		want         []domain.OHLCV
		wantFunction string
		wantInterval string
	}{
		{
			name:      "intraday response",
			timeframe: data.Timeframe5m,
			from:      time.Date(2024, time.January, 2, 14, 30, 0, 0, time.UTC),
			to:        time.Date(2024, time.January, 2, 14, 35, 0, 0, time.UTC),
			responseBody: `{
				"Meta Data": {
					"1. Information": "Intraday Prices",
					"6. Time Zone": "America/New_York"
				},
				"Time Series (5min)": {
					"2024-01-02 09:40:00": {
						"1. open": "102.00",
						"2. high": "102.10",
						"3. low": "101.90",
						"4. close": "102.05",
						"5. volume": "700"
					},
					"2024-01-02 09:25:00": {
						"1. open": "100.00",
						"2. high": "100.10",
						"3. low": "99.90",
						"4. close": "100.05",
						"5. volume": "500"
					},
					"2024-01-02 09:35:00": {
						"1. open": "101.00",
						"2. high": "101.30",
						"3. low": "100.80",
						"4. close": "101.20",
						"5. volume": "650"
					},
					"2024-01-02 09:30:00": {
						"1. open": "100.50",
						"2. high": "100.90",
						"3. low": "100.40",
						"4. close": "100.80",
						"5. volume": "600"
					}
				}
			}`,
			want: []domain.OHLCV{
				{
					Timestamp: time.Date(2024, time.January, 2, 14, 30, 0, 0, time.UTC),
					Open:      100.50,
					High:      100.90,
					Low:       100.40,
					Close:     100.80,
					Volume:    600,
				},
				{
					Timestamp: time.Date(2024, time.January, 2, 14, 35, 0, 0, time.UTC),
					Open:      101.00,
					High:      101.30,
					Low:       100.80,
					Close:     101.20,
					Volume:    650,
				},
			},
			wantFunction: functionTimeSeriesIntraday,
			wantInterval: "5min",
		},
		{
			name:      "daily response",
			timeframe: data.Timeframe1d,
			from:      time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
			to:        time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
			responseBody: `{
				"Meta Data": {
					"1. Information": "Daily Prices",
					"5. Time Zone": "US/Eastern"
				},
				"Time Series (Daily)": {
					"2024-01-03": {
						"1. open": "103.00",
						"2. high": "104.00",
						"3. low": "102.50",
						"4. close": "103.50",
						"5. volume": "1600"
					},
					"2024-01-01": {
						"1. open": "99.00",
						"2. high": "99.50",
						"3. low": "98.00",
						"4. close": "98.75",
						"5. volume": "800"
					},
					"2024-01-02": {
						"1. open": "100.00",
						"2. high": "101.00",
						"3. low": "99.50",
						"4. close": "100.75",
						"5. volume": "1200"
					}
				}
			}`,
			want: []domain.OHLCV{
				{
					Timestamp: time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC),
					Open:      100.00,
					High:      101.00,
					Low:       99.50,
					Close:     100.75,
					Volume:    1200,
				},
				{
					Timestamp: time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC),
					Open:      103.00,
					High:      104.00,
					Low:       102.50,
					Close:     103.50,
					Volume:    1600,
				},
			},
			wantFunction: functionTimeSeriesDaily,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			requests := make(chan requestDetails, 1)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests <- requestDetails{
					method: r.Method,
					path:   r.URL.Path,
					query:  r.URL.Query(),
				}

				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient("test-key", discardLogger())
			client.baseURL = server.URL + "/query"
			client.httpClient = server.Client()

			provider := NewProvider(client)

			got, err := provider.GetOHLCV(context.Background(), "AAPL", tt.timeframe, tt.from, tt.to)
			if err != nil {
				t.Fatalf("GetOHLCV() error = %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("GetOHLCV() = %#v, want %#v", got, tt.want)
			}

			select {
			case request := <-requests:
				if request.method != http.MethodGet {
					t.Fatalf("request method = %s, want %s", request.method, http.MethodGet)
				}
				if request.path != "/query" {
					t.Fatalf("request path = %s, want %s", request.path, "/query")
				}
				if request.query.Get("apikey") != "test-key" {
					t.Fatalf("apikey = %q, want %q", request.query.Get("apikey"), "test-key")
				}
				if request.query.Get("symbol") != "AAPL" {
					t.Fatalf("symbol = %q, want %q", request.query.Get("symbol"), "AAPL")
				}
				if request.query.Get("function") != tt.wantFunction {
					t.Fatalf("function = %q, want %q", request.query.Get("function"), tt.wantFunction)
				}
				if request.query.Get("outputsize") != "full" {
					t.Fatalf("outputsize = %q, want %q", request.query.Get("outputsize"), "full")
				}
				if request.query.Get("interval") != tt.wantInterval {
					t.Fatalf("interval = %q, want %q", request.query.Get("interval"), tt.wantInterval)
				}
			case <-time.After(time.Second):
				t.Fatal("request details were not captured")
			}
		})
	}
}

func TestProviderGetOHLCVErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		responseBody   string
		wantErrMessage string
	}{
		{
			name:           "invalid json",
			responseBody:   `{"Meta Data":`,
			wantErrMessage: "alphavantage: decode time series response: unexpected end of JSON input",
		},
		{
			name:           "missing time series data",
			responseBody:   `{"Meta Data":{"1. Information":"Daily Prices"}}`,
			wantErrMessage: "alphavantage: time series data not found in response",
		},
	}

	from := time.Date(2024, time.January, 2, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 3, 0, 0, 0, 0, time.UTC)

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient("test-key", discardLogger())
			client.baseURL = server.URL + "/query"
			client.httpClient = server.Client()

			provider := NewProvider(client)

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

	provider := NewProvider(&Client{})

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
