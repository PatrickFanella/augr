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
