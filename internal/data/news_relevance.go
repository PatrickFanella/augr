package data

import (
	"sort"
	"strings"
	"unicode"
)

const (
	directTickerRelevance = 1.0
	textTickerRelevance   = 0.85
	queryScopedRelevance  = 0.50
)

// RankRelevantNews removes provider-tagged false positives, de-duplicates
// stories, assigns an auditable relevance score, and ranks direct coverage
// ahead of query-scoped coverage. Articles with related-ticker metadata are
// accepted only when that metadata includes the requested ticker.
func RankRelevantNews(ticker string, articles []NewsArticle, limit int) []NewsArticle {
	ticker = strings.ToUpper(strings.TrimSpace(ticker))
	if ticker == "" || len(articles) == 0 || limit == 0 {
		return nil
	}
	seen := make(map[string]bool, len(articles))
	ranked := make([]NewsArticle, 0, len(articles))
	for _, article := range articles {
		relevance, keep := newsArticleRelevance(ticker, article)
		if !keep {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(article.URL))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(article.Title))
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		article.Relevance = relevance
		ranked = append(ranked, article)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].Relevance != ranked[j].Relevance {
			return ranked[i].Relevance > ranked[j].Relevance
		}
		return ranked[i].PublishedAt.After(ranked[j].PublishedAt)
	})
	if limit > 0 && len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

func newsArticleRelevance(ticker string, article NewsArticle) (float64, bool) {
	if len(article.RelatedTickers) > 0 {
		for _, related := range article.RelatedTickers {
			if strings.EqualFold(strings.TrimSpace(related), ticker) {
				return directTickerRelevance, true
			}
		}
		return 0, false
	}
	text := article.Title + " " + article.Summary
	if containsDirectTickerReference(text, ticker) {
		return textTickerRelevance, true
	}
	// Providers such as NewsAPI and Finnhub are already queried by ticker but
	// do not return entity metadata. Preserve those results at a lower score so
	// strategies can distinguish them from confirmed direct coverage.
	return queryScopedRelevance, true
}

func containsDirectTickerReference(text, ticker string) bool {
	upper := strings.ToUpper(text)
	if containsTickerToken(upper, "$"+ticker) {
		return true
	}
	// One- and two-character symbols (A, AI, IT, ON, etc.) collide heavily
	// with ordinary prose. Without provider entity metadata, require a cashtag.
	if len([]rune(ticker)) < 3 {
		return false
	}
	return containsTickerToken(upper, ticker)
}

func containsTickerToken(text, ticker string) bool {
	for _, token := range strings.FieldsFunc(strings.ToUpper(text), func(r rune) bool {
		return r != '$' && !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if token == ticker {
			return true
		}
	}
	return false
}
