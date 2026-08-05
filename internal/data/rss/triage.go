package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
	"github.com/PatrickFanella/get-rich-quick/internal/llm/parse"
)

// TriageResult is the LLM's analysis of a news headline.
type TriageResult struct {
	Tickers   []string `json:"tickers"`   // mentioned or affected tickers
	Category  string   `json:"category"`  // earnings, macro, sector, company, geopolitical, other
	Sentiment string   `json:"sentiment"` // bullish, bearish, neutral
	Relevance float64  `json:"relevance"` // 0-1 how relevant to equity/options trading
	Summary   string   `json:"summary"`   // one-line summary
}

const triageSystemPrompt = `You are a financial news classifier. For the provided batch of news headlines and descriptions, output a JSON object in this exact shape:

{
  "results": [
    {
      "tickers": ["AAPL", "MSFT"],
      "category": "company",
      "sentiment": "bearish",
      "relevance": 0.8,
      "summary": "Apple faces antitrust ruling"
    }
  ]
}

Rules:
- Return one object in results for each headline, in the same order as the input headlines.
- tickers: extract any stock tickers mentioned or clearly affected. Use standard symbols (AAPL not Apple). Empty array if none.
- category: one of "earnings", "macro", "sector", "company", "geopolitical", "other"
- sentiment: "bullish", "bearish", or "neutral" for the market/mentioned stocks
- relevance: 0-1 how relevant this is to stock/options trading. Personal finance articles = 0.1, Fed rate decisions = 0.9
- summary: one sentence, max 100 chars

Respond with ONLY the JSON object.`

// Triage runs LLM classification on a batch of articles.
// Uses the quick/small model for speed. Returns a map of GUID → TriageResult.
func Triage(ctx context.Context, provider llm.Provider, model string, articles []Article, logger *slog.Logger) map[string]*TriageResult {
	if provider == nil || len(articles) == 0 {
		return nil
	}
	if logger == nil {
		logger = slog.Default()
	}

	results := make(map[string]*TriageResult, len(articles))

	// Batch headlines into groups of 10 for efficiency.
	const batchSize = 10
	var totalBatchDuration time.Duration
	var completedBatches int

	for i := 0; i < len(articles); i += batchSize {
		// Budget check: skip if remaining deadline < 1.5× avg batch time.
		if completedBatches > 0 {
			if deadline, ok := ctx.Deadline(); ok {
				avgPerBatch := totalBatchDuration / time.Duration(completedBatches)
				if remaining := time.Until(deadline); remaining < time.Duration(float64(avgPerBatch)*1.5) {
					logger.Warn("rss/triage: skipping remaining batches, budget exhausted",
						slog.Int("completed", completedBatches),
						slog.Duration("remaining", remaining),
						slog.Duration("avg_per_batch", avgPerBatch),
					)
					break
				}
			}
		}

		end := i + batchSize
		if end > len(articles) {
			end = len(articles)
		}
		batch := articles[i:end]

		start := time.Now()
		batchResults := triageBatch(ctx, provider, model, batch, logger)
		totalBatchDuration += time.Since(start)
		completedBatches++

		for k, v := range batchResults {
			results[k] = v
		}
	}

	return results
}

func triageBatch(ctx context.Context, provider llm.Provider, model string, batch []Article, logger *slog.Logger) map[string]*TriageResult {
	results := make(map[string]*TriageResult, len(batch))

	// Build a numbered list of headlines for the LLM.
	var sb strings.Builder
	sb.WriteString("Classify each headline. Return a JSON object with a top-level \"results\" array containing one object per headline, in order.\n\n")
	for i, art := range batch {
		fmt.Fprintf(&sb, "%d. [%s] %s\n", i+1, art.Source, art.Title)
		if art.Description != "" {
			desc := art.Description
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}
			fmt.Fprintf(&sb, "   %s\n", desc)
		}
	}

	request := llm.CompletionRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: triageSystemPrompt},
			{Role: "user", Content: sb.String()},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	}

	var triageResults []TriageResult
	for attempt := 1; attempt <= 2; attempt++ {
		resp, err := provider.Complete(ctx, request)
		if err != nil {
			logger.Warn("rss/triage: LLM call failed", slog.Any("error", err))
			return results
		}

		content := normalizeTriageContent(resp.Content)
		parsed, parseErr := parseTriageResults(content)
		if parseErr == nil && len(parsed) != len(batch) {
			parseErr = fmt.Errorf("triage result count %d does not match batch size %d", len(parsed), len(batch))
		}
		if parseErr != nil {
			retryable := isRetryableTriageParseError(content, parseErr)
			if attempt == 1 && retryable {
				continue
			}
			logger.Warn("rss/triage: failed to parse LLM response",
				slog.Any("error", parseErr),
				slog.String("content", content[:min(200, len(content))]),
			)
			if retryable && len(batch) > 1 {
				return synthesizeNeutralTriageFallback(batch)
			}
			return results
		}
		triageResults = parsed
		break
	}

	for i, tr := range triageResults {
		if i >= len(batch) {
			break
		}
		tr := tr
		key := batch[i].GUID
		if key == "" {
			key = batch[i].Link
		}
		results[key] = &tr
	}

	return results
}

func normalizeTriageContent(content string) string {
	content = strings.TrimSpace(content)

	// Strip markdown fences if present (common with local models).
	if strings.HasPrefix(content, "```") {
		if idx := strings.Index(content[3:], "\n"); idx >= 0 {
			content = content[3+idx+1:]
		}
		if idx := strings.LastIndex(content, "```"); idx >= 0 {
			content = content[:idx]
		}
		content = strings.TrimSpace(content)
	}

	return content
}

func parseTriageResults(content string) ([]TriageResult, error) {
	var wrapper struct {
		Results []TriageResult `json:"results"`
	}
	extracted, err := parse.ExtractJSONObject(content)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(extracted), &wrapper); err != nil {
		return nil, err
	}
	if len(wrapper.Results) == 0 {
		return nil, fmt.Errorf("triage response contains no results")
	}
	for i := range wrapper.Results {
		normalizeTriageResult(&wrapper.Results[i])
		if err := validateTriageResult(wrapper.Results[i]); err != nil {
			return nil, fmt.Errorf("triage result %d: %w", i+1, err)
		}
	}
	return wrapper.Results, nil
}

func normalizeTriageResult(result *TriageResult) {
	if result == nil {
		return
	}
	result.Category = strings.ToLower(strings.TrimSpace(result.Category))
	result.Sentiment = strings.ToLower(strings.TrimSpace(result.Sentiment))
	result.Summary = strings.TrimSpace(result.Summary)
	for i := range result.Tickers {
		result.Tickers[i] = strings.ToUpper(strings.TrimSpace(result.Tickers[i]))
	}
}

func validateTriageResult(result TriageResult) error {
	switch result.Category {
	case "earnings", "macro", "sector", "company", "geopolitical", "other":
	default:
		return fmt.Errorf("invalid category %q", result.Category)
	}
	switch result.Sentiment {
	case "bullish", "bearish", "neutral":
	default:
		return fmt.Errorf("invalid sentiment %q", result.Sentiment)
	}
	if result.Relevance < 0 || result.Relevance > 1 {
		return fmt.Errorf("relevance %.3f outside [0,1]", result.Relevance)
	}
	if result.Summary == "" {
		return fmt.Errorf("summary is required")
	}
	return nil
}

func isRetryableTriageParseError(content string, err error) bool {
	if err == nil {
		return false
	}
	if strings.TrimSpace(content) == "" {
		return true
	}
	errText := strings.ToLower(err.Error())
	return strings.Contains(errText, "unexpected end of json input")
}

func synthesizeNeutralTriageFallback(batch []Article) map[string]*TriageResult {
	results := make(map[string]*TriageResult, len(batch))
	for _, art := range batch {
		key := art.GUID
		if key == "" {
			key = art.Link
		}
		results[key] = &TriageResult{
			Tickers:   []string{},
			Category:  "other",
			Sentiment: "neutral",
			Relevance: 0,
			Summary:   fallbackTriageSummary(art.Title),
		}
	}
	return results
}

func fallbackTriageSummary(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "LLM triage unavailable"
	}
	if len(title) > 100 {
		return title[:100]
	}
	return title
}
