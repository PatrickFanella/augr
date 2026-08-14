-- Stock leader following and paper portfolio replication.

CREATE TABLE copy_leaders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    entity_type     TEXT NOT NULL CHECK (entity_type IN ('individual', 'institution')),
    display_name    TEXT NOT NULL CHECK (btrim(display_name) <> ''),
    sec_cik         TEXT,
    identity_status TEXT NOT NULL DEFAULT 'unverified'
        CHECK (identity_status IN ('unverified', 'public_filing_verified', 'connected_verified')),
    metadata        JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_copy_leaders_sec_cik
    ON copy_leaders (sec_cik)
    WHERE sec_cik IS NOT NULL;

CREATE TABLE copy_leader_sources (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    leader_id        UUID NOT NULL REFERENCES copy_leaders(id) ON DELETE CASCADE,
    provider         TEXT NOT NULL,
    source_type      TEXT NOT NULL CHECK (source_type IN ('sec_13f', 'sec_form4', 'connected_broker', 'kalshi_connected')),
    external_key     TEXT NOT NULL CHECK (btrim(external_key) <> ''),
    status           TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'degraded', 'disabled')),
    metadata         JSONB NOT NULL DEFAULT '{}'::jsonb,
    checkpoint       JSONB NOT NULL DEFAULT '{}'::jsonb,
    last_observed_at TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, source_type, external_key)
);

CREATE INDEX idx_copy_leader_sources_leader ON copy_leader_sources (leader_id, created_at DESC);

CREATE TABLE copy_source_observations (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    source_id               UUID NOT NULL REFERENCES copy_leader_sources(id) ON DELETE CASCADE,
    provider_observation_id TEXT NOT NULL,
    observation_kind        TEXT NOT NULL CHECK (observation_kind IN ('transaction', 'portfolio_snapshot')),
    schema_version          INT NOT NULL DEFAULT 1 CHECK (schema_version > 0),
    effective_at            TIMESTAMPTZ NOT NULL,
    published_at            TIMESTAMPTZ NOT NULL,
    observed_at             TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    amendment_number        INT NOT NULL DEFAULT 0 CHECK (amendment_number >= 0),
    supersedes_id           UUID REFERENCES copy_source_observations(id),
    status                  TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'superseded', 'invalid')),
    content_hash            TEXT NOT NULL,
    normalized_payload      JSONB NOT NULL DEFAULT '{}'::jsonb,
    source_url              TEXT NOT NULL DEFAULT '',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (source_id, provider_observation_id, content_hash)
);

CREATE INDEX idx_copy_observations_source_published
    ON copy_source_observations (source_id, published_at DESC);

CREATE TABLE copy_portfolio_snapshots (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    observation_id          UUID NOT NULL UNIQUE REFERENCES copy_source_observations(id) ON DELETE CASCADE,
    report_period            DATE NOT NULL,
    total_disclosed_value   NUMERIC(24, 2) NOT NULL CHECK (total_disclosed_value >= 0),
    holding_count           INT NOT NULL CHECK (holding_count >= 0),
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE copy_portfolio_holdings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    snapshot_id         UUID NOT NULL REFERENCES copy_portfolio_snapshots(id) ON DELETE CASCADE,
    issuer_name         TEXT NOT NULL,
    title_of_class      TEXT NOT NULL DEFAULT '',
    cusip               TEXT NOT NULL,
    figi                TEXT NOT NULL DEFAULT '',
    disclosed_value     NUMERIC(24, 2) NOT NULL CHECK (disclosed_value >= 0),
    shares_or_principal NUMERIC(24, 6) NOT NULL CHECK (shares_or_principal >= 0),
    amount_type         TEXT NOT NULL DEFAULT '',
    put_call            TEXT NOT NULL DEFAULT '',
    investment_discretion TEXT NOT NULL DEFAULT '',
    voting_sole         NUMERIC(24, 6) NOT NULL DEFAULT 0,
    voting_shared       NUMERIC(24, 6) NOT NULL DEFAULT 0,
    voting_none         NUMERIC(24, 6) NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_copy_holdings_snapshot_value
    ON copy_portfolio_holdings (snapshot_id, disclosed_value DESC, id);
CREATE INDEX idx_copy_holdings_cusip ON copy_portfolio_holdings (cusip);

CREATE TABLE copy_instrument_mappings (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider            TEXT NOT NULL,
    identifier_type     TEXT NOT NULL,
    identifier_value    TEXT NOT NULL,
    instrument_key      TEXT NOT NULL,
    ticker              TEXT NOT NULL,
    confidence          TEXT NOT NULL DEFAULT 'manual_verified'
        CHECK (confidence IN ('manual_verified', 'provider_verified', 'ambiguous', 'stale')),
    mapping_method      TEXT NOT NULL DEFAULT 'manual',
    valid_from          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_to            TIMESTAMPTZ,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (provider, identifier_type, identifier_value, valid_from)
);

CREATE UNIQUE INDEX idx_copy_instrument_mapping_current
    ON copy_instrument_mappings (provider, identifier_type, identifier_value)
    WHERE valid_to IS NULL;

CREATE TABLE copy_subscriptions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    leader_id               UUID NOT NULL REFERENCES copy_leaders(id),
    source_id               UUID NOT NULL REFERENCES copy_leader_sources(id),
    strategy_id             UUID NOT NULL UNIQUE REFERENCES strategies(id),
    status                  TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'previewed', 'paper_active', 'paused', 'live_eligible', 'live_active', 'stopped')),
    is_paper                BOOLEAN NOT NULL DEFAULT TRUE CHECK (is_paper = TRUE),
    method                  TEXT NOT NULL DEFAULT 'target_weight'
        CHECK (method IN ('target_weight', 'fixed_notional', 'source_ratio')),
    capital_budget          NUMERIC(24, 2) NOT NULL CHECK (capital_budget > 0),
    cash_buffer_pct         NUMERIC(8, 6) NOT NULL DEFAULT 0.10 CHECK (cash_buffer_pct >= 0 AND cash_buffer_pct < 1),
    top_n                   INT NOT NULL DEFAULT 10 CHECK (top_n > 0 AND top_n <= 100),
    min_source_weight       NUMERIC(8, 6) NOT NULL DEFAULT 0.01 CHECK (min_source_weight >= 0 AND min_source_weight < 1),
    max_position_weight     NUMERIC(8, 6) NOT NULL DEFAULT 0.15 CHECK (max_position_weight > 0 AND max_position_weight <= 1),
    max_turnover_pct        NUMERIC(8, 6) NOT NULL DEFAULT 0.25 CHECK (max_turnover_pct > 0 AND max_turnover_pct <= 1),
    min_price               NUMERIC(20, 8) NOT NULL DEFAULT 5 CHECK (min_price >= 0),
    min_avg_dollar_volume   NUMERIC(24, 2) NOT NULL DEFAULT 1000000 CHECK (min_avg_dollar_volume >= 0),
    max_spread_bps          INT NOT NULL DEFAULT 100 CHECK (max_spread_bps >= 0),
    stock_allowlist         TEXT[] NOT NULL DEFAULT '{}',
    stock_blocklist         TEXT[] NOT NULL DEFAULT '{}',
    created_by              TEXT NOT NULL DEFAULT 'system',
    created_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    stopped_at              TIMESTAMPTZ
);

CREATE INDEX idx_copy_subscriptions_source_status ON copy_subscriptions (source_id, status);

CREATE TABLE copy_trade_intents (
    id                         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subscription_id            UUID NOT NULL REFERENCES copy_subscriptions(id) ON DELETE CASCADE,
    source_observation_id      UUID NOT NULL REFERENCES copy_source_observations(id),
    pipeline_run_id            UUID,
    instrument_key             TEXT NOT NULL,
    ticker                     TEXT NOT NULL,
    side                       TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    target_weight              NUMERIC(12, 8) NOT NULL DEFAULT 0,
    target_value               NUMERIC(24, 2) NOT NULL DEFAULT 0,
    attributed_current_value   NUMERIC(24, 2) NOT NULL DEFAULT 0,
    requested_notional         NUMERIC(24, 2) NOT NULL DEFAULT 0,
    executable_price           NUMERIC(20, 8),
    calculation_version        INT NOT NULL DEFAULT 1 CHECK (calculation_version > 0),
    calculation                JSONB NOT NULL DEFAULT '{}'::jsonb,
    policy_status              TEXT NOT NULL CHECK (policy_status IN ('approved', 'rejected', 'skipped')),
    policy_reasons             TEXT[] NOT NULL DEFAULT '{}',
    risk_status                TEXT NOT NULL DEFAULT 'pending' CHECK (risk_status IN ('pending', 'approved', 'rejected')),
    risk_reasons               TEXT[] NOT NULL DEFAULT '{}',
    order_id                   UUID REFERENCES orders(id),
    status                     TEXT NOT NULL DEFAULT 'received'
        CHECK (status IN ('received', 'skipped', 'policy_rejected', 'risk_rejected', 'ordered', 'partial', 'filled', 'cancelled', 'failed')),
    created_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at                 TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (subscription_id, source_observation_id, instrument_key, calculation_version)
);

CREATE INDEX idx_copy_intents_subscription_created
    ON copy_trade_intents (subscription_id, created_at DESC);
