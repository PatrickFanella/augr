package data

import (
	"testing"
	"time"
)

func TestRankRelevantNewsFiltersTaggedFalsePositivesAndDeduplicates(t *testing.T) {
	now := time.Date(2026, 8, 4, 20, 0, 0, 0, time.UTC)
	articles := []NewsArticle{
		{Title: "Microsoft expands AI capacity", URL: "direct", PublishedAt: now.Add(-time.Hour), RelatedTickers: []string{"MSFT"}},
		{Title: "Space company signs compute deal", URL: "wrong", PublishedAt: now, RelatedTickers: []string{"GOOG", "SPACE"}},
		{Title: "MSFT technical breakout", URL: "text", PublishedAt: now.Add(-2 * time.Hour)},
		{Title: "Duplicate", URL: "direct", PublishedAt: now, RelatedTickers: []string{"MSFT"}},
	}

	got := RankRelevantNews("msft", articles, 10)
	if len(got) != 2 {
		t.Fatalf("RankRelevantNews() len = %d, want 2: %#v", len(got), got)
	}
	if got[0].URL != "direct" || got[0].Relevance != directTickerRelevance {
		t.Fatalf("first article = %#v, want direct ticker match", got[0])
	}
	if got[1].URL != "text" || got[1].Relevance != textTickerRelevance {
		t.Fatalf("second article = %#v, want ticker text match", got[1])
	}
}

func TestRankRelevantNewsPreservesUntaggedQueryScopedCoverage(t *testing.T) {
	got := RankRelevantNews("AAPL", []NewsArticle{{Title: "Apple supplier outlook", URL: "query-scoped"}}, 10)
	if len(got) != 1 || got[0].Relevance != queryScopedRelevance {
		t.Fatalf("RankRelevantNews() = %#v, want one query-scoped article", got)
	}
}

func TestRankRelevantNewsRequiresCashtagForShortSymbols(t *testing.T) {
	got := RankRelevantNews("AI", []NewsArticle{
		{Title: "Companies increase AI spending", URL: "ordinary-word"},
		{Title: "$AI announces earnings", URL: "cashtag"},
	}, 10)
	if len(got) != 2 {
		t.Fatalf("RankRelevantNews() len = %d, want 2", len(got))
	}
	if got[0].URL != "cashtag" || got[0].Relevance != textTickerRelevance {
		t.Fatalf("first article = %#v, want direct cashtag match", got[0])
	}
	if got[1].URL != "ordinary-word" || got[1].Relevance != queryScopedRelevance {
		t.Fatalf("second article = %#v, want lower-confidence query match", got[1])
	}
}
