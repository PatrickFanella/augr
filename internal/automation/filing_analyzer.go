package automation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
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
func AnalyzeFiling(ctx context.Context, provider llm.Provider, model string, filing domain.SECFiling, strategyName string, _ *slog.Logger) (*FilingAnalysis, error) {
	if provider == nil {
		return nil, fmt.Errorf("filing_analyzer: LLM provider is nil")
	}
	// Fetch filing text from SEC.
	text, err := fetchFilingText(ctx, filing.URL)
	if err != nil {
		return nil, fmt.Errorf("filing_analyzer: fetch filing text: %w", err)
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
		return nil, fmt.Errorf("filing_analyzer: LLM call for %s: %w", filing.Symbol, err)
	}
	if resp == nil {
		return nil, fmt.Errorf("filing_analyzer: LLM returned nil response for %s", filing.Symbol)
	}

	var analysis FilingAnalysis
	if err := json.Unmarshal([]byte(resp.Content), &analysis); err != nil {
		return nil, fmt.Errorf("filing_analyzer: parse LLM response for %s: %w", filing.Symbol, err)
	}
	if err := validateFilingAnalysis(analysis); err != nil {
		return nil, fmt.Errorf("filing_analyzer: validate LLM response for %s: %w", filing.Symbol, err)
	}

	// Fill in metadata from the filing itself.
	analysis.Symbol = filing.Symbol
	analysis.Form = filing.Form
	analysis.FiledDate = filing.FiledDate

	return &analysis, nil
}

func validateFilingAnalysis(analysis FilingAnalysis) error {
	if !oneOf(strings.ToLower(strings.TrimSpace(analysis.Sentiment)), "bullish", "bearish", "neutral") {
		return fmt.Errorf("invalid sentiment %q", analysis.Sentiment)
	}
	if !oneOf(strings.ToLower(strings.TrimSpace(analysis.Impact)), "high", "medium", "low") {
		return fmt.Errorf("invalid impact %q", analysis.Impact)
	}
	if !oneOf(strings.ToLower(strings.TrimSpace(analysis.Action)), "hold", "increase_position", "reduce_position", "close_position", "no_change") {
		return fmt.Errorf("invalid action %q", analysis.Action)
	}
	if analysis.Confidence < 0 || analysis.Confidence > 1 {
		return fmt.Errorf("confidence %.4f outside [0,1]", analysis.Confidence)
	}
	if strings.TrimSpace(analysis.Summary) == "" || strings.TrimSpace(analysis.Reasoning) == "" {
		return fmt.Errorf("summary and reasoning are required")
	}
	return nil
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
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
	defer func() { _ = resp.Body.Close() }()

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
