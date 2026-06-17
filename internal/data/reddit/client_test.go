package reddit

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientFetchSubredditFallsBackOnRetryableStatus(t *testing.T) {
	client := NewClient(nil)
	var hosts []string
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		if req.URL.Host == "www.reddit.com" {
			return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("bad gateway")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(testAtomFeed)), Header: make(http.Header)}, nil
	})}

	posts, err := client.fetchSubreddit(context.Background(), "stocks")
	if err != nil {
		t.Fatalf("fetchSubreddit() error = %v", err)
	}
	if len(posts) != 1 {
		t.Fatalf("len(posts) = %d, want 1", len(posts))
	}
	if got := strings.Join(hosts, ","); got != "www.reddit.com,old.reddit.com" {
		t.Fatalf("hosts = %q, want www then old", got)
	}
}

func TestClientFetchSubredditsStartsCooldownOn429(t *testing.T) {
	client := NewClient(nil)
	var calls int
	client.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("rate limited")), Header: http.Header{"Retry-After": []string{"120"}}}, nil
	})}

	posts := client.FetchSubreddits(context.Background(), []string{"stocks"})
	if len(posts) != 0 {
		t.Fatalf("len(posts) = %d, want 0", len(posts))
	}
	if calls != 2 {
		t.Fatalf("calls after first fetch = %d, want 2 hosts", calls)
	}
	if remaining := client.cooldownRemaining("stocks"); remaining < 110*time.Second {
		t.Fatalf("cooldown = %s, want roughly Retry-After", remaining)
	}

	client.FetchSubreddits(context.Background(), []string{"stocks"})
	if calls != 2 {
		t.Fatalf("calls after cooldown fetch = %d, want unchanged", calls)
	}
}

const testAtomFeed = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>t3_test</id>
    <title>Test stock post</title>
    <link href="https://www.reddit.com/r/stocks/comments/test" />
    <author><name>tester</name></author>
    <content>body</content>
    <updated>2026-05-28T01:00:00Z</updated>
  </entry>
</feed>`
