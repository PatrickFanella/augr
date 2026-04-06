-- News feed articles with LLM-derived triage metadata.
CREATE TABLE IF NOT EXISTS news_feed (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    guid        TEXT        NOT NULL,
    source      TEXT        NOT NULL,
    title       TEXT        NOT NULL,
    description TEXT,
    link        TEXT,
    published_at TIMESTAMPTZ NOT NULL,
    tickers     TEXT[],
    category    TEXT,
    sentiment   TEXT,
    relevance   NUMERIC(4, 3),
    summary     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_news_feed_guid ON news_feed (guid);
CREATE INDEX IF NOT EXISTS idx_news_feed_published ON news_feed (published_at DESC);
CREATE INDEX IF NOT EXISTS idx_news_feed_tickers ON news_feed USING GIN (tickers);
CREATE INDEX IF NOT EXISTS idx_news_feed_category ON news_feed (category, published_at DESC);

-- Social sentiment snapshots from StockTwits/Reddit.
CREATE TABLE IF NOT EXISTS social_sentiment (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker       TEXT        NOT NULL,
    source       TEXT        NOT NULL,
    sentiment    NUMERIC(6, 4),
    bullish      NUMERIC(6, 4),
    bearish      NUMERIC(6, 4),
    post_count   INT,
    trending     BOOLEAN     NOT NULL DEFAULT FALSE,
    raw_data     JSONB,
    measured_at  TIMESTAMPTZ NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_social_sentiment_ticker ON social_sentiment (ticker, measured_at DESC);
CREATE INDEX IF NOT EXISTS idx_social_sentiment_source ON social_sentiment (source, measured_at DESC);
