package reddit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

// Provider implements data.DataProvider, providing social sentiment from
// Reddit RSS feeds using LLM-based sentiment classification.
type Provider struct {
	client      *Client
	llmProvider llm.Provider
	model       string
	subreddits  []string
	logger      *slog.Logger
	postsMu     sync.Mutex
	posts       []RedditPost
	postsAt     time.Time
}

// Compile-time check that Provider satisfies data.DataProvider.
var _ data.DataProvider = (*Provider)(nil)

// NewProvider constructs a Reddit social sentiment provider.
// subreddits controls which subreddits are scanned; pass nil for stock defaults.
func NewProvider(llmProvider llm.Provider, model string, subreddits []string, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	if len(subreddits) == 0 {
		subreddits = StockSubreddits()
	}
	return &Provider{
		client:      NewClient(logger),
		llmProvider: llmProvider,
		model:       model,
		subreddits:  subreddits,
		logger:      logger,
	}
}

// GetOHLCV is not supported by the Reddit provider.
func (p *Provider) GetOHLCV(_ context.Context, _ string, _ data.Timeframe, _, _ time.Time) ([]domain.OHLCV, error) {
	return nil, fmt.Errorf("reddit: GetOHLCV: %w", data.ErrNotImplemented)
}

// GetFundamentals is not supported by the Reddit provider.
func (p *Provider) GetFundamentals(_ context.Context, _ string) (data.Fundamentals, error) {
	return data.Fundamentals{}, fmt.Errorf("reddit: GetFundamentals: %w", data.ErrNotImplemented)
}

// GetNews is not supported by the Reddit provider.
func (p *Provider) GetNews(_ context.Context, _ string, _, _ time.Time) ([]data.NewsArticle, error) {
	return nil, fmt.Errorf("reddit: GetNews: %w", data.ErrNotImplemented)
}

// GetSocialSentiment fetches recent Reddit posts from configured subreddits,
// runs LLM triage to detect ticker mentions and sentiment, and returns an
// aggregated SocialSentiment snapshot.
func (p *Provider) GetSocialSentiment(ctx context.Context, ticker string, from, to time.Time) ([]data.SocialSentiment, error) {
	if p == nil {
		return nil, errors.New("reddit: provider is nil")
	}

	posts := p.cachedPosts(ctx)
	if len(posts) == 0 {
		p.logger.Info("reddit: no posts fetched",
			slog.String("ticker", ticker),
			slog.Int("subreddits", len(p.subreddits)),
		)
		return nil, nil
	}

	// Filter posts to the requested time window. Reddit RSS returns ~25 most
	// recent posts per subreddit, so we keep any post whose UpdatedAt is zero
	// (unparseable) or falls within [from, to].
	fromUTC := from.UTC()
	toUTC := to.UTC()
	filtered := make([]RedditPost, 0, len(posts))
	for _, post := range posts {
		if post.UpdatedAt.IsZero() || (!post.UpdatedAt.Before(fromUTC) && !post.UpdatedAt.After(toUTC)) {
			filtered = append(filtered, post)
		}
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	p.logger.Info("reddit: scoring posts for sentiment",
		slog.String("ticker", ticker),
		slog.Int("posts", len(filtered)),
	)

	result := ScorePosts(ctx, p.llmProvider, p.model, ticker, filtered, p.logger)
	if result.Mentions == 0 {
		return nil, nil
	}

	total := result.Bullish + result.Bearish + result.Neutral
	var score, bullish, bearish float64
	if total > 0 {
		bullish = float64(result.Bullish) / float64(total)
		bearish = float64(result.Bearish) / float64(total)
		score = bullish - bearish
	}

	now := time.Now().UTC()
	return []data.SocialSentiment{{
		Ticker:     ticker,
		Source:     "reddit",
		Score:      score,
		Bullish:    bullish,
		Bearish:    bearish,
		PostCount:  result.Mentions,
		MeasuredAt: now,
	}}, nil
}

// cachedPosts fetches the shared subreddit corpus once per scan window. A
// social run scores many tickers against the same posts; refetching every feed
// per ticker both wastes quota and triggers Reddit's provider-wide throttle.
func (p *Provider) cachedPosts(ctx context.Context) []RedditPost {
	p.postsMu.Lock()
	defer p.postsMu.Unlock()
	if !p.postsAt.IsZero() && time.Since(p.postsAt) < 5*time.Minute {
		return append([]RedditPost(nil), p.posts...)
	}
	posts := p.client.FetchSubreddits(ctx, p.subreddits)
	p.posts = append(p.posts[:0], posts...)
	p.postsAt = time.Now()
	return append([]RedditPost(nil), posts...)
}
