package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewsFeedItem is a row in the news_feed table.
type NewsFeedItem struct {
	GUID        string    `json:"guid"`
	Source      string    `json:"source"`
	Title       string    `json:"title"`
	Description string    `json:"description,omitempty"`
	Link        string    `json:"link,omitempty"`
	PublishedAt time.Time `json:"published_at"`
	Tickers     []string  `json:"tickers,omitempty"`
	Category    string    `json:"category,omitempty"`
	Sentiment   string    `json:"sentiment,omitempty"`
	Relevance   float64   `json:"relevance,omitempty"`
	Summary     string    `json:"summary,omitempty"`
}

// SocialSentimentRow is a row in the social_sentiment table.
type SocialSentimentRow struct {
	Ticker     string    `json:"ticker"`
	Source     string    `json:"source"`
	Sentiment  float64   `json:"sentiment"`
	Bullish    float64   `json:"bullish"`
	Bearish    float64   `json:"bearish"`
	PostCount  int       `json:"post_count"`
	Trending   bool      `json:"trending"`
	MeasuredAt time.Time `json:"measured_at"`
}

// NewsFeedRepo persists news articles and social sentiment.
type NewsFeedRepo struct {
	pool *pgxpool.Pool
}

// NewNewsFeedRepo returns a new NewsFeedRepo.
func NewNewsFeedRepo(pool *pgxpool.Pool) *NewsFeedRepo {
	return &NewsFeedRepo{pool: pool}
}

// UpsertArticle inserts or ignores (by guid) a news article.
func (r *NewsFeedRepo) UpsertArticle(ctx context.Context, item *NewsFeedItem) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO news_feed (guid, source, title, description, link, published_at, tickers, category, sentiment, relevance, summary)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		 ON CONFLICT (guid) DO NOTHING`,
		item.GUID, item.Source, item.Title, item.Description, item.Link,
		item.PublishedAt, item.Tickers, item.Category, item.Sentiment,
		item.Relevance, item.Summary,
	)
	if err != nil {
		return fmt.Errorf("postgres: upsert news article: %w", err)
	}
	return nil
}

// UpdateTriage updates the LLM-derived triage fields for an existing article.
func (r *NewsFeedRepo) UpdateTriage(ctx context.Context, item *NewsFeedItem) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE news_feed SET tickers = $2, category = $3, sentiment = $4, relevance = $5, summary = $6
		 WHERE guid = $1`,
		item.GUID, item.Tickers, item.Category, item.Sentiment, item.Relevance, item.Summary,
	)
	if err != nil {
		return fmt.Errorf("postgres: update news triage: %w", err)
	}
	return nil
}

// ListRecent returns recent news articles, newest first.
func (r *NewsFeedRepo) ListRecent(ctx context.Context, limit int) ([]NewsFeedItem, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx,
		`SELECT guid, source, title, description, link, published_at, tickers, category, sentiment, relevance, summary
		 FROM news_feed
		 ORDER BY published_at DESC
		 LIMIT $1`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list recent news: %w", err)
	}
	defer rows.Close()

	var items []NewsFeedItem
	for rows.Next() {
		var item NewsFeedItem
		if err := rows.Scan(&item.GUID, &item.Source, &item.Title, &item.Description,
			&item.Link, &item.PublishedAt, &item.Tickers, &item.Category,
			&item.Sentiment, &item.Relevance, &item.Summary); err != nil {
			return nil, fmt.Errorf("postgres: scan news item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// ListByTicker returns recent news mentioning a specific ticker.
func (r *NewsFeedRepo) ListByTicker(ctx context.Context, ticker string, limit int) ([]NewsFeedItem, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := r.pool.Query(ctx,
		`SELECT guid, source, title, description, link, published_at, tickers, category, sentiment, relevance, summary
		 FROM news_feed
		 WHERE $1 = ANY(tickers)
		 ORDER BY published_at DESC
		 LIMIT $2`,
		ticker, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("postgres: list news by ticker: %w", err)
	}
	defer rows.Close()

	var items []NewsFeedItem
	for rows.Next() {
		var item NewsFeedItem
		if err := rows.Scan(&item.GUID, &item.Source, &item.Title, &item.Description,
			&item.Link, &item.PublishedAt, &item.Tickers, &item.Category,
			&item.Sentiment, &item.Relevance, &item.Summary); err != nil {
			return nil, fmt.Errorf("postgres: scan news item: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// InsertSocialSentiment persists a social sentiment snapshot.
func (r *NewsFeedRepo) InsertSocialSentiment(ctx context.Context, row *SocialSentimentRow) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO social_sentiment (ticker, source, sentiment, bullish, bearish, post_count, trending, measured_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		row.Ticker, row.Source, row.Sentiment, row.Bullish, row.Bearish,
		row.PostCount, row.Trending, row.MeasuredAt,
	)
	if err != nil {
		return fmt.Errorf("postgres: insert social sentiment: %w", err)
	}
	return nil
}
