\set ON_ERROR_STOP on

CREATE TABLE historical_ohlcv (
    ticker text NOT NULL,
    provider text NOT NULL,
    timeframe text NOT NULL,
    bar_time timestamptz NOT NULL,
    open double precision NOT NULL,
    high double precision NOT NULL,
    low double precision NOT NULL,
    close double precision NOT NULL,
    volume double precision NOT NULL
);

INSERT INTO historical_ohlcv
SELECT 'T' || g, 'fixture', '1d', timestamptz '2020-01-01 UTC' + g * interval '1 day',
       g, g + 1, g - 1, g + 0.5, g * 100
FROM generate_series(1, 1253) g
CROSS JOIN generate_series(1, 2) duplicate;

CREATE TABLE universe_tickers (
    ticker text NOT NULL,
    name text,
    exchange text,
    index_group text NOT NULL,
    watch_score numeric(10,4) NOT NULL,
    last_scanned timestamptz,
    active boolean NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL
);

INSERT INTO universe_tickers VALUES
('BF.B', 'Brown-Forman Corporation Class B', 'XNYS', 'nyse', 8.6698, NULL, true, '2026-05-25 UTC', '2026-05-29 UTC'),
('BF.B', 'Brown-Forman Corporation Class B', 'XNYS', 'nyse', 0, NULL, true, '2026-05-30 UTC', '2026-07-20 UTC'),
('BIO.B', 'Bio-Rad Laboratories, Inc. Class B', 'XNYS', 'nyse', 0, NULL, true, '2026-05-25 UTC', '2026-07-20 UTC'),
('BIO.B', 'Bio-Rad Laboratories, Inc. Class B', 'XNYS', 'nyse', 0, NULL, true, '2026-05-30 UTC', '2026-07-20 UTC');

CREATE TABLE news_feed (
    id text NOT NULL,
    guid text NOT NULL,
    source text NOT NULL,
    title text NOT NULL,
    description text,
    link text,
    published_at timestamptz NOT NULL,
    tickers text[],
    category text,
    sentiment text,
    relevance numeric(4,3),
    summary text,
    created_at timestamptz NOT NULL,
    embedding text
);

INSERT INTO news_feed
SELECT 'old-' || g, 'GUID-' || g, 'fixture', 'Title ' || g, 'Description ' || g,
       'https://example.invalid/' || g, timestamptz '2026-07-19 UTC' + g * interval '1 minute',
       ARRAY['OLD' || g], NULL, NULL, NULL, NULL,
       timestamptz '2026-07-19 UTC' + g * interval '1 minute', NULL
FROM generate_series(1, 40) g;

INSERT INTO news_feed
SELECT 'new-' || g, 'GUID-' || g, 'fixture', 'Title ' || g, 'Description ' || g,
       'https://example.invalid/' || g, timestamptz '2026-07-19 UTC' + g * interval '1 minute',
       CASE WHEN g = 1 THEN ARRAY['NEW1'] ELSE ARRAY['OLD' || g] END,
       'market', 'neutral', 0.500, 'Enriched summary ' || g,
       timestamptz '2026-07-20 UTC' + g * interval '1 minute', NULL
FROM generate_series(1, 40) g;

CREATE TABLE audit_log (
    event_type text NOT NULL,
    entity_type text,
    actor text,
    details jsonb,
    created_at timestamptz NOT NULL DEFAULT now()
);
