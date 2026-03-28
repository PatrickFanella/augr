-- 000006_api_keys.up.sql
-- Store hashed API keys for programmatic access.

CREATE TABLE api_keys (
    id                    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name                  TEXT        NOT NULL,
    key_prefix            TEXT        NOT NULL UNIQUE,
    key_hash              TEXT        NOT NULL UNIQUE,
    rate_limit_per_minute INT         NOT NULL DEFAULT 100,
    last_used_at          TIMESTAMPTZ,
    expires_at            TIMESTAMPTZ,
    revoked_at            TIMESTAMPTZ,
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_api_keys_created_at ON api_keys (created_at DESC);
CREATE INDEX idx_api_keys_revoked_at ON api_keys (revoked_at);
