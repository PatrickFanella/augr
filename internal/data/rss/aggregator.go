package rss

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Feed represents a single RSS feed source.
type Feed struct {
	Name string
	URL  string
}

// DefaultFeeds returns the set of verified financial news RSS feeds.
func DefaultFeeds() []Feed {
	return []Feed{
		{Name: "WSJ Markets", URL: "https://feeds.a.dj.com/rss/RSSMarketsMain.xml"},
		{Name: "WSJ Business", URL: "https://feeds.a.dj.com/rss/WSJcomUSBusiness.xml"},
		{Name: "CNBC Top News", URL: "https://search.cnbc.com/rs/search/combinedcms/view.xml?partnerId=wrss01&id=100003114"},
		{Name: "SeekingAlpha", URL: "https://seekingalpha.com/market_currents.xml"},
		{Name: "Investing.com", URL: "https://www.investing.com/rss/news.rss"},
	}
}

// Article is a parsed RSS item.
type Article struct {
	GUID        string
	Source      string
	Title       string
	Description string
	Link        string
	PublishedAt time.Time
}

// FetchResult reports source coverage as well as the newly observed articles.
type FetchResult struct {
	Articles       []Article
	FeedsAttempted int
	FeedsSucceeded int
	FeedsFailed    int
	ItemsRejected  int
}

// Aggregator fetches and deduplicates articles from multiple RSS feeds.
type Aggregator struct {
	feeds  []Feed
	client *http.Client
	logger *slog.Logger

	mu   sync.Mutex
	seen map[string]time.Time // GUID → first seen
}

// NewAggregator creates an RSS aggregator for the given feeds.
func NewAggregator(feeds []Feed, logger *slog.Logger) *Aggregator {
	if logger == nil {
		logger = slog.Default()
	}
	return &Aggregator{
		feeds:  feeds,
		client: &http.Client{Timeout: 15 * time.Second},
		logger: logger,
		seen:   make(map[string]time.Time),
	}
}

// Fetch retrieves new articles from all feeds, deduplicating by GUID.
// Returns only articles not seen in previous calls.
func (a *Aggregator) Fetch(ctx context.Context) []Article {
	result := a.FetchWithStats(ctx)
	for _, article := range result.Articles {
		a.MarkSeen(article)
	}
	return result.Articles
}

// FetchWithStats retrieves new articles and exposes per-feed completion so a
// caller cannot confuse a partial provider outage with a clean empty result.
func (a *Aggregator) FetchWithStats(ctx context.Context) FetchResult {
	var (
		mu        sync.Mutex
		articles  []Article
		succeeded int
		failed    int
		wg        sync.WaitGroup
	)

	for _, feed := range a.feeds {
		wg.Add(1)
		go func(f Feed) {
			defer wg.Done()
			items, err := a.fetchFeed(ctx, f)
			if err != nil {
				mu.Lock()
				failed++
				mu.Unlock()
				a.logger.Warn("rss: fetch failed",
					slog.String("feed", f.Name),
					slog.Any("error", err),
				)
				return
			}
			mu.Lock()
			succeeded++
			articles = append(articles, items...)
			mu.Unlock()
		}(feed)
	}

	wg.Wait()

	// Deduplicate.
	a.mu.Lock()
	defer a.mu.Unlock()

	var newArticles []Article
	batchSeen := make(map[string]struct{}, len(articles))
	now := time.Now()
	rejected := 0
	for _, art := range articles {
		if !currentRSSArticle(art.PublishedAt, now) {
			rejected++
			continue
		}
		key := art.GUID
		if key == "" {
			key = art.Link
		}
		if _, exists := a.seen[key]; exists {
			continue
		}
		if _, exists := batchSeen[key]; exists {
			continue
		}
		batchSeen[key] = struct{}{}
		newArticles = append(newArticles, art)
	}

	// Prune seen cache older than 24 hours.
	cutoff := now.Add(-24 * time.Hour)
	for k, t := range a.seen {
		if t.Before(cutoff) {
			delete(a.seen, k)
		}
	}

	return FetchResult{
		Articles:       newArticles,
		FeedsAttempted: len(a.feeds),
		FeedsSucceeded: succeeded,
		FeedsFailed:    failed,
		ItemsRejected:  rejected,
	}
}

// MarkSeen acknowledges an article only after its caller has completed the
// required persistence and enrichment work. Failed work therefore remains
// eligible for a later retry.
func (a *Aggregator) MarkSeen(article Article) {
	key := article.GUID
	if key == "" {
		key = article.Link
	}
	if key == "" {
		return
	}
	a.mu.Lock()
	a.seen[key] = time.Now()
	a.mu.Unlock()
}

func currentRSSArticle(publishedAt, now time.Time) bool {
	if publishedAt.IsZero() {
		return false
	}
	return !publishedAt.Before(now.Add(-24*time.Hour)) && !publishedAt.After(now.Add(15*time.Minute))
}

func (a *Aggregator) fetchFeed(ctx context.Context, feed Feed) ([]Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "get-rich-quick/1.0")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024)) // 2 MB limit
	if err != nil {
		return nil, err
	}

	return parseRSS(feed.Name, body)
}

// RSS XML structures.

type rssDoc struct {
	XMLName xml.Name   `xml:"rss"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Items []rssItem `xml:"item"`
}

type rssItem struct {
	GUID        string `xml:"guid"`
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
}

func parseRSS(source string, data []byte) ([]Article, error) {
	var doc rssDoc
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse RSS: %w", err)
	}

	var articles []Article
	for _, item := range doc.Channel.Items {
		pubDate, _ := time.Parse(time.RFC1123, strings.TrimSpace(item.PubDate))
		if pubDate.IsZero() {
			pubDate, _ = time.Parse(time.RFC1123Z, strings.TrimSpace(item.PubDate))
		}
		articles = append(articles, Article{
			GUID:        strings.TrimSpace(item.GUID),
			Source:      source,
			Title:       strings.TrimSpace(item.Title),
			Description: stripHTML(strings.TrimSpace(item.Description)),
			Link:        strings.TrimSpace(item.Link),
			PublishedAt: pubDate,
		})
	}

	return articles, nil
}

// stripHTML removes HTML tags from a string (simple approach for RSS descriptions).
func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			out.WriteRune(r)
		}
	}
	return strings.TrimSpace(out.String())
}
