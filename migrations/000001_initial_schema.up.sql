-- 000001_initial_schema.up.sql
-- Initial database schema for get-rich-quick trading agent platform.

-- ============================================================================
-- EXTENSIONS
-- ============================================================================

-- Ensures gen_random_uuid() is available on PostgreSQL < 13.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ============================================================================
-- ENUM TYPES
-- Values must stay aligned with Go domain constants in internal/domain/.
-- ============================================================================

CREATE TYPE pipeline_status AS ENUM (
    'running',
    'completed',
    'failed',
    'cancelled'
);

CREATE TYPE order_status AS ENUM (
    'pending',
    'submitted',
    'partial',
    'filled',
    'cancelled',
    'rejected'
);

CREATE TYPE trade_side AS ENUM (
    'buy',
    'sell'
);

CREATE TYPE order_type AS ENUM (
    'market',
    'limit',
    'stop',
    'stop_limit'
);

CREATE TYPE market_type AS ENUM (
    'stock',
    'crypto',
    'polymarket'
);

CREATE TYPE position_side AS ENUM (
    'long',
    'short'
);

-- ============================================================================
-- TABLES
-- ============================================================================

-- strategies: trading strategy definitions
CREATE TABLE strategies (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT        NOT NULL,
    description   TEXT,
    ticker        TEXT        NOT NULL,
    market_type   market_type NOT NULL,
    schedule_cron TEXT,
    config        JSONB       NOT NULL DEFAULT '{}',
    is_active     BOOLEAN     NOT NULL DEFAULT FALSE,
    is_paper      BOOLEAN     NOT NULL DEFAULT TRUE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- pipeline_runs: individual pipeline execution records, partitioned by month.
-- Note: Because the PK includes the partition key (trade_date), other tables
-- store only pipeline_run_id without a formal FK. Referential integrity for
-- pipeline_run_id is enforced at the application layer.
CREATE TABLE pipeline_runs (
    id              UUID            NOT NULL DEFAULT gen_random_uuid(),
    strategy_id     UUID            NOT NULL,
    ticker          TEXT            NOT NULL,
    trade_date      DATE            NOT NULL,
    status          pipeline_status NOT NULL DEFAULT 'running',
    signal          TEXT            NOT NULL DEFAULT '',
    started_at      TIMESTAMPTZ     NOT NULL,
    completed_at    TIMESTAMPTZ,
    error_message   TEXT            NOT NULL DEFAULT '',
    config_snapshot JSONB,
    PRIMARY KEY (id, trade_date)
) PARTITION BY RANGE (trade_date);

-- agent_decisions: LLM agent decision logs, partitioned by date.
-- Same FK caveat as pipeline_runs: pipeline_run_id has no formal FK.
CREATE TABLE agent_decisions (
    id                UUID        NOT NULL DEFAULT gen_random_uuid(),
    pipeline_run_id   UUID        NOT NULL,
    agent_role        TEXT        NOT NULL,
    phase             TEXT        NOT NULL,
    round_number      INT,
    input_summary     TEXT,
    output_text       TEXT        NOT NULL,
    output_structured JSONB,
    llm_provider      TEXT,
    llm_model         TEXT,
    prompt_tokens     INT,
    completion_tokens INT,
    latency_ms        INT,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- orders: broker order records
CREATE TABLE orders (
    id               UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id      UUID         REFERENCES strategies (id),
    pipeline_run_id  UUID,
    external_id      TEXT,
    ticker           TEXT         NOT NULL,
    side             trade_side   NOT NULL,
    order_type       order_type   NOT NULL,
    quantity         NUMERIC(20, 8) NOT NULL,
    limit_price      NUMERIC(20, 8),
    stop_price       NUMERIC(20, 8),
    filled_quantity  NUMERIC(20, 8) NOT NULL DEFAULT 0,
    filled_avg_price NUMERIC(20, 8),
    status           order_status NOT NULL DEFAULT 'pending',
    broker           TEXT,
    submitted_at     TIMESTAMPTZ,
    filled_at        TIMESTAMPTZ,
    created_at       TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- positions: current and historical position tracking
CREATE TABLE positions (
    id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id     UUID           REFERENCES strategies (id),
    ticker          TEXT           NOT NULL,
    side            position_side  NOT NULL,
    quantity        NUMERIC(20, 8) NOT NULL,
    avg_entry       NUMERIC(20, 8) NOT NULL,
    current_price   NUMERIC(20, 8),
    unrealized_pnl  NUMERIC(20, 8),
    realized_pnl    NUMERIC(20, 8) NOT NULL DEFAULT 0,
    stop_loss       NUMERIC(20, 8),
    take_profit     NUMERIC(20, 8),
    opened_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    closed_at       TIMESTAMPTZ
);

-- trades: individual fill/execution records
CREATE TABLE trades (
    id          UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID           REFERENCES orders (id),
    position_id UUID           REFERENCES positions (id),
    ticker      TEXT           NOT NULL,
    side        trade_side     NOT NULL,
    quantity    NUMERIC(20, 8) NOT NULL,
    price       NUMERIC(20, 8) NOT NULL,
    fee         NUMERIC(20, 8) NOT NULL DEFAULT 0,
    executed_at TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    created_at  TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

-- agent_memories: semantic memory store for agents
CREATE TABLE agent_memories (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_role       TEXT        NOT NULL,
    situation        TEXT        NOT NULL,
    situation_tsv    TSVECTOR,
    recommendation   TEXT        NOT NULL DEFAULT '',
    outcome          TEXT,
    pipeline_run_id  UUID,
    relevance_score  NUMERIC(5, 4),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Trigger to auto-populate agent_memories.situation_tsv from situation text
CREATE OR REPLACE FUNCTION agent_memories_tsv_trigger() RETURNS trigger AS $$
BEGIN
    NEW.situation_tsv := to_tsvector('english', NEW.situation);
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_agent_memories_tsv
    BEFORE INSERT OR UPDATE OF situation ON agent_memories
    FOR EACH ROW EXECUTE FUNCTION agent_memories_tsv_trigger();

-- market_data_cache: cached market data from providers
CREATE TABLE market_data_cache (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker     TEXT        NOT NULL,
    provider   TEXT        NOT NULL,
    data_type  TEXT        NOT NULL,
    timeframe  TEXT,
    date_from  DATE,
    date_to    DATE,
    data       JSONB       NOT NULL DEFAULT '{}',
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ
);

-- audit_log: system-wide audit trail, partitioned by date
CREATE TABLE audit_log (
    id          UUID        NOT NULL DEFAULT gen_random_uuid(),
    event_type  TEXT        NOT NULL,
    entity_type TEXT,
    entity_id   UUID,
    actor       TEXT,
    details     JSONB,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (id, created_at)
) PARTITION BY RANGE (created_at);

-- ============================================================================
-- PARTITIONS
-- Default partitions catch rows outside explicitly created ranges.
-- Initial quarterly partitions are provided for the current period; add more
-- via follow-up migrations or an automated partition-management job.
-- ============================================================================

-- pipeline_runs: monthly partitions
CREATE TABLE pipeline_runs_2026_q1 PARTITION OF pipeline_runs
    FOR VALUES FROM ('2026-01-01') TO ('2026-04-01');
CREATE TABLE pipeline_runs_2026_q2 PARTITION OF pipeline_runs
    FOR VALUES FROM ('2026-04-01') TO ('2026-07-01');
CREATE TABLE pipeline_runs_default PARTITION OF pipeline_runs DEFAULT;

-- agent_decisions: quarterly partitions
CREATE TABLE agent_decisions_2026_q1 PARTITION OF agent_decisions
    FOR VALUES FROM ('2026-01-01') TO ('2026-04-01');
CREATE TABLE agent_decisions_2026_q2 PARTITION OF agent_decisions
    FOR VALUES FROM ('2026-04-01') TO ('2026-07-01');
CREATE TABLE agent_decisions_default PARTITION OF agent_decisions DEFAULT;

-- audit_log: quarterly partitions
CREATE TABLE audit_log_2026_q1 PARTITION OF audit_log
    FOR VALUES FROM ('2026-01-01') TO ('2026-04-01');
CREATE TABLE audit_log_2026_q2 PARTITION OF audit_log
    FOR VALUES FROM ('2026-04-01') TO ('2026-07-01');
CREATE TABLE audit_log_default PARTITION OF audit_log DEFAULT;

-- ============================================================================
-- INDEXES
-- ============================================================================

-- strategies
CREATE INDEX idx_strategies_ticker ON strategies (ticker);
CREATE INDEX idx_strategies_market_type ON strategies (market_type);
CREATE INDEX idx_strategies_is_active ON strategies (is_active);

-- pipeline_runs
CREATE INDEX idx_pipeline_runs_strategy_id ON pipeline_runs (strategy_id);
CREATE INDEX idx_pipeline_runs_ticker ON pipeline_runs (ticker);
CREATE INDEX idx_pipeline_runs_status ON pipeline_runs (status);
CREATE INDEX idx_pipeline_runs_trade_date ON pipeline_runs (trade_date);

-- agent_decisions
CREATE INDEX idx_agent_decisions_pipeline_run_id ON agent_decisions (pipeline_run_id);
CREATE INDEX idx_agent_decisions_agent_role ON agent_decisions (agent_role);
CREATE INDEX idx_agent_decisions_created_at ON agent_decisions (created_at);

-- orders
CREATE INDEX idx_orders_strategy_id ON orders (strategy_id);
CREATE INDEX idx_orders_pipeline_run_id ON orders (pipeline_run_id);
CREATE INDEX idx_orders_ticker ON orders (ticker);
CREATE INDEX idx_orders_status ON orders (status);
CREATE INDEX idx_orders_external_id ON orders (external_id);

-- positions
CREATE INDEX idx_positions_strategy_id ON positions (strategy_id);
CREATE INDEX idx_positions_ticker ON positions (ticker);
CREATE INDEX idx_positions_closed_at ON positions (closed_at);

-- trades
CREATE INDEX idx_trades_order_id ON trades (order_id);
CREATE INDEX idx_trades_position_id ON trades (position_id);
CREATE INDEX idx_trades_ticker ON trades (ticker);
CREATE INDEX idx_trades_executed_at ON trades (executed_at);

-- agent_memories: GIN index for full-text search
CREATE INDEX idx_agent_memories_situation_tsv ON agent_memories USING GIN (situation_tsv);
CREATE INDEX idx_agent_memories_agent_role ON agent_memories (agent_role);
CREATE INDEX idx_agent_memories_pipeline_run_id ON agent_memories (pipeline_run_id);

-- market_data_cache
CREATE INDEX idx_market_data_cache_ticker_provider ON market_data_cache (ticker, provider);
CREATE INDEX idx_market_data_cache_expires_at ON market_data_cache (expires_at);

-- audit_log
CREATE INDEX idx_audit_log_event_type ON audit_log (event_type);
CREATE INDEX idx_audit_log_entity ON audit_log (entity_type, entity_id);
CREATE INDEX idx_audit_log_created_at ON audit_log (created_at);
