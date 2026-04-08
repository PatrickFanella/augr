-- Polymarket account profiling: track wallet addresses, trade history, and win rates.

CREATE TABLE IF NOT EXISTS polymarket_accounts (
    address          TEXT PRIMARY KEY,
    display_name     TEXT,
    first_seen       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active      TIMESTAMPTZ,
    total_trades     INT NOT NULL DEFAULT 0,
    total_volume     NUMERIC(20, 2) NOT NULL DEFAULT 0,
    markets_entered  INT NOT NULL DEFAULT 0,
    markets_won      INT NOT NULL DEFAULT 0,
    markets_lost     INT NOT NULL DEFAULT 0,
    win_rate         NUMERIC(6, 4) NOT NULL DEFAULT 0,
    category_stats   JSONB,
    avg_position     NUMERIC(20, 2),
    max_position     NUMERIC(20, 2),
    avg_entry_hours_before_resolution NUMERIC(10, 2),
    early_entry_rate NUMERIC(6, 4) NOT NULL DEFAULT 0,
    tags             TEXT[],
    tracked          BOOLEAN NOT NULL DEFAULT false,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Fast lookup for tracked accounts sorted by win rate.
CREATE INDEX IF NOT EXISTS idx_polymarket_accounts_tracked_win_rate
    ON polymarket_accounts (win_rate DESC)
    WHERE tracked = true;

-- Polymarket per-account trade records.
CREATE TABLE IF NOT EXISTS polymarket_account_trades (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_address TEXT NOT NULL REFERENCES polymarket_accounts(address) ON DELETE CASCADE,
    market_slug     TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('YES', 'NO')),
    action          TEXT NOT NULL CHECK (action IN ('buy', 'sell')),
    price           NUMERIC(10, 6) NOT NULL,
    size_usdc       NUMERIC(20, 2) NOT NULL,
    timestamp       TIMESTAMPTZ NOT NULL,
    outcome         TEXT,
    pnl             NUMERIC(20, 2),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pm_account_trades_address
    ON polymarket_account_trades (account_address, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_pm_account_trades_market
    ON polymarket_account_trades (market_slug, timestamp DESC);
