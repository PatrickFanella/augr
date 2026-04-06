package automation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/PatrickFanella/get-rich-quick/internal/data/rss"
	"github.com/PatrickFanella/get-rich-quick/internal/data/stocktwits"
	"github.com/PatrickFanella/get-rich-quick/internal/repository"
	pgrepo "github.com/PatrickFanella/get-rich-quick/internal/repository/postgres"
	"github.com/PatrickFanella/get-rich-quick/internal/scheduler"
)

var (
	newsScanSpec = scheduler.ScheduleSpec{
		Type:         scheduler.ScheduleTypeMarketHours,
		Cron:         "*/5 * * * 1-5", // every 5 minutes during market hours
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
	if o.deps.NewsFeedRepo == nil {
		o.logger.Info("news_scan: skipped — news feed repo not configured")
		return nil
	}

	// Lazily initialize the RSS aggregator.
	if o.rssAggregator == nil {
		o.rssAggregator = rss.NewAggregator(rss.DefaultFeeds(), o.logger)
	}

	articles := o.rssAggregator.Fetch(ctx)
	if len(articles) == 0 {
		o.logger.Info("news_scan: no new articles")
		return nil
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
			o.logger.Warn("news_scan: persist failed",
				slog.String("guid", key),
				slog.Any("error", err),
			)
			continue
		}
		saved++
	}

	o.logger.Info("news_scan: articles saved", slog.Int("saved", saved))

	// Best-effort LLM triage — classify headlines and update rows.
	// Only triage the first 20 articles to keep LLM time bounded.
	if o.deps.LLMProvider != nil && len(articles) > 0 {
		batch := articles
		if len(batch) > 20 {
			batch = batch[:20]
		}
		triageResults := rss.Triage(ctx, o.deps.LLMProvider, "", batch, o.logger)
		var classified int
		for _, art := range batch {
			key := art.GUID
			if key == "" {
				key = art.Link
			}
			tr, ok := triageResults[key]
			if !ok || tr == nil {
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
				continue
			}
			classified++
		}
		o.logger.Info("news_scan: triage complete", slog.Int("classified", classified))
	}

	o.logger.Info("news_scan: complete",
		slog.Int("new_articles", len(articles)),
		slog.Int("saved", saved),
	)
	return nil
}

// socialScan fetches StockTwits trending + sentiment for active strategy tickers.
func (o *JobOrchestrator) socialScan(ctx context.Context) error {
	if o.deps.NewsFeedRepo == nil {
		o.logger.Info("social_scan: skipped — news feed repo not configured")
		return nil
	}

	client := stocktwits.NewClient(o.logger)

	// Fetch trending symbols.
	trending, err := client.GetTrending(ctx)
	if err != nil {
		o.logger.Warn("social_scan: trending fetch failed", slog.Any("error", err))
	} else {
		now := time.Now()
		for _, t := range trending {
			_ = o.deps.NewsFeedRepo.InsertSocialSentiment(ctx, &pgrepo.SocialSentimentRow{
				Ticker:     t.Symbol,
				Source:     "stocktwits",
				Trending:   true,
				PostCount:  t.WatchlistCount,
				MeasuredAt: now,
			})
		}
		o.logger.Info("social_scan: trending symbols saved", slog.Int("count", len(trending)))
	}

	// Fetch sentiment for active strategy tickers.
	if o.deps.StrategyRepo != nil {
		strategies, err := o.deps.StrategyRepo.List(ctx, repository.StrategyFilter{Status: "active"}, 50, 0)
		if err != nil {
			return fmt.Errorf("social_scan: list strategies: %w", err)
		}

		// Deduplicate tickers.
		seen := make(map[string]bool)
		for _, s := range strategies {
			if seen[s.Ticker] {
				continue
			}
			seen[s.Ticker] = true

			sentiment, err := client.GetSymbolSentiment(ctx, s.Ticker)
			if err != nil {
				o.logger.Warn("social_scan: sentiment fetch failed",
					slog.String("ticker", s.Ticker),
					slog.Any("error", err),
				)
				continue
			}

			if sentiment.Total > 0 {
				_ = o.deps.NewsFeedRepo.InsertSocialSentiment(ctx, &pgrepo.SocialSentimentRow{
					Ticker:     sentiment.Symbol,
					Source:     "stocktwits",
					Sentiment:  sentiment.Score,
					Bullish:    float64(sentiment.Bullish),
					Bearish:    float64(sentiment.Bearish),
					PostCount:  sentiment.Total,
					MeasuredAt: sentiment.MeasuredAt,
				})
			}
		}
	}

	o.logger.Info("social_scan: complete")
	return nil
}
