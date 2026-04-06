package rss

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// TriageResult is the LLM's analysis of a news headline.
type TriageResult struct {
	Tickers   []string `json:"tickers"`   // mentioned or affected tickers
	Category  string   `json:"category"`  // earnings, macro, sector, company, geopolitical, other
	Sentiment string   `json:"sentiment"` // bullish, bearish, neutral
	Relevance float64  `json:"relevance"` // 0-1 how relevant to equity/options trading
	Summary   string   `json:"summary"`   // one-line summary
}

const triageSystemPrompt = `You are a financial news classifier. For each news headline and description, output a JSON object:

{
  "tickers": ["AAPL", "MSFT"],
  "category": "company",
  "sentiment": "bearish",
  "relevance": 0.8,
  "summary": "Apple faces antitrust ruling"
}

Rules:
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
	for i := 0; i < len(articles); i += batchSize {
		end := i + batchSize
		if end > len(articles) {
			end = len(articles)
		}
		batch := articles[i:end]

		batchResults := triageBatch(ctx, provider, model, batch, logger)
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
	sb.WriteString("Classify each headline. Return a JSON array with one object per headline, in order.\n\n")
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

	resp, err := provider.Complete(ctx, llm.CompletionRequest{
		Model: model,
		Messages: []llm.Message{
			{Role: "system", Content: triageSystemPrompt},
			{Role: "user", Content: sb.String()},
		},
		ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
	})
	if err != nil {
		logger.Warn("rss/triage: LLM call failed", slog.Any("error", err))
		return results
	}

	// Try to parse as array first, then as wrapper object.
	var triageResults []TriageResult
	content := strings.TrimSpace(resp.Content)

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

	if err := json.Unmarshal([]byte(content), &triageResults); err != nil {
		// Try wrapper: {"results": [...]}
		var wrapper struct {
			Results []TriageResult `json:"results"`
		}
		if err2 := json.Unmarshal([]byte(content), &wrapper); err2 != nil {
			logger.Warn("rss/triage: failed to parse LLM response",
				slog.Any("error", err),
				slog.String("content", content[:min(200, len(content))]),
			)
			return results
		}
		triageResults = wrapper.Results
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
