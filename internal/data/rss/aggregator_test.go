package rss

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCurrentRSSArticleRejectsFabricatedStaleAndFutureDates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 6, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name      string
		published time.Time
		want      bool
	}{
		{name: "current", published: now.Add(-time.Hour), want: true},
		{name: "zero", published: time.Time{}, want: false},
		{name: "stale", published: now.Add(-25 * time.Hour), want: false},
		{name: "future", published: now.Add(16 * time.Minute), want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := currentRSSArticle(test.published, now); got != test.want {
				t.Fatalf("currentRSSArticle() = %t, want %t", got, test.want)
			}
		})
	}
}

func TestFetchWithStatsRetriesUntilArticleIsAcknowledged(t *testing.T) {
	t.Parallel()

	published := time.Now().UTC().Format(time.RFC1123)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<rss><channel><item><guid>x</guid><title>Title</title><pubDate>` + published + `</pubDate></item></channel></rss>`))
	}))
	defer server.Close()

	aggregator := NewAggregator([]Feed{{Name: "test", URL: server.URL}}, nil)
	first := aggregator.FetchWithStats(context.Background())
	second := aggregator.FetchWithStats(context.Background())
	if len(first.Articles) != 1 || len(second.Articles) != 1 {
		t.Fatalf("unacknowledged fetch counts = %d, %d; want retry", len(first.Articles), len(second.Articles))
	}
	aggregator.MarkSeen(first.Articles[0])
	if third := aggregator.FetchWithStats(context.Background()); len(third.Articles) != 0 {
		t.Fatalf("acknowledged fetch count = %d, want 0", len(third.Articles))
	}
}

func TestParseRSSLeavesInvalidPublicationDateUntrusted(t *testing.T) {
	t.Parallel()

	articles, err := parseRSS("test", []byte(`<rss><channel><item><guid>x</guid><title>Title</title><pubDate>not-a-date</pubDate></item></channel></rss>`))
	if err != nil {
		t.Fatal(err)
	}
	if len(articles) != 1 || !articles[0].PublishedAt.IsZero() {
		t.Fatalf("articles = %#v, want one zero-date item", articles)
	}
}
