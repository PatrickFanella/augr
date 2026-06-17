package signal

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

var redditFeedHosts = []string{
	"https://www.reddit.com",
	"https://old.reddit.com",
}

// DefaultSubreddits returns the default subreddits to monitor for signal events.
func DefaultSubreddits() []string {
	return []string{
		"politics",
		"worldnews",
		"cryptocurrency",
		"polymarket",
		"wallstreetbets",
	}
}

// RedditSource is a SignalSource that polls Reddit Atom feeds and emits new
// posts as RawSignalEvents. Deduplication uses a 24-hour in-memory seen set.
type RedditSource struct {
	subreddits []string
	interval   time.Duration
	client     *http.Client
	logger     *slog.Logger

	mu        sync.Mutex
	seen      map[string]time.Time // post URL → first seen
	cooldowns map[string]time.Time
}

// NewRedditSource creates a Reddit signal source for the given subreddits.
// If interval is zero it defaults to 60 seconds.
func NewRedditSource(subreddits []string, interval time.Duration, logger *slog.Logger) *RedditSource {
	if interval == 0 {
		interval = 60 * time.Second
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &RedditSource{
		subreddits: subreddits,
		interval:   interval,
		client:     &http.Client{Timeout: 15 * time.Second},
		logger:     logger,
		seen:       make(map[string]time.Time),
		cooldowns:  make(map[string]time.Time),
	}
}

func (r *RedditSource) Name() string { return "reddit" }

// Start polls each subreddit's RSS feed on the given interval, emitting one
// RawSignalEvent per new post. The channel is closed when ctx is cancelled.
func (r *RedditSource) Start(ctx context.Context) (<-chan RawSignalEvent, error) {
	ch := make(chan RawSignalEvent, 64)
	go func() {
		defer close(ch)
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				posts := r.fetchAll(ctx)
				for _, p := range posts {
					select {
					case ch <- p:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch, nil
}

func (r *RedditSource) fetchAll(ctx context.Context) []RawSignalEvent {
	var (
		mu     sync.Mutex
		events []RawSignalEvent
		wg     sync.WaitGroup
	)
	for _, sub := range r.subreddits {
		wg.Add(1)
		go func(sub string) {
			defer wg.Done()
			if wait := r.cooldownRemaining(sub); wait > 0 {
				r.logger.Debug("reddit: fetch skipped during cooldown",
					slog.String("subreddit", sub),
					slog.Duration("remaining", wait),
				)
				return
			}
			got, err := r.fetchSubreddit(ctx, sub)
			if err != nil {
				if retryAfter, ok := signalRedditRetryAfter(err); ok {
					r.startCooldown(sub, retryAfter)
					r.logger.Info("reddit: rate limited; cooling down subreddit",
						slog.String("subreddit", sub),
						slog.Duration("cooldown", retryAfter),
					)
					return
				}
				r.logger.Warn("reddit: fetch failed",
					slog.String("subreddit", sub),
					slog.Any("error", err),
				)
				return
			}
			mu.Lock()
			events = append(events, got...)
			mu.Unlock()
		}(sub)
	}
	wg.Wait()

	// Deduplicate and prune seen cache.
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)
	for k, t := range r.seen {
		if t.Before(cutoff) {
			delete(r.seen, k)
		}
	}

	var fresh []RawSignalEvent
	for _, e := range events {
		if _, exists := r.seen[e.URL]; exists {
			continue
		}
		r.seen[e.URL] = now
		fresh = append(fresh, e)
	}
	return fresh
}

func (r *RedditSource) fetchSubreddit(ctx context.Context, sub string) ([]RawSignalEvent, error) {
	var lastErr error
	for _, host := range redditFeedHosts {
		got, err := r.fetchSubredditFromHost(ctx, host, sub)
		if err == nil {
			return got, nil
		}
		lastErr = err
		if !isRetryableRedditError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}

func (r *RedditSource) fetchSubredditFromHost(ctx context.Context, host, sub string) ([]RawSignalEvent, error) {
	url := fmt.Sprintf("%s/r/%s/.rss", strings.TrimRight(host, "/"), sub)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// Reddit requires a descriptive User-Agent.
	req.Header.Set("User-Agent", "get-rich-quick/1.0 signal-monitor")
	req.Header.Set("Accept", "application/atom+xml, application/rss+xml, application/xml;q=0.9, */*;q=0.8")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, redditHTTPStatusError{status: resp.StatusCode, retryAfter: parseSignalRetryAfter(resp.Header.Get("Retry-After"))}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}

	return parseRedditAtom(sub, body)
}

type redditHTTPStatusError struct {
	status     int
	retryAfter time.Duration
}

func (e redditHTTPStatusError) Error() string {
	return fmt.Sprintf("status %d", e.status)
}

func isRetryableRedditError(err error) bool {
	if err == nil {
		return false
	}
	var statusErr redditHTTPStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	return statusErr.status == http.StatusTooManyRequests || statusErr.status >= http.StatusInternalServerError
}

func signalRedditRetryAfter(err error) (time.Duration, bool) {
	var statusErr redditHTTPStatusError
	if !errors.As(err, &statusErr) || statusErr.status != http.StatusTooManyRequests {
		return 0, false
	}
	if statusErr.retryAfter > 0 {
		return statusErr.retryAfter, true
	}
	return 15 * time.Minute, true
}

func parseSignalRetryAfter(raw string) time.Duration {
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

func (r *RedditSource) cooldownRemaining(sub string) time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	until := r.cooldowns[sub]
	if until.IsZero() {
		return 0
	}
	remaining := time.Until(until)
	if remaining <= 0 {
		delete(r.cooldowns, sub)
		return 0
	}
	return remaining
}

func (r *RedditSource) startCooldown(sub string, d time.Duration) {
	if d <= 0 {
		d = 15 * time.Minute
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	until := time.Now().Add(d)
	if until.After(r.cooldowns[sub]) {
		r.cooldowns[sub] = until
	}
}

// Reddit serves Atom 1.0 feeds; we parse only the fields we need.
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

func parseRedditAtom(subreddit string, data []byte) ([]RawSignalEvent, error) {
	var feed atomFeed
	if err := xml.Unmarshal(data, &feed); err != nil {
		return nil, fmt.Errorf("parse atom: %w", err)
	}

	now := time.Now()
	var events []RawSignalEvent
	for _, e := range feed.Entries {
		link := postLink(e)
		events = append(events, RawSignalEvent{
			Source: "reddit",
			Title:  strings.TrimSpace(e.Title),
			Body:   stripAtomHTML(e.Content),
			URL:    link,
			Metadata: map[string]any{
				"subreddit": subreddit,
				"author":    e.Author.Name,
			},
			ReceivedAt: now,
		})
	}
	return events, nil
}

// postLink returns the alternate (post) link from an Atom entry.
func postLink(e atomEntry) string {
	for _, l := range e.Links {
		if l.Rel == "alternate" {
			return l.Href
		}
	}
	// Fallback: first non-empty href.
	for _, l := range e.Links {
		if l.Href != "" {
			return l.Href
		}
	}
	return e.ID
}

// stripAtomHTML strips HTML tags from Atom content (Reddit wraps content in HTML).
func stripAtomHTML(s string) string {
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
