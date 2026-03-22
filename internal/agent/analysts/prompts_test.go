package analysts

import (
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
)

func TestMarketAnalystSystemPromptIsNonEmpty(t *testing.T) {
	if MarketAnalystSystemPrompt == "" {
		t.Fatal("MarketAnalystSystemPrompt must not be empty")
	}
}

func TestMarketAnalystSystemPromptContainsRequiredSections(t *testing.T) {
	required := []string{
		"Trend",
		"Momentum",
		"Volatility",
		"Volume",
		"Overall Assessment",
		"SMA",
		"RSI",
		"MACD",
		"Bollinger Band",
		"OBV",
		"ADL",
		"Stochastic",
		"Williams %R",
		"CCI",
		"ROC",
		"MFI",
		"ATR",
		"VWMA",
		"bullish",
		"bearish",
		"confidence",
	}
	for _, keyword := range required {
		if !strings.Contains(MarketAnalystSystemPrompt, keyword) {
			t.Errorf("system prompt missing required keyword %q", keyword)
		}
	}
}

func TestFormatMarketAnalystUserPromptWithData(t *testing.T) {
	ts := time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		{Timestamp: ts, Open: 100.50, High: 105.25, Low: 99.75, Close: 103.00, Volume: 1500000},
		{Timestamp: ts.AddDate(0, 0, 1), Open: 103.00, High: 107.00, Low: 102.00, Close: 106.50, Volume: 1800000},
	}
	indicators := []domain.Indicator{
		{Name: "SMA_20", Value: 101.5, Timestamp: ts},
		{Name: "RSI_14", Value: 65.3, Timestamp: ts},
	}

	result := FormatMarketAnalystUserPrompt("AAPL", bars, indicators)

	checks := []string{
		"AAPL",
		"## OHLCV Data",
		"## Technical Indicators",
		"100.50",
		"105.25",
		"99.75",
		"103.00",
		"1500000",
		"2025-03-20",
		"2025-03-21",
		"SMA_20",
		"RSI_14",
		"101.5000",
		"65.3000",
		"Provide your structured technical analysis report.",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("user prompt missing expected content %q", want)
		}
	}
}

func TestFormatMarketAnalystUserPromptEmptyData(t *testing.T) {
	result := FormatMarketAnalystUserPrompt("TSLA", nil, nil)

	if !strings.Contains(result, "TSLA") {
		t.Error("user prompt should contain ticker")
	}
	if !strings.Contains(result, "No OHLCV data available.") {
		t.Error("user prompt should indicate missing OHLCV data")
	}
	if !strings.Contains(result, "No indicator data available.") {
		t.Error("user prompt should indicate missing indicator data")
	}
}

func TestFormatMarketAnalystUserPromptEmptyBarsWithIndicators(t *testing.T) {
	ts := time.Date(2025, 1, 15, 0, 0, 0, 0, time.UTC)
	indicators := []domain.Indicator{
		{Name: "MACD", Value: 1.25, Timestamp: ts},
	}

	result := FormatMarketAnalystUserPrompt("GOOG", nil, indicators)

	if !strings.Contains(result, "No OHLCV data available.") {
		t.Error("user prompt should indicate missing OHLCV data")
	}
	if !strings.Contains(result, "MACD") {
		t.Error("user prompt should contain indicator name")
	}
	if !strings.Contains(result, "1.2500") {
		t.Error("user prompt should contain indicator value")
	}
}

func TestFormatMarketAnalystUserPromptBarsWithoutIndicators(t *testing.T) {
	ts := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	bars := []domain.OHLCV{
		{Timestamp: ts, Open: 50.0, High: 55.0, Low: 49.0, Close: 54.0, Volume: 500000},
	}

	result := FormatMarketAnalystUserPrompt("MSFT", bars, nil)

	if !strings.Contains(result, "50.00") {
		t.Error("user prompt should contain OHLCV open value")
	}
	if !strings.Contains(result, "No indicator data available.") {
		t.Error("user prompt should indicate missing indicator data")
	}
}

func TestFormatMarketAnalystUserPromptIntradayTimestamps(t *testing.T) {
	bars := []domain.OHLCV{
		{Timestamp: time.Date(2025, 3, 20, 9, 30, 0, 0, time.UTC), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 5000},
		{Timestamp: time.Date(2025, 3, 20, 9, 35, 0, 0, time.UTC), Open: 100.5, High: 102, Low: 100, Close: 101.5, Volume: 6000},
	}
	indicators := []domain.Indicator{
		{Name: "RSI_14", Value: 55.0, Timestamp: time.Date(2025, 3, 20, 9, 35, 0, 0, time.UTC)},
	}

	result := FormatMarketAnalystUserPrompt("SPY", bars, indicators)

	// Intraday bars should include time-of-day.
	if !strings.Contains(result, "2025-03-20 09:30 UTC") {
		t.Error("intraday bars should include time-of-day")
	}
	if !strings.Contains(result, "2025-03-20 09:35 UTC") {
		t.Error("intraday bars should include time-of-day for second bar")
	}
	// Intraday indicator timestamp.
	if !strings.Contains(result, "09:35 UTC") {
		t.Error("intraday indicator should include time-of-day")
	}
}

func TestFormatMarketAnalystUserPromptMixedTimestamps(t *testing.T) {
	// One midnight, one intraday bar — should trigger intraday formatting.
	bars := []domain.OHLCV{
		{Timestamp: time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC), Open: 100, High: 101, Low: 99, Close: 100.5, Volume: 5000},
		{Timestamp: time.Date(2025, 3, 20, 14, 0, 0, 0, time.UTC), Open: 100.5, High: 102, Low: 100, Close: 101.5, Volume: 6000},
	}

	result := FormatMarketAnalystUserPrompt("QQQ", bars, nil)

	if !strings.Contains(result, "2025-03-20 00:00 UTC") {
		t.Error("mixed series should use full timestamp format for midnight bar")
	}
	if !strings.Contains(result, "2025-03-20 14:00 UTC") {
		t.Error("mixed series should use full timestamp format for intraday bar")
	}
}

func TestFormatMarketAnalystUserPromptSanitizesTicker(t *testing.T) {
	result := FormatMarketAnalystUserPrompt("BAD|TICK\nER", nil, nil)

	if !strings.Contains(result, `BAD\|TICK ER`) {
		t.Error("ticker should have pipes escaped and newlines replaced")
	}
}

func TestFormatMarketAnalystUserPromptSanitizesIndicatorName(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	indicators := []domain.Indicator{
		{Name: "evil|name\nbreaker", Value: 42.0, Timestamp: ts},
	}

	result := FormatMarketAnalystUserPrompt("TEST", nil, indicators)

	if !strings.Contains(result, `evil\|name breaker`) {
		t.Error("indicator name should have pipes escaped and newlines replaced")
	}
}

// ---------------------------------------------------------------------------
// Fundamentals analyst prompt tests
// ---------------------------------------------------------------------------

func TestFundamentalsAnalystSystemPromptIsNonEmpty(t *testing.T) {
	if FundamentalsAnalystSystemPrompt == "" {
		t.Fatal("FundamentalsAnalystSystemPrompt must not be empty")
	}
}

func TestFundamentalsAnalystSystemPromptContainsRequiredSections(t *testing.T) {
	required := []string{
		"Valuation",
		"Growth",
		"Financial Health",
		"Dividend",
		"P/E Ratio",
		"Market Capitalization",
		"Revenue Growth",
		"EPS",
		"Debt-to-Equity",
		"Free Cash Flow",
		"Gross Margin",
		"Dividend Yield",
		"Overall Fundamental Rating",
		"strong buy",
		"sell",
		"confidence",
		"not applicable",
	}
	for _, keyword := range required {
		if !strings.Contains(FundamentalsAnalystSystemPrompt, keyword) {
			t.Errorf("system prompt missing required keyword %q", keyword)
		}
	}
}

func TestFormatFundamentalsAnalystUserPromptWithData(t *testing.T) {
	f := &data.Fundamentals{
		Ticker:           "AAPL",
		MarketCap:        2800000000000,
		PERatio:          28.5,
		EPS:              6.15,
		Revenue:          394000000000,
		RevenueGrowthYoY: 0.08,
		GrossMargin:      0.438,
		DebtToEquity:     1.87,
		FreeCashFlow:     111000000000,
		DividendYield:    0.005,
		FetchedAt:        time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC),
	}

	result := FormatFundamentalsAnalystUserPrompt("AAPL", f)

	checks := []string{
		"AAPL",
		"## Fundamental Data",
		"2800000000000.00",
		"28.50",
		"6.15",
		"394000000000.00",
		"8.00%",
		"43.80%",
		"1.87",
		"111000000000.00",
		"0.50%",
		"2025-03-20",
		"Provide your structured fundamental analysis report.",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("user prompt missing expected content %q", want)
		}
	}
}

func TestFormatFundamentalsAnalystUserPromptNilFundamentals(t *testing.T) {
	result := FormatFundamentalsAnalystUserPrompt("BTC-USD", nil)

	if !strings.Contains(result, "BTC-USD") {
		t.Error("user prompt should contain ticker")
	}
	if !strings.Contains(result, "No fundamental data available") {
		t.Error("user prompt should indicate missing fundamental data")
	}
	if !strings.Contains(result, "not applicable") {
		t.Error("user prompt should mention that metrics are not applicable")
	}
	if strings.Contains(result, "| Metric | Value |") {
		t.Error("user prompt should not contain the data table when fundamentals are nil")
	}
}

func TestFormatFundamentalsAnalystUserPromptZeroValues(t *testing.T) {
	f := &data.Fundamentals{
		Ticker:    "PENNY",
		FetchedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	result := FormatFundamentalsAnalystUserPrompt("PENNY", f)

	// Zero values should still be formatted in the table.
	if !strings.Contains(result, "| P/E Ratio | 0.00 |") {
		t.Error("user prompt should contain zero P/E ratio")
	}
	if !strings.Contains(result, "| Dividend Yield | 0.00% |") {
		t.Error("user prompt should contain zero dividend yield")
	}
	if !strings.Contains(result, "2025-06-01") {
		t.Error("user prompt should contain fetched-at date")
	}
}

func TestFormatFundamentalsAnalystUserPromptSanitizesTicker(t *testing.T) {
	result := FormatFundamentalsAnalystUserPrompt("BAD|TICK\nER", nil)

	if !strings.Contains(result, `BAD\|TICK ER`) {
		t.Error("ticker should have pipes escaped and newlines replaced")
	}
}

// ---------------------------------------------------------------------------
// Social analyst prompt tests
// ---------------------------------------------------------------------------

func TestSocialAnalystSystemPromptIsNonEmpty(t *testing.T) {
	if SocialAnalystSystemPrompt == "" {
		t.Fatal("SocialAnalystSystemPrompt must not be empty")
	}
}

func TestSocialAnalystSystemPromptContainsRequiredSections(t *testing.T) {
	required := []string{
		"Sentiment Score",
		"Bullish",
		"Bearish",
		"Post count",
		"Comment count",
		"Retail Sentiment Summary",
		"Trending Assessment",
		"Contrarian Signals",
		"Overall Assessment",
		"bullish",
		"bearish",
		"confidence",
		"engagement",
	}
	for _, keyword := range required {
		if !strings.Contains(SocialAnalystSystemPrompt, keyword) {
			t.Errorf("SocialAnalystSystemPrompt missing required keyword %q", keyword)
		}
	}
}

// News analyst prompt tests
// ---------------------------------------------------------------------------

func TestNewsAnalystSystemPromptIsNonEmpty(t *testing.T) {
	if NewsAnalystSystemPrompt == "" {
		t.Fatal("NewsAnalystSystemPrompt must not be empty")
	}
}

func TestNewsAnalystSystemPromptContainsRequiredSections(t *testing.T) {
	required := []string{
		"Sentiment",
		"Catalyst",
		"Macro",
		"Risk",
		"Overall Assessment",
		"bullish",
		"bearish",
		"neutral",
		"earnings",
		"regulatory",
		"confidence",
		"Product",
		"M&A",
		"Management",
	}
	for _, keyword := range required {
		if !strings.Contains(NewsAnalystSystemPrompt, keyword) {
			t.Errorf("system prompt missing required keyword %q", keyword)
		}
	}
}

func TestFormatSocialAnalystUserPromptWithData(t *testing.T) {
	s := &data.SocialSentiment{
		Ticker:       "GME",
		Score:        0.7523,
		Bullish:      0.82,
		Bearish:      0.18,
		PostCount:    15420,
		CommentCount: 87300,
		MeasuredAt:   time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC),
	}

	result := FormatSocialAnalystUserPrompt("GME", s)

	checks := []string{
		"GME",
		"## Social Sentiment Data",
		"0.7523",
		"0.8200",
		"0.1800",
		"15420",
		"87300",
		"2025-03-20",
		"Provide your structured social sentiment analysis report.",
	}
	for _, check := range checks {
		if !strings.Contains(result, check) {
			t.Errorf("FormatSocialAnalystUserPrompt() missing expected content %q", check)
		}
	}
}

func TestFormatNewsAnalystUserPromptWithData(t *testing.T) {
	articles := []data.NewsArticle{
		{
			Title:       "AAPL beats earnings expectations",
			Summary:     "Apple reported Q1 earnings above analyst estimates.",
			URL:         "https://example.com/1",
			Source:      "Reuters",
			PublishedAt: time.Date(2025, 3, 20, 0, 0, 0, 0, time.UTC),
			Sentiment:   0.85,
		},
		{
			Title:       "AAPL faces regulatory scrutiny",
			Summary:     "EU announces antitrust investigation into Apple.",
			URL:         "https://example.com/2",
			Source:      "Bloomberg",
			PublishedAt: time.Date(2025, 3, 19, 0, 0, 0, 0, time.UTC),
			Sentiment:   -0.60,
		},
	}

	result := FormatNewsAnalystUserPrompt("AAPL", articles)

	checks := []string{
		"AAPL",
		"## News Articles",
		"AAPL beats earnings expectations",
		"Apple reported Q1 earnings above analyst estimates.",
		"Reuters",
		"0.85",
		"2025-03-20",
		"AAPL faces regulatory scrutiny",
		"EU announces antitrust investigation into Apple.",
		"Bloomberg",
		"-0.60",
		"2025-03-19",
		"Provide your structured news analysis report.",
	}
	for _, want := range checks {
		if !strings.Contains(result, want) {
			t.Errorf("user prompt missing expected content %q", want)
		}
	}
}

func TestFormatSocialAnalystUserPromptNilSentiment(t *testing.T) {
	result := FormatSocialAnalystUserPrompt("DOGE-USD", nil)

	if !strings.Contains(result, "DOGE-USD") {
		t.Error("user prompt should contain ticker")
	}
	if !strings.Contains(result, "No social sentiment data available") {
		t.Error("user prompt should indicate missing social sentiment data")
	}
	if strings.Contains(result, "| Metric | Value |") {
		t.Error("user prompt should not contain the data table when sentiment is nil")
	}
}

func TestFormatSocialAnalystUserPromptZeroValues(t *testing.T) {
	s := &data.SocialSentiment{
		Ticker:     "NEWCO",
		MeasuredAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	result := FormatSocialAnalystUserPrompt("NEWCO", s)

	if !strings.Contains(result, "| Sentiment Score | 0.0000 |") {
		t.Error("user prompt should contain zero sentiment score")
	}
	if !strings.Contains(result, "| Post Count | 0 |") {
		t.Error("user prompt should contain zero post count")
	}
	if !strings.Contains(result, "2025-06-01") {
		t.Error("user prompt should contain measured-at date")
	}
}

func TestFormatSocialAnalystUserPromptSanitizesTicker(t *testing.T) {
	result := FormatSocialAnalystUserPrompt("BAD|TICK\nER", nil)

	if !strings.Contains(result, `BAD\|TICK ER`) {
		t.Error("ticker should have pipes escaped and newlines replaced")
	}
}

func TestFormatNewsAnalystUserPromptEmptyArticles(t *testing.T) {
	result := FormatNewsAnalystUserPrompt("TSLA", nil)

	if !strings.Contains(result, "TSLA") {
		t.Error("user prompt should contain ticker")
	}
	if !strings.Contains(result, "No news articles available.") {
		t.Error("user prompt should indicate missing news data")
	}
	if strings.Contains(result, "| Date | Title |") {
		t.Error("user prompt should not contain the data table when articles are nil")
	}
}

func TestFormatNewsAnalystUserPromptEmptySlice(t *testing.T) {
	result := FormatNewsAnalystUserPrompt("GOOG", []data.NewsArticle{})

	if !strings.Contains(result, "No news articles available.") {
		t.Error("user prompt should indicate missing news data for empty slice")
	}
}

func TestFormatNewsAnalystUserPromptSanitizesTicker(t *testing.T) {
	result := FormatNewsAnalystUserPrompt("BAD|TICK\nER", nil)

	if !strings.Contains(result, `BAD\|TICK ER`) {
		t.Error("ticker should have pipes escaped and newlines replaced")
	}
}

func TestFormatNewsAnalystUserPromptSanitizesTitleAndSummary(t *testing.T) {
	articles := []data.NewsArticle{
		{
			Title:       "evil|title\nbreaker",
			Summary:     "bad|summary\r\ninjection",
			PublishedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Sentiment:   0.0,
		},
	}

	result := FormatNewsAnalystUserPrompt("TEST", articles)

	if !strings.Contains(result, `evil\|title breaker`) {
		t.Error("title should have pipes escaped and newlines replaced")
	}
	if !strings.Contains(result, `bad\|summary injection`) {
		t.Error("summary should have pipes escaped and newlines replaced")
	}
}

func TestSanitizeCell(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain", "SMA_20", "SMA_20"},
		{"pipe", "a|b", `a\|b`},
		{"newline", "a\nb", "a b"},
		{"carriage return", "a\rb", "a b"},
		{"crlf", "a\r\nb", "a b"},
		{"leading space", "  padded  ", "padded"},
		{"combined", " evil|name\r\nbreaker ", `evil\|name breaker`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeCell(tc.input)
			if got != tc.want {
				t.Errorf("sanitizeCell(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}
