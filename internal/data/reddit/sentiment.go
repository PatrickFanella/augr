package reddit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const (
	sentimentBatchSize   = 10
	sentimentMaxTokens   = 4096
	sentimentConcurrency = 1 // Serialize batches to reduce ollama contention with other consumers.
	sentimentMaxRetries  = 1
	sentimentMaxPosts    = 30 // Cap total posts to keep LLM work within analysis timeout.
)

// SentimentResult aggregates LLM-derived sentiment for a ticker from Reddit posts.
type SentimentResult struct {
	Mentions int
	Bullish  int
	Bearish  int
	Neutral  int
}

// postSentiment is the per-post LLM response.
type postSentiment struct {
	MentionsTicker bool   `json:"mentions_ticker"` // does post reference the target ticker?
	Sentiment      string `json:"sentiment"`       // bullish, bearish, neutral
}

func sentimentSystemPrompt(ticker string) string {
	return fmt.Sprintf(`You are a financial social-media sentiment classifier.
For each Reddit post, determine:
1. Whether it mentions or is clearly about the stock ticker %s (use the symbol, not just the company name).
2. If it does mention the ticker, classify the overall sentiment toward %s as "bullish", "bearish", or "neutral".

Respond with a JSON array. Each element must have:
- "mentions_ticker": true/false
- "sentiment": "bullish" | "bearish" | "neutral" (only meaningful when mentions_ticker is true)

Return ONLY the JSON array.`, ticker, ticker)
}

// ScorePosts runs LLM triage on posts to extract sentiment about a specific ticker.
// Batches are processed sequentially. If the context deadline is approaching and
// the remaining budget is less than the average time per completed batch, remaining
// batches are skipped and partial results are returned. This prevents the last batch
// from timing out mid-request when the GPU is under load.
// Returns aggregated counts.
func ScorePosts(ctx context.Context, provider llm.Provider, model, ticker string, posts []RedditPost, logger *slog.Logger) SentimentResult {
	if provider == nil || len(posts) == 0 {
		return SentimentResult{}
	}
	if logger == nil {
		logger = slog.Default()
	}

	// Cap total posts to keep LLM work within the analysis timeout when using
	// local models. The most recent posts are kept (slice is already ordered by
	// recency from the RSS feed).
	if len(posts) > sentimentMaxPosts {
		logger.Info("reddit/sentiment: capping posts",
			slog.Int("total", len(posts)),
			slog.Int("cap", sentimentMaxPosts),
		)
		posts = posts[:sentimentMaxPosts]
	}

	// Build batches up front.
	var batches [][]RedditPost
	for i := 0; i < len(posts); i += sentimentBatchSize {
		end := i + sentimentBatchSize
		if end > len(posts) {
			end = len(posts)
		}
		batches = append(batches, posts[i:end])
	}

	totalBatches := len(batches)
	var result SentimentResult
	var totalBatchDuration time.Duration
	var completedBatches int

	for i, batch := range batches {
		// Budget check: if we have a deadline and the remaining time is less than
		// the average time per batch so far (with a 1.5× safety margin), skip.
		if completedBatches > 0 {
			if deadline, ok := ctx.Deadline(); ok {
				avgPerBatch := totalBatchDuration / time.Duration(completedBatches)
				remaining := time.Until(deadline)
				if remaining < time.Duration(float64(avgPerBatch)*1.5) {
					logger.Warn("reddit/sentiment: skipping remaining batches, budget exhausted",
						slog.Int("completed", completedBatches),
						slog.Int("skipped", totalBatches-i),
						slog.Duration("remaining", remaining),
						slog.Duration("avg_per_batch", avgPerBatch),
					)
					break
				}
			}
		}

		start := time.Now()
		r := scoreBatch(ctx, provider, model, ticker, batch, i, totalBatches, logger)
		elapsed := time.Since(start)

		result.Mentions += r.Mentions
		result.Bullish += r.Bullish
		result.Bearish += r.Bearish
		result.Neutral += r.Neutral

		totalBatchDuration += elapsed
		completedBatches++
	}

	return result
}

func scoreBatch(ctx context.Context, provider llm.Provider, model, ticker string, batch []RedditPost, batchIdx, totalBatches int, logger *slog.Logger) SentimentResult {
	prompt := buildBatchPrompt(ticker, batch)

	for attempt := 0; attempt <= sentimentMaxRetries; attempt++ {
		sysPrompt := sentimentSystemPrompt(ticker)
		if attempt > 0 {
			// On retry, explicitly instruct the model not to use thinking mode
			// which can cause empty content with Qwen3 models.
			sysPrompt += "\n\nIMPORTANT: Do NOT use <think> tags. Respond directly with the JSON array."
		}

		resp, err := provider.Complete(ctx, llm.CompletionRequest{
			Model: model,
			Messages: []llm.Message{
				{Role: "system", Content: sysPrompt},
				{Role: "user", Content: prompt},
			},
			MaxTokens:      sentimentMaxTokens,
			ResponseFormat: &llm.ResponseFormat{Type: llm.ResponseFormatJSONObject},
		})
		if err != nil {
			logger.Warn("reddit/sentiment: LLM call failed",
				slog.Int("batch", batchIdx+1),
				slog.Int("total_batches", totalBatches),
				slog.Int("attempt", attempt+1),
				slog.Any("error", err),
			)
			return SentimentResult{}
		}

		content := cleanContent(resp.Content)
		if content == "" {
			logger.Warn("reddit/sentiment: empty LLM response, retrying",
				slog.Int("batch", batchIdx+1),
				slog.Int("total_batches", totalBatches),
				slog.Int("attempt", attempt+1),
			)
			continue
		}

		result, ok := parseSentimentResponse(content)
		if !ok {
			logger.Warn("reddit/sentiment: failed to parse LLM response",
				slog.Int("batch", batchIdx+1),
				slog.Int("total_batches", totalBatches),
				slog.Int("attempt", attempt+1),
				slog.String("content", content[:min(200, len(content))]),
			)
			return SentimentResult{}
		}
		return result
	}

	logger.Warn("reddit/sentiment: exhausted retries with empty responses",
		slog.Int("batch", batchIdx+1),
		slog.Int("total_batches", totalBatches),
	)
	return SentimentResult{}
}

func buildBatchPrompt(ticker string, batch []RedditPost) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Classify each post for ticker %s. Return a JSON array with one object per post, in order.\n\n", ticker)
	for i, p := range batch {
		title := p.Title
		body := p.Body
		if len(body) > 300 {
			body = body[:300] + "..."
		}
		fmt.Fprintf(&sb, "%d. [r/%s] %s\n", i+1, p.Subreddit, title)
		if body != "" {
			fmt.Fprintf(&sb, "   %s\n", body)
		}
	}
	return sb.String()
}

func cleanContent(raw string) string {
	content := strings.TrimSpace(raw)
	// Strip markdown fences if present.
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

func parseSentimentResponse(content string) (SentimentResult, bool) {
	content = extractJSONArray(content)
	var sentiments []postSentiment
	if err := json.Unmarshal([]byte(content), &sentiments); err != nil {
		// Try wrapper: {"results": [...]}
		var wrapper struct {
			Results []postSentiment `json:"results"`
		}
		if err2 := json.Unmarshal([]byte(content), &wrapper); err2 != nil {
			return SentimentResult{}, false
		}
		sentiments = wrapper.Results
	}

	var result SentimentResult
	for _, s := range sentiments {
		if !s.MentionsTicker {
			continue
		}
		result.Mentions++
		switch strings.ToLower(s.Sentiment) {
		case "bullish":
			result.Bullish++
		case "bearish":
			result.Bearish++
		default:
			result.Neutral++
		}
	}
	return result, true
}

// extractJSONArray tolerates reasoning or markdown surrounding the model's
// structured response. Some local/OpenAI-compatible models ignore JSON mode
// and prepend an explanation even when explicitly asked not to.
func extractJSONArray(content string) string {
	trimmed := cleanContent(content)
	start := strings.Index(trimmed, "[")
	end := strings.LastIndex(trimmed, "]")
	if start >= 0 && end > start {
		return trimmed[start : end+1]
	}
	return trimmed
}
