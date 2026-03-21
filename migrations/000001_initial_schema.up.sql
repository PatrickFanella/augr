-- 000001_initial_schema.up.sql
-- Initial database schema for get-rich-quick trading agent platform.

-- ============================================================================
-- ENUM TYPES
-- ============================================================================

CREATE TYPE pipeline_status AS ENUM (
    'pending',
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
    'rejected',
    'expired'
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
    'forex',
    'options',
    'futures'
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

-- pipeline_runs: individual pipeline execution records, partitioned by month
CREATE TABLE pipeline_runs (
    id              UUID            NOT NULL DEFAULT gen_random_uuid(),
    strategy_id     UUID            NOT NULL,
    ticker          TEXT            NOT NULL,
    trade_date      DATE            NOT NULL,
    status          pipeline_status NOT NULL DEFAULT 'pending',
    signal          TEXT,
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    error_message   TEXT,
    config_snapshot JSONB,
    PRIMARY KEY (id, trade_date)
) PARTITION BY RANGE (trade_date);

-- agent_decisions: LLM agent decision logs, partitioned by date
CREATE TABLE agent_decisions (
    id                UUID        NOT NULL DEFAULT gen_random_uuid(),
    pipeline_run_id   UUID        NOT NULL,
    agent_role        TEXT        NOT NULL,
    phase             TEXT        NOT NULL,
    round_number      INT         NOT NULL DEFAULT 1,
    input_summary     TEXT,
    output_text       TEXT,
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
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id     UUID         NOT NULL,
    pipeline_run_id UUID,
    external_id     TEXT,
    ticker          TEXT         NOT NULL,
    side            trade_side   NOT NULL,
    order_type      order_type   NOT NULL,
    quantity         NUMERIC(20, 8) NOT NULL,
    limit_price     NUMERIC(20, 8),
    stop_price      NUMERIC(20, 8),
    filled_quantity NUMERIC(20, 8) NOT NULL DEFAULT 0,
    filled_avg_price NUMERIC(20, 8),
    status          order_status NOT NULL DEFAULT 'pending',
    broker          TEXT,
    submitted_at    TIMESTAMPTZ,
    filled_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- positions: current and historical position tracking
CREATE TABLE positions (
    id              UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id     UUID           NOT NULL,
    ticker          TEXT           NOT NULL,
    side            trade_side     NOT NULL,
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
    order_id    UUID           NOT NULL,
    position_id UUID,
    ticker      TEXT           NOT NULL,
    side        trade_side     NOT NULL,
    quantity    NUMERIC(20, 8) NOT NULL,
    price       NUMERIC(20, 8) NOT NULL,
    fee         NUMERIC(20, 8) NOT NULL DEFAULT 0,
    executed_at TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

-- agent_memories: semantic memory store for agents
CREATE TABLE agent_memories (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_role       TEXT        NOT NULL,
    situation        TEXT        NOT NULL,
    situation_tsv    TSVECTOR,
    recommendation   TEXT,
    outcome          TEXT,
    pipeline_run_id  UUID,
    relevance_score  NUMERIC(5, 4),
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

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
-- DEFAULT PARTITIONS (catch-all for data outside explicitly created ranges)
-- ============================================================================

CREATE TABLE pipeline_runs_default PARTITION OF pipeline_runs DEFAULT;
CREATE TABLE agent_decisions_default PARTITION OF agent_decisions DEFAULT;
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
