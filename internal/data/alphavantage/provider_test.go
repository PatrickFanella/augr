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

func TestProviderGetFundamentals(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method string
		path   string
		query  url.Values
	}

	requests := make(chan requestDetails, 3)
	unexpectedFunctions := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("function") {
		case functionOverview:
			_, _ = w.Write([]byte(`{
				"Symbol": "AAPL",
				"MarketCapitalization": "123456789",
				"PERatio": "28.50",
				"EPS": "6.15",
				"DividendYield": "0.0045"
			}`))
		case functionIncomeStatement:
			_, _ = w.Write([]byte(`{
				"annualReports": [
					{
						"fiscalDateEnding": "2024-09-28",
						"totalRevenue": "2000",
						"grossProfit": "800"
					},
					{
						"fiscalDateEnding": "2023-09-30",
						"totalRevenue": "1600",
						"grossProfit": "640"
					}
				]
			}`))
		case functionBalanceSheet:
			_, _ = w.Write([]byte(`{
				"annualReports": [
					{
						"fiscalDateEnding": "2024-09-28",
						"totalLiabilities": "500",
						"totalShareholderEquity": "1000"
					}
				]
			}`))
		default:
			unexpectedFunctions <- r.URL.Query().Get("function")
			http.Error(w, "unexpected function", http.StatusBadRequest)
			return
		}
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL + "/query"
	client.httpClient = server.Client()

	provider := NewProvider(client)

	start := time.Now().UTC()
	got, err := provider.GetFundamentals(context.Background(), " AAPL ")
	end := time.Now().UTC()
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}

	if got.Ticker != "AAPL" {
		t.Fatalf("Ticker = %q, want %q", got.Ticker, "AAPL")
	}
	if got.MarketCap != 123456789 {
		t.Fatalf("MarketCap = %v, want %v", got.MarketCap, 123456789)
	}
	if got.PERatio != 28.50 {
		t.Fatalf("PERatio = %v, want %v", got.PERatio, 28.50)
	}
	if got.EPS != 6.15 {
		t.Fatalf("EPS = %v, want %v", got.EPS, 6.15)
	}
	if got.DividendYield != 0.0045 {
		t.Fatalf("DividendYield = %v, want %v", got.DividendYield, 0.0045)
	}
	if got.Revenue != 2000 {
		t.Fatalf("Revenue = %v, want %v", got.Revenue, 2000)
	}
	if got.RevenueGrowthYoY != 0.25 {
		t.Fatalf("RevenueGrowthYoY = %v, want %v", got.RevenueGrowthYoY, 0.25)
	}
	if got.GrossMargin != 0.4 {
		t.Fatalf("GrossMargin = %v, want %v", got.GrossMargin, 0.4)
	}
	if got.DebtToEquity != 0.5 {
		t.Fatalf("DebtToEquity = %v, want %v", got.DebtToEquity, 0.5)
	}
	if got.FreeCashFlow != 0 {
		t.Fatalf("FreeCashFlow = %v, want 0", got.FreeCashFlow)
	}
	if got.FetchedAt.Before(start) || got.FetchedAt.After(end) {
		t.Fatalf("FetchedAt = %v, want between %v and %v", got.FetchedAt, start, end)
	}

	wantFunctions := map[string]bool{
		functionOverview:        false,
		functionIncomeStatement: false,
		functionBalanceSheet:    false,
	}

	for i := 0; i < 3; i++ {
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

			function := request.query.Get("function")
			if _, ok := wantFunctions[function]; !ok {
				t.Fatalf("function = %q, want one of overview/income/balance", function)
			}
			wantFunctions[function] = true
		case <-time.After(time.Second):
			t.Fatal("request details were not captured")
		}
	}

	for function, seen := range wantFunctions {
		if !seen {
			t.Fatalf("function %q was not requested", function)
		}
	}

	select {
	case function := <-unexpectedFunctions:
		t.Fatalf("unexpected function %q", function)
	default:
	}
}

func TestProviderGetFundamentalsMissingFieldsGracefully(t *testing.T) {
	t.Parallel()

	unexpectedFunctions := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("function") {
		case functionOverview:
			_, _ = w.Write([]byte(`{
				"Symbol": "",
				"MarketCapitalization": "",
				"PERatio": "N/A",
				"EPS": "",
				"DividendYield": "-"
			}`))
		case functionIncomeStatement:
			_, _ = w.Write([]byte(`{
				"annualReports": [
					{
						"fiscalDateEnding": "2024-09-28",
						"totalRevenue": "",
						"grossProfit": "N/A"
					},
					{
						"fiscalDateEnding": "2023-09-30",
						"totalRevenue": "",
						"grossProfit": ""
					}
				]
			}`))
		case functionBalanceSheet:
			_, _ = w.Write([]byte(`{
				"annualReports": [
					{
						"fiscalDateEnding": "2024-09-28",
						"totalLiabilities": "",
						"totalShareholderEquity": ""
					}
				]
			}`))
		default:
			unexpectedFunctions <- r.URL.Query().Get("function")
			http.Error(w, "unexpected function", http.StatusBadRequest)
			return
		}
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL + "/query"
	client.httpClient = server.Client()

	provider := NewProvider(client)

	got, err := provider.GetFundamentals(context.Background(), "MSFT")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}

	want := data.Fundamentals{
		Ticker:    "MSFT",
		FetchedAt: got.FetchedAt,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetFundamentals() = %#v, want %#v", got, want)
	}
	if got.FetchedAt.IsZero() {
		t.Fatal("FetchedAt is zero, want non-zero")
	}

	select {
	case function := <-unexpectedFunctions:
		t.Fatalf("unexpected function %q", function)
	default:
	}
}

func TestProviderGetFundamentalsFallsBackToQuarterlyReports(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Query().Get("function") {
		case functionOverview:
			_, _ = w.Write([]byte(`{
				"MarketCapitalization": "5000",
				"PERatio": "10.5",
				"EPS": "2.5",
				"DividendYield": "0.01"
			}`))
		case functionIncomeStatement:
			_, _ = w.Write([]byte(`{
				"annualReports": [],
				"quarterlyReports": [
					{
						"fiscalDateEnding": "2024-09-30",
						"totalRevenue": "300",
						"grossProfit": "90"
					},
					{
						"fiscalDateEnding": "2024-06-30",
						"totalRevenue": "240",
						"grossProfit": "72"
					}
				]
			}`))
		case functionBalanceSheet:
			_, _ = w.Write([]byte(`{
				"annualReports": [],
				"quarterlyReports": [
					{
						"fiscalDateEnding": "2024-09-30",
						"totalLiabilities": "150",
						"totalShareholderEquity": "300"
					}
				]
			}`))
		default:
			http.Error(w, "unexpected function", http.StatusBadRequest)
			return
		}
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL + "/query"
	client.httpClient = server.Client()

	provider := NewProvider(client)

	got, err := provider.GetFundamentals(context.Background(), "NVDA")
	if err != nil {
		t.Fatalf("GetFundamentals() error = %v", err)
	}

	if got.Ticker != "NVDA" {
		t.Fatalf("Ticker = %q, want %q", got.Ticker, "NVDA")
	}
	if got.MarketCap != 5000 {
		t.Fatalf("MarketCap = %v, want %v", got.MarketCap, 5000)
	}
	if got.Revenue != 300 {
		t.Fatalf("Revenue = %v, want %v", got.Revenue, 300)
	}
	if got.GrossMargin != 0.3 {
		t.Fatalf("GrossMargin = %v, want %v", got.GrossMargin, 0.3)
	}
	if got.RevenueGrowthYoY != 0.25 {
		t.Fatalf("RevenueGrowthYoY = %v, want %v", got.RevenueGrowthYoY, 0.25)
	}
	if got.DebtToEquity != 0.5 {
		t.Fatalf("DebtToEquity = %v, want %v", got.DebtToEquity, 0.5)
	}
}

func TestProviderGetFundamentalsErrors(t *testing.T) {
	t.Parallel()

	t.Run("invalid overview json", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("function") == functionOverview {
				_, _ = w.Write([]byte(`{"Symbol":`))
				return
			}

			_, _ = w.Write([]byte(`{"annualReports":[]}`))
		}))
		defer server.Close()

		client := NewClient("test-key", discardLogger())
		client.baseURL = server.URL + "/query"
		client.httpClient = server.Client()

		provider := NewProvider(client)

		_, err := provider.GetFundamentals(context.Background(), "AAPL")
		if err == nil {
			t.Fatal("GetFundamentals() error = nil, want non-nil")
		}
		wantErr := "alphavantage: GetFundamentals: decode overview response: unexpected end of JSON input"
		if err.Error() != wantErr {
			t.Fatalf("GetFundamentals() error = %q, want %q", err.Error(), wantErr)
		}
	})

	t.Run("empty ticker", func(t *testing.T) {
		t.Parallel()

		provider := NewProvider(&Client{})

		_, err := provider.GetFundamentals(context.Background(), "   ")
		if err == nil {
			t.Fatal("GetFundamentals() error = nil, want non-nil")
		}
		if err.Error() != "alphavantage: ticker is required" {
			t.Fatalf("GetFundamentals() error = %q, want %q", err.Error(), "alphavantage: ticker is required")
		}
	})
}

func TestProviderGetNews(t *testing.T) {
	t.Parallel()

	type requestDetails struct {
		method string
		path   string
		query  url.Values
	}

	from := time.Date(2024, time.January, 2, 14, 30, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)

	requests := make(chan requestDetails, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- requestDetails{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.Query(),
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"feed": [
				{
					"title": "Apple launches new product",
					"url": "https://example.com/apple-product",
					"time_published": "20240102T143000",
					"summary": "Apple announced a new device.",
					"source": "Reuters",
					"overall_sentiment_score": 0.1,
					"ticker_sentiment": [
						{
							"ticker": "AAPL",
							"ticker_sentiment_score": "0.45"
						}
					]
				},
				{
					"title": "Tech stocks mixed",
					"url": "https://example.com/tech-stocks",
					"time_published": "20240102T150000",
					"summary": "Large-cap technology names traded mixed.",
					"source": "Associated Press",
					"overall_sentiment_score": "-0.2",
					"ticker_sentiment": [
						{
							"ticker": "MSFT",
							"ticker_sentiment_score": "-0.6"
						}
					]
				},
				{
					"title": "Older story",
					"url": "https://example.com/older-story",
					"time_published": "20240102T142959",
					"summary": "Should be filtered out.",
					"source": "Example News",
					"overall_sentiment_score": -0.5
				}
			]
		}`))
	}))
	defer server.Close()

	client := NewClient("test-key", discardLogger())
	client.baseURL = server.URL + "/query"
	client.httpClient = server.Client()

	provider := NewProvider(client)

	got, err := provider.GetNews(context.Background(), "AAPL", from, to)
	if err != nil {
		t.Fatalf("GetNews() error = %v", err)
	}

	want := []data.NewsArticle{
		{
			Title:       "Apple launches new product",
			Summary:     "Apple announced a new device.",
			URL:         "https://example.com/apple-product",
			Source:      "Reuters",
			PublishedAt: from,
			Sentiment:   0.45,
		},
		{
			Title:       "Tech stocks mixed",
			Summary:     "Large-cap technology names traded mixed.",
			URL:         "https://example.com/tech-stocks",
			Source:      "Associated Press",
			PublishedAt: to,
			Sentiment:   -0.2,
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GetNews() = %#v, want %#v", got, want)
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
		if request.query.Get("function") != functionNewsSentiment {
			t.Fatalf("function = %q, want %q", request.query.Get("function"), functionNewsSentiment)
		}
		if request.query.Get("tickers") != "AAPL" {
			t.Fatalf("tickers = %q, want %q", request.query.Get("tickers"), "AAPL")
		}
		if request.query.Get("time_from") != "20240102T143000" {
			t.Fatalf("time_from = %q, want %q", request.query.Get("time_from"), "20240102T143000")
		}
		if request.query.Get("time_to") != "20240102T150000" {
			t.Fatalf("time_to = %q, want %q", request.query.Get("time_to"), "20240102T150000")
		}
		if request.query.Get("sort") != "EARLIEST" {
			t.Fatalf("sort = %q, want %q", request.query.Get("sort"), "EARLIEST")
		}
	case <-time.After(time.Second):
		t.Fatal("request details were not captured")
	}
}

func TestProviderGetNewsErrors(t *testing.T) {
	t.Parallel()

	from := time.Date(2024, time.January, 2, 14, 30, 0, 0, time.UTC)
	to := time.Date(2024, time.January, 2, 15, 0, 0, 0, time.UTC)

	tests := []struct {
		name           string
		responseBody   string
		wantErrMessage string
	}{
		{
			name:           "invalid json",
			responseBody:   `{"feed":`,
			wantErrMessage: "alphavantage: decode news response: unexpected end of JSON input",
		},
		{
			name: "invalid published time",
			responseBody: `{
				"feed": [
					{
						"title": "Bad timestamp",
						"time_published": "not-a-timestamp"
					}
				]
			}`,
			wantErrMessage: "alphavantage: parse news time_published \"not-a-timestamp\": parsing time \"not-a-timestamp\" as \"20060102T150405\": cannot parse \"not-a-timestamp\" as \"2006\"",
		},
	}

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

			_, err := provider.GetNews(context.Background(), "AAPL", from, to)
			if err == nil {
				t.Fatal("GetNews() error = nil, want non-nil")
			}
			if err.Error() != tt.wantErrMessage {
				t.Fatalf("GetNews() error = %q, want %q", err.Error(), tt.wantErrMessage)
			}
		})
	}
}

func TestParseOptionalFloat64(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  float64
	}{
		{name: "empty", value: "", want: 0},
		{name: "spaces", value: "   ", want: 0},
		{name: "na", value: "N/A", want: 0},
		{name: "plain na", value: "NA", want: 0},
		{name: "none", value: "NONE", want: 0},
		{name: "null", value: "NULL", want: 0},
		{name: "dash", value: "-", want: 0},
		{name: "invalid", value: "abc", want: 0},
		{name: "number", value: "123.45", want: 123.45},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := parseOptionalFloat64(tt.value); got != tt.want {
				t.Fatalf("parseOptionalFloat64(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

func TestProviderGetSocialSentimentReturnsErrNotImplemented(t *testing.T) {
	t.Parallel()

	provider := NewProvider(&Client{})

	_, socialErr := provider.GetSocialSentiment(context.Background(), "AAPL")
	if !errors.Is(socialErr, data.ErrNotImplemented) {
		t.Fatalf("GetSocialSentiment() error = %v, want ErrNotImplemented", socialErr)
	}
}
