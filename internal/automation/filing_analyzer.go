package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const filingAnalysisSystemPrompt = `You are a senior financial analyst. A new SEC filing has been detected for a stock in our portfolio. Analyze the filing excerpt and assess its impact on our trading strategy.

Respond with JSON only:
{
  "sentiment": "bullish" | "bearish" | "neutral",
  "impact": "high" | "medium" | "low",
  "summary": "<2-3 sentence summary of the key findings>",
  "action": "hold" | "increase_position" | "reduce_position" | "close_position" | "no_change",
  "confidence": <float 0.0-1.0>,
  "key_items": ["<list of material items found>"],
  "reasoning": "<why you recommend this action>"
}`

const (
	filingMaxTextLen   = 15000
	filingUserAgent    = "get-rich-quick admin@example.com"
	filingFetchTimeout = 15 * time.Second
)

// htmlTagRe strips HTML tags from fetched filing text.
var htmlTagRe = regexp.MustCompile(`<[^>]*>`)

// FilingAnalysis is the structured result of an LLM analysis of an SEC filing.
type FilingAnalysis struct {
	Symbol     string    `json:"symbol"`
	Form       string    `json:"form"`
	FiledDate  time.Time `json:"filed_date"`
	Sentiment  string    `json:"sentiment"`
	Impact     string    `json:"impact"`
	Summary    string    `json:"summary"`
	Action     string    `json:"action"`
	Confidence float64   `json:"confidence"`
	KeyItems   []string  `json:"key_items"`
	Reasoning  string    `json:"reasoning"`
}

// AnalyzeFiling fetches the filing document from SEC and asks the LLM to analyze it.
func AnalyzeFiling(ctx context.Context, provider llm.Provider, model string, filing domain.SECFiling, strategyName string, logger *slog.Logger) (*FilingAnalysis, error) {
	if provider == nil {
		return nil, fmt.Errorf("filing_analyzer: LLM provider is nil")
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Fetch filing text from SEC.
	text, err := fetchFilingText(ctx, filing.URL)
	if err != nil {
		logger.Warn("filing_analyzer: failed to fetch filing text, returning neutral analysis",
			slog.String("url", filing.URL),
			slog.Any("error", err),
		)
		return neutralAnalysis(filing), nil
	}

	// Truncate to fit in context window.
	if len(text) > filingMaxTextLen {
		text = text[:filingMaxTextLen]
	}

	userPrompt := buildFilingPrompt(filing, strategyName, text)

	resp, err := provider.Complete(ctx, llm.CompletionRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: filingAnalysisSystemPrompt},
			{Role: "user", Content: userPrompt},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		logger.Warn("filing_analyzer: LLM call failed, returning neutral analysis",
			slog.String("symbol", filing.Symbol),
			slog.Any("error", err),
		)
		return neutralAnalysis(filing), nil
	}

	var analysis FilingAnalysis
	if err := json.Unmarshal([]byte(resp.Content), &analysis); err != nil {
		logger.Warn("filing_analyzer: failed to parse LLM response, returning neutral analysis",
			slog.String("content", resp.Content),
			slog.Any("error", err),
		)
		return neutralAnalysis(filing), nil
	}

	// Fill in metadata from the filing itself.
	analysis.Symbol = filing.Symbol
	analysis.Form = filing.Form
	analysis.FiledDate = filing.FiledDate

	return &analysis, nil
}

func fetchFilingText(ctx context.Context, url string) (string, error) {
	if url == "" {
		return "", fmt.Errorf("empty filing URL")
	}

	fetchCtx, cancel := context.WithTimeout(ctx, filingFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", filingUserAgent)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch filing: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("filing fetch returned status %d", resp.StatusCode)
	}

	// Read up to filingMaxTextLen*2 raw bytes to account for HTML overhead.
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(filingMaxTextLen*2)))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	// Strip HTML tags.
	text := htmlTagRe.ReplaceAllString(string(body), " ")
	return text, nil
}

func buildFilingPrompt(filing domain.SECFiling, strategyName, text string) string {
	return fmt.Sprintf(`SEC Filing Analysis Request

Ticker: %s
Filing Type: %s
Filed Date: %s
Strategy: %s

Filing Excerpt (first ~15,000 chars):
---
%s
---

Analyze this filing. What are the key material items? How does this affect our trading strategy for %s?`,
		filing.Symbol,
		filing.Form,
		filing.FiledDate.Format("2006-01-02"),
		strategyName,
		text,
		filing.Symbol,
	)
}

func neutralAnalysis(filing domain.SECFiling) *FilingAnalysis {
	return &FilingAnalysis{
		Symbol:     filing.Symbol,
		Form:       filing.Form,
		FiledDate:  filing.FiledDate,
		Sentiment:  "neutral",
		Impact:     "low",
		Summary:    "Unable to analyze filing — returned neutral assessment.",
		Action:     "no_change",
		Confidence: 0.0,
		KeyItems:   []string{},
		Reasoning:  "Analysis could not be completed due to an error.",
	}
}
