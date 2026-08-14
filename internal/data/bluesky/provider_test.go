package bluesky

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetSocialSentimentSearchesPublicAppView(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/app.bsky.feed.searchPosts" {
			t.Errorf("path = %q", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "$AAPL" {
			t.Errorf("q = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"posts":[]}`))
	}))
	defer server.Close()

	provider := NewProvider(nil, "", nil)
	provider.api.SetBaseURL(server.URL)
	got, err := provider.GetSocialSentiment(context.Background(), " aapl ", time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("GetSocialSentiment() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("GetSocialSentiment() = %v, want empty", got)
	}
}
