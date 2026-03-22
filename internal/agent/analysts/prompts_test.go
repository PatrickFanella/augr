package analysts

import (
	"strings"
	"testing"
	"time"

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

	if strings.Contains(result, "BAD|TICK") {
		t.Error("pipe characters in ticker should be escaped")
	}
	if strings.Contains(result, "\n") && strings.Contains(result, "BAD") && strings.Contains(result, "ER") {
		// Check that the newline between BAD and ER was replaced.
		if !strings.Contains(result, `BAD\|TICK ER`) {
			t.Error("newlines and pipes in ticker should be sanitized")
		}
	}
}

func TestFormatMarketAnalystUserPromptSanitizesIndicatorName(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	indicators := []domain.Indicator{
		{Name: "evil|name\nbreaker", Value: 42.0, Timestamp: ts},
	}

	result := FormatMarketAnalystUserPrompt("TEST", nil, indicators)

	if strings.Contains(result, "evil|name") {
		t.Error("pipe in indicator name should be escaped")
	}
	if !strings.Contains(result, `evil\|name breaker`) {
		t.Error("indicator name should have pipes escaped and newlines replaced")
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
