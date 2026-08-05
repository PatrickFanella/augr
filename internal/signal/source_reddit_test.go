package signal

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/integration/redditlimit"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestRedditSourceFetchSubredditFallsBackOnRetryableStatus(t *testing.T) {
	source := NewRedditSource([]string{"polymarket"}, 0, nil)
	source.limiter = &redditlimit.Coordinator{}
	var hosts []string
	source.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		hosts = append(hosts, req.URL.Host)
		if req.URL.Host == "www.reddit.com" {
			return &http.Response{StatusCode: http.StatusBadGateway, Body: io.NopCloser(strings.NewReader("bad gateway")), Header: make(http.Header)}, nil
		}
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(testRedditAtom)), Header: make(http.Header)}, nil
	})}

	events, err := source.fetchSubreddit(context.Background(), "polymarket")
	if err != nil {
		t.Fatalf("fetchSubreddit() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("len(events) = %d, want 1", len(events))
	}
	if got := strings.Join(hosts, ","); got != "www.reddit.com,old.reddit.com" {
		t.Fatalf("hosts = %q, want www then old", got)
	}
}

func TestRedditSourceFetchAllStartsCooldownOn429(t *testing.T) {
	source := NewRedditSource([]string{"polymarket"}, 0, nil)
	source.limiter = &redditlimit.Coordinator{}
	var calls int
	source.client = &http.Client{Transport: roundTripFunc(func(_ *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusTooManyRequests, Body: io.NopCloser(strings.NewReader("rate limited")), Header: http.Header{"Retry-After": []string{"120"}}}, nil
	})}

	events := source.fetchAll(context.Background())
	if len(events) != 0 {
		t.Fatalf("len(events) = %d, want 0", len(events))
	}
	if calls != 1 {
		t.Fatalf("calls after first fetch = %d, want rate limit to stop fallback", calls)
	}
	if remaining := source.cooldownRemaining("polymarket"); remaining < 110*time.Second {
		t.Fatalf("cooldown = %s, want roughly Retry-After", remaining)
	}

	source.fetchAll(context.Background())
	if calls != 1 {
		t.Fatalf("calls after cooldown fetch = %d, want unchanged", calls)
	}
}

const testRedditAtom = `<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom">
  <entry>
    <id>t3_test</id>
    <title>Test post</title>
    <link href="https://www.reddit.com/r/polymarket/comments/test" />
    <author><name>tester</name></author>
    <content>body</content>
    <updated>2026-05-28T01:00:00Z</updated>
  </entry>
</feed>`
