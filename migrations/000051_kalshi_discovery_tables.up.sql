CREATE TABLE IF NOT EXISTS kalshi_watched_markets (
    ticker TEXT PRIMARY KEY,
    event_ticker TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    category TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    close_time TIMESTAMPTZ,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    added_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kalshi_watched_markets_enabled_added_at
    ON kalshi_watched_markets (enabled, added_at DESC);

CREATE TABLE IF NOT EXISTS kalshi_market_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
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
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kalshi_market_snapshots_ticker_captured_at
    ON kalshi_market_snapshots (ticker, captured_at DESC);

CREATE INDEX IF NOT EXISTS idx_kalshi_market_snapshots_captured_at
    ON kalshi_market_snapshots (captured_at DESC);

CREATE TABLE IF NOT EXISTS kalshi_discovery_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    status TEXT NOT NULL DEFAULT 'running',
    fetched INTEGER NOT NULL DEFAULT 0,
    screened INTEGER NOT NULL DEFAULT 0,
    proposed INTEGER NOT NULL DEFAULT 0,
    deployed INTEGER NOT NULL DEFAULT 0,
    errors JSONB NOT NULL DEFAULT '[]'::jsonb,
    summary JSONB NOT NULL DEFAULT '{}'::jsonb,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_kalshi_discovery_runs_status_updated_at
    ON kalshi_discovery_runs (status, updated_at DESC);

CREATE INDEX IF NOT EXISTS idx_kalshi_discovery_runs_started_at
    ON kalshi_discovery_runs (started_at DESC);
