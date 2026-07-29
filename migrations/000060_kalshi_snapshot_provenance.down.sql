ALTER TABLE kalshi_market_snapshots
    DROP CONSTRAINT IF EXISTS kalshi_market_snapshots_environment_check,
    DROP COLUMN IF EXISTS source_url,
    DROP COLUMN IF EXISTS environment,
    DROP COLUMN IF EXISTS provider;
