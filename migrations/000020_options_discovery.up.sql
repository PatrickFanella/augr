-- Options scan results for UI and overnight pipeline input.
CREATE TABLE IF NOT EXISTS options_scan_results (
    id             UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker         TEXT        NOT NULL,
    scan_date      DATE        NOT NULL,
    close_price    NUMERIC(20, 8),
    adv            NUMERIC(20, 2),
    iv_rank        NUMERIC(10, 4),
    iv_percentile  NUMERIC(10, 4),
    atm_iv         NUMERIC(10, 6),
    put_call_ratio NUMERIC(10, 4),
    volume_ratio   NUMERIC(10, 4),
    chain_depth    INT,
    atm_oi         NUMERIC(20, 2),
    score          NUMERIC(10, 6),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_options_scan_date ON options_scan_results (scan_date DESC);
CREATE INDEX IF NOT EXISTS idx_options_scan_ticker ON options_scan_results (ticker, scan_date DESC);
CREATE UNIQUE INDEX IF NOT EXISTS idx_options_scan_unique ON options_scan_results (ticker, scan_date);

-- Historical IV tracking for IV rank/percentile computation.
CREATE TABLE IF NOT EXISTS iv_history (
    ticker        TEXT        NOT NULL,
    date          DATE        NOT NULL,
    atm_iv        NUMERIC(10, 6) NOT NULL,
    iv_rank       NUMERIC(10, 4),
    iv_percentile NUMERIC(10, 4),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (ticker, date)
);

CREATE INDEX IF NOT EXISTS idx_iv_history_ticker ON iv_history (ticker, date DESC);
