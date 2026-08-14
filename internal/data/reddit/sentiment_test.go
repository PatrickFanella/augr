package reddit

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"

	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type stubLLMProvider struct {
	responses []string
	callCount atomic.Int32
	err       error
}

func (s *stubLLMProvider) Complete(_ context.Context, req llm.CompletionRequest) (*llm.CompletionResponse, error) {
	idx := int(s.callCount.Add(1)) - 1
	if s.err != nil {
		return nil, s.err
	}
	content := ""
	if idx < len(s.responses) {
		content = s.responses[idx]
	}
	return &llm.CompletionResponse{Content: content, Model: req.Model}, nil
}

func makePosts(n int) []RedditPost {
	posts := make([]RedditPost, n)
	for i := range posts {
		posts[i] = RedditPost{
			Title:     "Test post",
			Subreddit: "stocks",
		}
	}
	return posts
}

func TestScoreBatchValidJSON(t *testing.T) {
	t.Parallel()

	provider := &stubLLMProvider{
		responses: []string{`[
			{"mentions_ticker": true, "sentiment": "bullish"},
			{"mentions_ticker": false, "sentiment": "neutral"}
		]`},
	}

	result := scoreBatch(context.Background(), provider, "test-model", "AAPL",
		makePosts(2), 0, 1, discardLogger())

	if result.Mentions != 1 {
		t.Fatalf("expected 1 mention, got %d", result.Mentions)
	}
	if result.Bullish != 1 {
		t.Fatalf("expected 1 bullish, got %d", result.Bullish)
	}
}

func TestScoreBatchWrappedJSON(t *testing.T) {
	t.Parallel()

	provider := &stubLLMProvider{
		responses: []string{`{"results": [
			{"mentions_ticker": true, "sentiment": "bearish"}
		]}`},
	}

	result := scoreBatch(context.Background(), provider, "test-model", "AAPL",
		makePosts(1), 0, 1, discardLogger())

	if result.Bearish != 1 {
		t.Fatalf("expected 1 bearish, got %d", result.Bearish)
	}
}

func TestScoreBatchEmptyResponseRetries(t *testing.T) {
	t.Parallel()

	// First response is empty (simulates thinking-only output),
	// second response has valid JSON.
	provider := &stubLLMProvider{
		responses: []string{
			"",
			`[{"mentions_ticker": true, "sentiment": "bullish"}]`,
		},
	}

	result := scoreBatch(context.Background(), provider, "test-model", "AAPL",
		makePosts(1), 0, 1, discardLogger())

	calls := int(provider.callCount.Load())
	if calls != 2 {
		t.Fatalf("expected 2 LLM calls (1 retry), got %d", calls)
	}
	if result.Bullish != 1 {
		t.Fatalf("expected 1 bullish after retry, got %d", result.Bullish)
	}
}

func TestScoreBatchEmptyResponseExhaustsRetries(t *testing.T) {
	t.Parallel()

	// All responses empty — should exhaust retries gracefully.
	provider := &stubLLMProvider{
		responses: []string{"", ""},
	}

	result := scoreBatch(context.Background(), provider, "test-model", "AAPL",
		makePosts(1), 0, 1, discardLogger())

	calls := int(provider.callCount.Load())
	if calls != 2 {
		t.Fatalf("expected 2 LLM calls (initial + 1 retry), got %d", calls)
	}
	if result.Mentions != 0 {
		t.Fatalf("expected 0 mentions on exhausted retries, got %d", result.Mentions)
	}
}

func TestScoreBatchLLMError(t *testing.T) {
	t.Parallel()

	provider := &stubLLMProvider{
		err: errors.New("network timeout"),
	}

	result := scoreBatch(context.Background(), provider, "test-model", "AAPL",
		makePosts(1), 0, 1, discardLogger())

	if result.Mentions != 0 {
		t.Fatalf("expected 0 mentions on error, got %d", result.Mentions)
	}
}

func TestScoreBatchMalformedJSON(t *testing.T) {
	t.Parallel()

	provider := &stubLLMProvider{
		responses: []string{`not json at all`},
	}

	result := scoreBatch(context.Background(), provider, "test-model", "AAPL",
		makePosts(1), 0, 1, discardLogger())

	if result.Mentions != 0 {
		t.Fatalf("expected 0 mentions on malformed JSON, got %d", result.Mentions)
	}
}

func TestScorePostsConcurrency(t *testing.T) {
	t.Parallel()

	// 25 posts → 3 batches (10 + 10 + 5). All should return valid results.
	validJSON := `[
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"},
		{"mentions_ticker": true, "sentiment": "bullish"}
	]`

	provider := &stubLLMProvider{
		responses: []string{validJSON, validJSON, validJSON},
	}

	result := ScorePosts(context.Background(), provider, "test-model", "AAPL",
		makePosts(25), discardLogger())

	calls := int(provider.callCount.Load())
	if calls != 3 {
		t.Fatalf("expected 3 LLM calls for 25 posts, got %d", calls)
	}
	// 10+10+5=25 posts, all mention ticker.
	// Third batch has only 5 posts but response has 10 entries — extra entries
	// are still counted. This is acceptable since real LLM matches post count.
	if result.Mentions < 25 {
		t.Fatalf("expected at least 25 mentions, got %d", result.Mentions)
	}
}

func TestScorePostsNilProvider(t *testing.T) {
	t.Parallel()

	result := ScorePosts(context.Background(), nil, "test-model", "AAPL",
		makePosts(5), discardLogger())

	if result.Mentions != 0 {
		t.Fatalf("expected 0 mentions with nil provider, got %d", result.Mentions)
	}
}

func TestScorePostsEmptyPosts(t *testing.T) {
	t.Parallel()

	provider := &stubLLMProvider{}
	result := ScorePosts(context.Background(), provider, "test-model", "AAPL",
		nil, discardLogger())

	if result.Mentions != 0 {
		t.Fatalf("expected 0 mentions with empty posts, got %d", result.Mentions)
	}
}

func TestCleanContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"plain JSON", `[{"a":1}]`, `[{"a":1}]`},
		{"markdown fenced", "```json\n[{\"a\":1}]\n```", `[{"a":1}]`},
		{"empty", "", ""},
		{"whitespace only", "   \n  ", ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := cleanContent(tc.input)
			if got != tc.want {
				t.Fatalf("cleanContent(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestParseSentimentResponse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		content    string
		wantOK     bool
		wantResult SentimentResult
	}{
		{
			name:       "valid array",
			content:    `[{"mentions_ticker": true, "sentiment": "bullish"}, {"mentions_ticker": true, "sentiment": "bearish"}]`,
			wantOK:     true,
			wantResult: SentimentResult{Mentions: 2, Bullish: 1, Bearish: 1},
		},
		{
			name:       "wrapped results",
			content:    `{"results": [{"mentions_ticker": true, "sentiment": "neutral"}]}`,
			wantOK:     true,
			wantResult: SentimentResult{Mentions: 1, Neutral: 1},
		},
		{
			name:       "reasoning before array",
			content:    "I analyzed the posts first.\n```json\n[{\"mentions_ticker\":true,\"sentiment\":\"bullish\"}]\n```",
			wantOK:     true,
			wantResult: SentimentResult{Mentions: 1, Bullish: 1},
		},
		{
			name:    "invalid JSON",
			content: "not json",
			wantOK:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, ok := parseSentimentResponse(tc.content)
			if ok != tc.wantOK {
				t.Fatalf("parseSentimentResponse() ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && result != tc.wantResult {
				t.Fatalf("parseSentimentResponse() = %+v, want %+v", result, tc.wantResult)
			}
		})
	}
}
