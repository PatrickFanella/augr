CREATE TABLE kalshi_settlement_gate (
    job_name TEXT PRIMARY KEY,
    consecutive_successes INTEGER NOT NULL DEFAULT 0,
    threshold INTEGER NOT NULL DEFAULT 0,
    eligible BOOLEAN NOT NULL DEFAULT FALSE,
    projection_fingerprint TEXT NOT NULL DEFAULT '',
    last_outcome TEXT NOT NULL DEFAULT '',
    last_error TEXT NOT NULL DEFAULT '',
    fetched INTEGER NOT NULL DEFAULT 0,
    resolved INTEGER NOT NULL DEFAULT 0,
    would_settle_markets INTEGER NOT NULL DEFAULT 0,
    would_settle_decisions INTEGER NOT NULL DEFAULT 0,
    last_run_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO kalshi_settlement_gate (job_name) VALUES ('kalshi_settlement') ON CONFLICT (job_name) DO NOTHING;
