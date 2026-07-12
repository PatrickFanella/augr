package reddit

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/integration/redditlimit"
)

const (
	userAgent     = "get-rich-quick/1.0 social-sentiment"
	clientTimeout = 15 * time.Second
	maxBodySize   = 2 * 1024 * 1024 // 2 MiB
	fetchDelay    = 1 * time.Second // delay between subreddit fetches to respect rate limits
	maxSubreddits = 10              // safety cap
)

var feedHosts = []string{
	"https://www.reddit.com",
	"https://old.reddit.com",
}

// StockSubreddits returns the default subreddits to scan for equity sentiment.
func StockSubreddits() []string {
	return []string{"wallstreetbets", "stocks", "investing", "options"}
}

// CryptoSubreddits returns the default subreddits to scan for crypto sentiment.
func CryptoSubreddits() []string {
	return []string{"cryptocurrency", "cryptomarkets"}
}

// RedditPost is a single post parsed from a Reddit Atom RSS feed.
type RedditPost struct {
	Title     string
	Body      string
	URL       string
	Author    string
	Subreddit string
	UpdatedAt time.Time
}

// Client fetches Reddit RSS feeds and returns parsed posts.
type Client struct {
	client  *http.Client
	logger  *slog.Logger
	limiter *redditlimit.Coordinator
}

// NewClient creates a Reddit RSS client.
func NewClient(logger *slog.Logger) *Client {
	if logger == nil {
		logger = slog.Default()
	}
	return &Client{
		client:  &http.Client{Timeout: clientTimeout},
		logger:  logger,
		limiter: redditlimit.Default,
	}
}

// FetchSubreddits fetches posts from multiple subreddits concurrently.
// A short delay is inserted between requests to respect Reddit rate limits.
func (c *Client) FetchSubreddits(ctx context.Context, subreddits []string) []RedditPost {
	if len(subreddits) > maxSubreddits {
		subreddits = subreddits[:maxSubreddits]
	}

	var posts []RedditPost

	for i, sub := range subreddits {
		if wait := c.cooldownRemaining(sub); wait > 0 {
			c.logger.Debug("reddit: provider cooldown active; skipping remaining feeds",
				slog.Duration("remaining", wait.Round(time.Second)),
			)
			break
		}
		if i > 0 {
			select {
			case <-ctx.Done():
				return posts
			case <-time.After(fetchDelay):
			}
		}

		got, err := c.fetchSubreddit(ctx, sub)
		if err != nil {
			if retryAfter, ok := redditRetryAfter(err); ok {
				effective := c.startCooldown(sub, retryAfter)
				c.logger.Info("reddit: rate limited; provider cooldown started",
					slog.String("subreddit", sub),
					slog.Duration("cooldown", effective.Round(time.Second)),
				)
				break
			}
			c.logger.Warn("reddit: fetch failed", slog.String("subreddit", sub), slog.Any("error", err))
			continue
		}
		c.limiter.MarkSuccess(time.Now().UTC())
		posts = append(posts, got...)
	}

	return posts
}

func (c *Client) fetchSubreddit(ctx context.Context, sub string) ([]RedditPost, error) {
	var lastErr error
	for _, host := range feedHosts {
		got, err := c.fetchSubredditFromHost(ctx, host, sub)
		if err == nil {
			return got, nil
		}
		lastErr = err
		if _, limited := redditRetryAfter(err); limited {
			return nil, err
		}
		if !isRetryableStatusError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (c *Client) fetchSubredditFromHost(ctx context.Context, host, sub string) ([]RedditPost, error) {
	feedURL := fmt.Sprintf("%s/r/%s/.rss", strings.TrimRight(host, "/"), sub)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/xml;q=0.9, */*;q=0.8")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort close

	if resp.StatusCode != http.StatusOK {
		return nil, statusError{status: resp.StatusCode, retryAfter: parseRetryAfter(resp.Header.Get("Retry-After"))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodySize))
	if err != nil {
		return nil, err
	}

	return parseAtomFeed(sub, body)
}

type statusError struct {
	status     int
	retryAfter time.Duration
}

func (e statusError) Error() string {
	return fmt.Sprintf("status %d", e.status)
}

func isRetryableStatusError(err error) bool {
	var statusErr statusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.status == http.StatusTooManyRequests || statusErr.status >= http.StatusInternalServerError
}

func redditRetryAfter(err error) (time.Duration, bool) {
	var statusErr statusError
	if !errors.As(err, &statusErr) || statusErr.status != http.StatusTooManyRequests {
		return 0, false
	}
	if statusErr.retryAfter > 0 {
		return statusErr.retryAfter, true
	}
	return 15 * time.Minute, true
}

func parseRetryAfter(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	if secs, err := time.ParseDuration(raw + "s"); err == nil {
		return secs
	}
	if when, err := http.ParseTime(raw); err == nil {
		return time.Until(when)
	}
	return 0
}

func (c *Client) cooldownRemaining(sub string) time.Duration {
	_ = sub // cooldown is intentionally provider-wide, not subreddit-specific.
	return c.limiter.Remaining(time.Now())
}

func (c *Client) startCooldown(sub string, d time.Duration) time.Duration {
	_ = sub
	return c.limiter.Start(time.Now(), d)
}

// ── Atom XML types (Reddit serves Atom 1.0) ────────────────────────────

type atomFeed struct {
	XMLName xml.Name    `xml:"feed"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	ID      string     `xml:"id"`
	Title   string     `xml:"title"`
	Links   []atomLink `xml:"link"`
	Author  atomAuthor `xml:"author"`
	Content string     `xml:"content"`
	Updated string     `xml:"updated"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

type atomAuthor struct {
	Name string `xml:"name"`
}

func parseAtomFeed(subreddit string, raw []byte) ([]RedditPost, error) {
	var feed atomFeed
	if err := xml.Unmarshal(raw, &feed); err != nil {
		return nil, fmt.Errorf("parse atom: %w", err)
	}

	posts := make([]RedditPost, 0, len(feed.Entries))
	for _, e := range feed.Entries {
		updated, _ := time.Parse(time.RFC3339, e.Updated)
		posts = append(posts, RedditPost{
			Title:     strings.TrimSpace(e.Title),
			Body:      stripHTML(e.Content),
			URL:       postLink(e),
			Author:    e.Author.Name,
			Subreddit: subreddit,
			UpdatedAt: updated,
		})
	}
	return posts, nil
}

func postLink(e atomEntry) string {
	for _, l := range e.Links {
		if l.Rel == "alternate" {
			return l.Href
		}
	}
	for _, l := range e.Links {
		if l.Href != "" {
			return l.Href
		}
	}
	return e.ID
}

// stripHTML removes HTML tags from content (Reddit wraps Atom content in HTML).
func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}
