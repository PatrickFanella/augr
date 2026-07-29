ALTER TABLE kalshi_market_snapshots
    ADD COLUMN provider TEXT NOT NULL DEFAULT 'kalshi',
    ADD COLUMN environment TEXT NOT NULL DEFAULT 'unknown',
    ADD COLUMN source_url TEXT NOT NULL DEFAULT '';

ALTER TABLE kalshi_market_snapshots
    ADD CONSTRAINT kalshi_market_snapshots_environment_check
    CHECK (environment IN ('demo', 'live', 'unknown'));
