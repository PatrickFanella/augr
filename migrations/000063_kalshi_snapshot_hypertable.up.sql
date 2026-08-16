CREATE TABLE kalshi_market_snapshots_partitioned (
    id UUID NOT NULL DEFAULT gen_random_uuid(),
    provider TEXT NOT NULL DEFAULT 'kalshi',
    environment TEXT NOT NULL DEFAULT 'unknown',
    source_url TEXT NOT NULL DEFAULT '',
    ticker TEXT NOT NULL,
    title TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    yes_bid DOUBLE PRECISION NOT NULL DEFAULT 0,
    yes_ask DOUBLE PRECISION NOT NULL DEFAULT 0,
    no_bid DOUBLE PRECISION NOT NULL DEFAULT 0,
    no_ask DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume DOUBLE PRECISION NOT NULL DEFAULT 0,
    open_interest DOUBLE PRECISION NOT NULL DEFAULT 0,
    close_time TIMESTAMPTZ,
    raw JSONB NOT NULL DEFAULT '{}'::jsonb,
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT kalshi_market_snapshots_partitioned_pkey
        PRIMARY KEY (id, captured_at),
    CONSTRAINT kalshi_market_snapshots_partitioned_environment_check
        CHECK (environment IN ('demo', 'live', 'unknown'))
);

SELECT create_hypertable(
    'kalshi_market_snapshots_partitioned',
    by_range('captured_at', INTERVAL '1 day'),
    if_not_exists => TRUE
);

CREATE INDEX idx_kalshi_market_snapshots_partitioned_ticker_captured_at
    ON kalshi_market_snapshots_partitioned (ticker, captured_at DESC);

CREATE INDEX idx_kalshi_market_snapshots_partitioned_captured_at
    ON kalshi_market_snapshots_partitioned (captured_at DESC);

ALTER TABLE kalshi_market_snapshots_partitioned SET (
    timescaledb.compress,
    timescaledb.compress_segmentby = 'provider,environment,ticker',
    timescaledb.compress_orderby = 'captured_at DESC, id'
);

SELECT add_compression_policy(
    'kalshi_market_snapshots_partitioned',
    compress_after => INTERVAL '7 days',
    if_not_exists => TRUE
);

SELECT add_retention_policy(
    'kalshi_market_snapshots_partitioned',
    drop_after => INTERVAL '30 days',
    if_not_exists => TRUE
);
