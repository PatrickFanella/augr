package bluesky

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data"
	"github.com/PatrickFanella/get-rich-quick/internal/data/reddit"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/llm"
)

const (
	defaultBaseURL = "https://api.bsky.app"
	defaultLimit   = 50
)

// Provider searches the public Bluesky AppView for ticker cashtags and uses
// the existing bounded social classifier to derive a source-specific signal.
type Provider struct {
	api      *data.APIClient
	provider llm.Provider
	model    string
	logger   *slog.Logger
}

var _ data.DataProvider = (*Provider)(nil)

type searchResponse struct {
	Posts []struct {
		URI    string `json:"uri"`
		Record struct {
			Text      string `json:"text"`
			CreatedAt string `json:"createdAt"`
		} `json:"record"`
		Author struct {
			Handle string `json:"handle"`
		} `json:"author"`
		ReplyCount  int `json:"replyCount"`
		RepostCount int `json:"repostCount"`
		LikeCount   int `json:"likeCount"`
	} `json:"posts"`
}

func NewProvider(provider llm.Provider, model string, logger *slog.Logger) *Provider {
	if logger == nil {
		logger = slog.Default()
	}
	return &Provider{
		api: data.NewAPIClient(data.APIClientConfig{
			BaseURL:     defaultBaseURL,
			Headers:     http.Header{"Accept": []string{"application/json"}, "User-Agent": []string{"augr/1.0 social-research"}},
			Timeout:     15 * time.Second,
			RateLimiter: data.NewRateLimiter(120, time.Minute),
			Logger:      logger,
			Prefix:      "bluesky",
		}),
		provider: provider,
		model:    model,
		logger:   logger,
	}
}

func (p *Provider) GetOHLCV(context.Context, string, data.Timeframe, time.Time, time.Time) ([]domain.OHLCV, error) {
	return nil, fmt.Errorf("bluesky: GetOHLCV: %w", data.ErrNotImplemented)
}

func (p *Provider) GetFundamentals(context.Context, string) (data.Fundamentals, error) {
	return data.Fundamentals{}, fmt.Errorf("bluesky: GetFundamentals: %w", data.ErrNotImplemented)
}

func (p *Provider) GetNews(context.Context, string, time.Time, time.Time) ([]data.NewsArticle, error) {
	return nil, fmt.Errorf("bluesky: GetNews: %w", data.ErrNotImplemented)
}

func (p *Provider) GetSocialSentiment(ctx context.Context, ticker string, from, to time.Time) ([]data.SocialSentiment, error) {
	if p == nil || p.api == nil {
		return nil, errors.New("bluesky: provider is not configured")
	}
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" {
		return nil, errors.New("bluesky: ticker is required")
	}
	params := url.Values{"q": {"$" + ticker}, "limit": {fmt.Sprint(defaultLimit)}, "sort": {"latest"}}
	body, _, err := p.api.Get(ctx, "/xrpc/app.bsky.feed.searchPosts", params)
	if err != nil {
		return nil, fmt.Errorf("bluesky: search %s: %w", ticker, err)
	}
	var response searchResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("bluesky: decode search response: %w", err)
	}

	posts := make([]reddit.RedditPost, 0, len(response.Posts))
	engagement := 0
	for _, item := range response.Posts {
		createdAt, err := time.Parse(time.RFC3339Nano, item.Record.CreatedAt)
		if err != nil || createdAt.Before(from.UTC()) || createdAt.After(to.UTC()) {
			continue
		}
		posts = append(posts, reddit.RedditPost{Title: item.Record.Text, URL: item.URI, Author: item.Author.Handle, Subreddit: "bluesky", UpdatedAt: createdAt})
		engagement += item.ReplyCount + item.RepostCount + item.LikeCount
	}
	if len(posts) == 0 {
		return nil, nil
	}
	result := reddit.ScorePosts(ctx, p.provider, p.model, ticker, posts, p.logger)
	if result.Mentions == 0 {
		return nil, nil
	}
	total := result.Bullish + result.Bearish + result.Neutral
	if total == 0 {
		return nil, nil
	}
	bullish := float64(result.Bullish) / float64(total)
	bearish := float64(result.Bearish) / float64(total)
	return []data.SocialSentiment{{
		Ticker: ticker, Source: "bluesky", Score: bullish - bearish,
		Bullish: bullish, Bearish: bearish, PostCount: result.Mentions,
		CommentCount: engagement, MeasuredAt: time.Now().UTC(),
	}}, nil
}
