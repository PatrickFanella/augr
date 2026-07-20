CREATE TABLE provider_rate_limit_cooldowns (
    provider TEXT PRIMARY KEY,
    retry_after_until TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
