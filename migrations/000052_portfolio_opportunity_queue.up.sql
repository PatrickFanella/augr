-- 000052_portfolio_opportunity_queue.up.sql
-- Portfolio opportunity queue and allocator decision persistence.

CREATE TYPE order_side AS ENUM (
    'buy',
    'sell'
);

CREATE TYPE pipeline_signal AS ENUM (
    'buy',
    'sell',
    'hold'
);

CREATE TABLE portfolio_opportunities (
    id                 UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id        UUID            NOT NULL REFERENCES strategies (id),
    pipeline_run_id    UUID,
    market_type        market_type      NOT NULL,
    ticker             TEXT            NOT NULL,
    side               order_side      NOT NULL,
    prediction_side    TEXT            NOT NULL DEFAULT '',
    signal             pipeline_signal  NOT NULL,
    status             TEXT            NOT NULL CHECK (status IN ('queued', 'selected', 'rejected', 'expired', 'executed')),
    score              NUMERIC,
    confidence         NUMERIC         NOT NULL DEFAULT 0,
    edge_pct           NUMERIC         NOT NULL DEFAULT 0,
    expected_return_pct NUMERIC        NOT NULL DEFAULT 0,
    max_loss_pct       NUMERIC         NOT NULL DEFAULT 0,
    entry_price        NUMERIC         NOT NULL DEFAULT 0,
    liquidity_usd      NUMERIC         NOT NULL DEFAULT 0,
    market_cap_usd     NUMERIC         NOT NULL DEFAULT 0,
    spread_pct         NUMERIC         NOT NULL DEFAULT 0,
    proposed_notional  NUMERIC         NOT NULL DEFAULT 0,
    selected_notional  NUMERIC         NOT NULL DEFAULT 0,
    reason             TEXT            NOT NULL DEFAULT '',
    reject_reason      TEXT            NOT NULL DEFAULT '',
    evidence           JSONB           NOT NULL DEFAULT '{}'::jsonb,
    expires_at         TIMESTAMPTZ     NOT NULL,
    created_at         TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    dedupe_key         TEXT            NOT NULL UNIQUE
);

CREATE INDEX idx_portfolio_opportunities_status_expires_at
    ON portfolio_opportunities (status, expires_at);

CREATE INDEX idx_portfolio_opportunities_strategy_id
    ON portfolio_opportunities (strategy_id);

CREATE INDEX idx_portfolio_opportunities_market_type_ticker
    ON portfolio_opportunities (market_type, ticker);

CREATE TABLE allocation_decisions (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    opportunity_id  UUID            REFERENCES portfolio_opportunities (id),
    strategy_id     UUID            REFERENCES strategies (id),
    mode            TEXT            NOT NULL CHECK (mode IN ('shadow', 'paper')),
    action          TEXT            NOT NULL CHECK (action IN ('shadow_selected', 'shadow_rejected', 'paper_order_intent', 'execution_rejected', 'executed')),
    score           NUMERIC         NOT NULL DEFAULT 0,
    notional_usd    NUMERIC         NOT NULL DEFAULT 0,
    quantity        NUMERIC         NOT NULL DEFAULT 0,
    reasons         TEXT[]          NOT NULL DEFAULT '{}',
    created_order_id UUID           REFERENCES orders (id),
    created_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_allocation_decisions_mode_action_created_at
    ON allocation_decisions (mode, action, created_at DESC);
