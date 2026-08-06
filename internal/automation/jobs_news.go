package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data/rss"
	"github.com/PatrickFanella/get-rich-quick/internal/data/stocktwits"
	"github.com/PatrickFanella/get-rich-quick/internal/domain"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var (
	newsScanSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		Cron:         "7-59/30 * * * 1-5", // every 30 minutes during market hours, staggered off shared minute boundaries
		SkipWeekends: true,
		SkipHolidays: true,
	}
	socialScanSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		Cron:         "*/15 * * * 1-5", // every 15 minutes
		SkipWeekends: true,
		SkipHolidays: true,
	}
)

func (o *JobOrchestrator) registerNewsJobs() {
	o.Register("news_scan", "Aggregate financial news from RSS feeds with LLM triage", newsScanSpec, o.newsScan)
	o.Register("social_scan", "StockTwits trending + sentiment for portfolio tickers", socialScanSpec, o.socialScan)
}

// newsScan fetches RSS feeds, runs LLM triage, and persists tagged articles.
func (o *JobOrchestrator) newsScan(ctx context.Context) error {
	summary := map[string]int{"feeds_attempted": 0, "feeds_succeeded": 0, "feed_errors": 0, "fetched": 0, "saved": 0, "save_errors": 0, "triage_requested": 0, "classified": 0, "triage_missing": 0, "triage_write_errors": 0}
	defer func() { o.SetLastSummary("news_scan", summary) }()
	if o.deps.NewsFeedRepo == nil {
		o.logger.Info("news_scan: skipped — news feed repo not configured")
		return nil
	}

	// Lazily initialize the RSS aggregator.
	if o.rssAggregator == nil {
		o.rssAggregator = rss.NewAggregator(rss.DefaultFeeds(), o.logger)
	}

	fetch := o.rssAggregator.FetchWithStats(ctx)
	articles := fetch.Articles
	summary["feeds_attempted"] = fetch.FeedsAttempted
	summary["feeds_succeeded"] = fetch.FeedsSucceeded
	summary["feed_errors"] = fetch.FeedsFailed
	summary["fetched"] = len(articles)
	if len(articles) == 0 {
		o.logger.Info("news_scan: no new articles")
		return newsScanCompletionError(summary)
	}

	o.logger.Info("news_scan: fetched new articles", slog.Int("count", len(articles)))

	// Persist articles immediately (before triage) so we never lose them.
	var saved int
	for _, art := range articles {
		key := art.GUID
		if key == "" {
			key = art.Link
		}
		item := &pgrepo.NewsFeedItem{
			GUID:        key,
			Source:      art.Source,
			Title:       art.Title,
			Description: art.Description,
			Link:        art.Link,
			PublishedAt: art.PublishedAt,
		}
		if err := o.deps.NewsFeedRepo.UpsertArticle(ctx, item); err != nil {
			summary["save_errors"]++
			o.logger.Warn("news_scan: persist failed",
				slog.String("guid", key),
				slog.Any("error", err),
			)
			continue
		}
		saved++
	}
	summary["saved"] = saved

	o.logger.Info("news_scan: articles saved", slog.Int("saved", saved))

	// Best-effort LLM triage — classify headlines and update rows.
	// Only triage the first 20 articles to keep LLM time bounded.
	if o.deps.LLMProvider != nil && len(articles) > 0 {
		batch := articles
		if len(batch) > 20 {
			batch = batch[:20]
		}
		summary["triage_requested"] = len(batch)
		triageResults := rss.Triage(ctx, o.deps.LLMProvider, "", batch, o.logger)
		var classified int
		for _, art := range batch {
			key := art.GUID
			if key == "" {
				key = art.Link
			}
			tr, ok := triageResults[key]
			if !ok || tr == nil {
				summary["triage_missing"]++
				continue
			}
			item := &pgrepo.NewsFeedItem{
				GUID:      key,
				Tickers:   tr.Tickers,
				Category:  tr.Category,
				Sentiment: tr.Sentiment,
				Relevance: tr.Relevance,
				Summary:   tr.Summary,
			}
			// Update the already-persisted row with triage data.
			if err := o.deps.NewsFeedRepo.UpdateTriage(ctx, item); err != nil {
				summary["triage_write_errors"]++
				continue
			}
			classified++
		}
		o.logger.Info("news_scan: triage complete", slog.Int("classified", classified))
		summary["classified"] = classified
	}

	o.logger.Info("news_scan: complete",
		slog.Int("new_articles", len(articles)),
		slog.Int("saved", saved),
	)
	return newsScanCompletionError(summary)
}

// socialScan fetches StockTwits trending + sentiment for active strategy and open position tickers.
func (o *JobOrchestrator) socialScan(ctx context.Context) error {
	summary := map[string]int{"trending_fetched": 0, "trending_saved": 0, "tickers": 0, "sentiment_saved": 0, "errors": 0}
	defer func() { o.SetLastSummary("social_scan", summary) }()
	if o.deps.NewsFeedRepo == nil {
		o.logger.Info("social_scan: skipped — news feed repo not configured")
		return nil
	}

	client := stocktwits.NewClient(o.logger)

	// Fetch trending symbols.
	trending, err := client.GetTrending(ctx)
	if err != nil {
		summary["errors"]++
		o.logger.Warn("social_scan: trending fetch failed", slog.Any("error", err))
	} else {
		summary["trending_fetched"] = len(trending)
		now := time.Now()
		for _, t := range trending {
			if err := o.deps.NewsFeedRepo.InsertSocialSentiment(ctx, &pgrepo.SocialSentimentRow{
				Ticker:     t.Symbol,
				Source:     "stocktwits",
				Trending:   true,
				PostCount:  t.WatchlistCount,
				MeasuredAt: now,
			}); err != nil {
				summary["errors"]++
				continue
			}
			summary["trending_saved"]++
		}
		o.logger.Info("social_scan: trending symbols saved", slog.Int("count", len(trending)))
	}

	// Fetch sentiment for active strategy and open position tickers.
	tickers := make(map[string]struct{})
	addTicker := func(ticker string) {
		ticker = strings.ToUpper(strings.TrimSpace(ticker))
		if ticker == "" {
			return
		}
		tickers[ticker] = struct{}{}
	}

	if o.deps.StrategyRepo != nil {
		strategies, err := listAllStrategies(ctx, o.deps.StrategyRepo, repository.StrategyFilter{Status: "active"})
		if err != nil {
			return fmt.Errorf("social_scan: list strategies: %w", err)
		}
		for _, s := range strategies {
			if s.MarketType.Normalize() != domain.MarketTypeStock {
				continue
			}
			addTicker(s.Ticker)
		}
	}

	if o.deps.PositionRepo != nil {
		positions, err := listAllOpenPositions(ctx, o.deps.PositionRepo)
		if err != nil {
			summary["errors"]++
			o.logger.Warn("social_scan: open positions fetch failed", slog.Any("error", err))
		} else {
			for _, pos := range positions {
				marketType := pos.MarketType.Normalize()
				if marketType != "" && marketType != domain.MarketTypeStock {
					continue
				}
				addTicker(pos.Ticker)
			}
		}
	}
	summary["tickers"] = len(tickers)

	for ticker := range tickers {
		sentiment, err := client.GetSymbolSentiment(ctx, ticker)
		if err != nil {
			summary["errors"]++
			o.logger.Warn("social_scan: sentiment fetch failed",
				slog.String("ticker", ticker),
				slog.Any("error", err),
			)
			continue
		}

		if sentiment.Total > 0 {
			score, bullishRatio, bearishRatio := normalizeStocktwitsSentiment(sentiment)
			if err := o.deps.NewsFeedRepo.InsertSocialSentiment(ctx, &pgrepo.SocialSentimentRow{
				Ticker:     sentiment.Symbol,
				Source:     "stocktwits",
				Sentiment:  score,
				Bullish:    bullishRatio,
				Bearish:    bearishRatio,
				PostCount:  sentiment.Total,
				MeasuredAt: sentiment.MeasuredAt,
			}); err != nil {
				summary["errors"]++
				continue
			}
			summary["sentiment_saved"]++
		}
	}

	o.logger.Info("social_scan: complete")
	return socialScanCompletionError(summary)
}

func newsScanCompletionError(summary map[string]int) error {
	errors := summary["feed_errors"] + summary["save_errors"] + summary["triage_missing"] + summary["triage_write_errors"]
	if errors == 0 {
		return nil
	}
	return fmt.Errorf("news_scan: incomplete run: feed_errors=%d save_errors=%d triage_missing=%d triage_write_errors=%d",
		summary["feed_errors"], summary["save_errors"], summary["triage_missing"], summary["triage_write_errors"])
}

func socialScanCompletionError(summary map[string]int) error {
	if summary["errors"] == 0 {
		return nil
	}
	return fmt.Errorf("social_scan: completed with %d provider or persistence errors", summary["errors"])
}

func normalizeStocktwitsSentiment(sentiment *stocktwits.SymbolSentiment) (score, bullishRatio, bearishRatio float64) {
	if sentiment == nil || sentiment.Total <= 0 {
		return 0, 0, 0
	}
	bullishRatio = float64(sentiment.Bullish) / float64(sentiment.Total)
	bearishRatio = float64(sentiment.Bearish) / float64(sentiment.Total)
	return bullishRatio - bearishRatio, bullishRatio, bearishRatio
}

func listAllStrategies(ctx context.Context, repo repository.StrategyRepository, filter repository.StrategyFilter) ([]domain.Strategy, error) {
	count, err := repo.Count(ctx, filter)
	if err != nil {
		return nil, err
	}
	const pageSize = 100
	strategies := make([]domain.Strategy, 0, count)
	for offset := 0; offset < count; {
		page, err := repo.List(ctx, filter, min(pageSize, count-offset), offset)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			break
		}
		strategies = append(strategies, page...)
		offset += len(page)
	}
	return strategies, nil
}
