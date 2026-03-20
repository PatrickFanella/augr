---
title: "Database Schema"
date: 2026-03-20
tags: [database, postgresql, schema, data-model]
---

# Database Schema

All persistent state lives in PostgreSQL. The schema is organized into four domains: **trading**, **agents**, **market data**, and **system**.

## Entity Relationship Overview

```
strategies ──1:N──► pipeline_runs ──1:N──► agent_decisions
                         │
                         ├──1:1──► trade_signals
                         │              │
                         │         orders ◄── positions
                         │
                    agent_memories

market_data_cache (independent, time-partitioned)
```

## Trading Domain

### `strategies`

```sql
CREATE TABLE strategies (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name          TEXT NOT NULL,
    description   TEXT,
    ticker        TEXT NOT NULL,
    market_type   TEXT NOT NULL CHECK (market_type IN ('stock', 'crypto', 'polymarket')),
    schedule_cron TEXT,                    -- e.g., '0 9 30 * * 1-5' (market open)
    config        JSONB NOT NULL DEFAULT '{}',  -- strategy-specific parameters
    is_active     BOOLEAN NOT NULL DEFAULT true,
    is_paper      BOOLEAN NOT NULL DEFAULT true, -- paper vs. live trading
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### `orders`

```sql
CREATE TABLE orders (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id     UUID REFERENCES strategies(id),
    pipeline_run_id UUID REFERENCES pipeline_runs(id),
    external_id     TEXT,                  -- broker order ID
    ticker          TEXT NOT NULL,
    side            TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    order_type      TEXT NOT NULL CHECK (order_type IN ('market', 'limit', 'stop', 'stop_limit')),
    quantity         NUMERIC NOT NULL,
    limit_price     NUMERIC,
    stop_price      NUMERIC,
    filled_quantity NUMERIC DEFAULT 0,
    filled_avg_price NUMERIC,
    status          TEXT NOT NULL DEFAULT 'pending'
                    CHECK (status IN ('pending', 'submitted', 'partial', 'filled', 'cancelled', 'rejected')),
    broker          TEXT NOT NULL,         -- 'alpaca', 'binance', 'polymarket', 'paper'
    submitted_at    TIMESTAMPTZ,
    filled_at       TIMESTAMPTZ,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_orders_strategy ON orders(strategy_id);
CREATE INDEX idx_orders_status ON orders(status) WHERE status NOT IN ('filled', 'cancelled', 'rejected');
```

### `positions`

```sql
CREATE TABLE positions (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id   UUID REFERENCES strategies(id),
    ticker        TEXT NOT NULL,
    side          TEXT NOT NULL CHECK (side IN ('long', 'short')),
    quantity      NUMERIC NOT NULL,
    avg_entry     NUMERIC NOT NULL,
    current_price NUMERIC,
    unrealized_pnl NUMERIC,
    realized_pnl  NUMERIC DEFAULT 0,
    stop_loss     NUMERIC,
    take_profit   NUMERIC,
    opened_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at     TIMESTAMPTZ,
    UNIQUE (strategy_id, ticker, side) WHERE closed_at IS NULL  -- one open position per side
);
```

### `trades`

```sql
CREATE TABLE trades (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id    UUID REFERENCES orders(id),
    position_id UUID REFERENCES positions(id),
    ticker      TEXT NOT NULL,
    side        TEXT NOT NULL,
    quantity    NUMERIC NOT NULL,
    price       NUMERIC NOT NULL,
    fee         NUMERIC DEFAULT 0,
    executed_at TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Agent Domain

### `pipeline_runs`

```sql
CREATE TABLE pipeline_runs (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id  UUID REFERENCES strategies(id),
    ticker       TEXT NOT NULL,
    trade_date   DATE NOT NULL,
    status       TEXT NOT NULL DEFAULT 'running'
                 CHECK (status IN ('running', 'completed', 'failed', 'cancelled')),
    signal       TEXT CHECK (signal IN ('buy', 'sell', 'hold')),
    started_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    error_message TEXT,
    config_snapshot JSONB                 -- frozen config at run time
) PARTITION BY RANGE (started_at);

-- Monthly partitions
CREATE TABLE pipeline_runs_2026_03 PARTITION OF pipeline_runs
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
```

### `agent_decisions`

Stores every intermediate output from every agent in the pipeline.

```sql
CREATE TABLE agent_decisions (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_run_id UUID NOT NULL REFERENCES pipeline_runs(id),
    agent_role      TEXT NOT NULL,         -- 'market_analyst', 'bull_researcher', 'risk_manager', etc.
    phase           TEXT NOT NULL,         -- 'analysis', 'research_debate', 'trading', 'risk_debate'
    round_number    INT,                   -- debate round (NULL for non-debate agents)
    input_summary   TEXT,                  -- what the agent received
    output_text     TEXT NOT NULL,         -- full agent response
    output_structured JSONB,              -- parsed structured data (trade plan, signal, etc.)
    llm_provider    TEXT,
    llm_model       TEXT,
    prompt_tokens   INT,
    completion_tokens INT,
    latency_ms      INT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (created_at);

CREATE INDEX idx_agent_decisions_run ON agent_decisions(pipeline_run_id);
CREATE INDEX idx_agent_decisions_role ON agent_decisions(agent_role);
```

### `agent_memories`

Replaces the in-memory BM25 system with PostgreSQL full-text search.

```sql
CREATE TABLE agent_memories (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_role      TEXT NOT NULL,         -- 'bull', 'bear', 'trader', 'invest_judge', 'risk_manager'
    situation       TEXT NOT NULL,         -- the scenario description
    situation_tsv   TSVECTOR GENERATED ALWAYS AS (to_tsvector('english', situation)) STORED,
    recommendation  TEXT NOT NULL,         -- the learned recommendation
    outcome         TEXT,                  -- what actually happened
    pipeline_run_id UUID REFERENCES pipeline_runs(id),
    relevance_score NUMERIC,              -- optional manual relevance weight
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_memories_tsv ON agent_memories USING GIN (situation_tsv);
CREATE INDEX idx_memories_role ON agent_memories(agent_role);
```

**Memory retrieval query** (replaces BM25):

```sql
SELECT situation, recommendation, outcome,
       ts_rank(situation_tsv, plainto_tsquery('english', $1)) AS rank
FROM agent_memories
WHERE agent_role = $2
  AND situation_tsv @@ plainto_tsquery('english', $1)
ORDER BY rank DESC
LIMIT 5;
```

## Market Data Domain

### `market_data_cache`

```sql
CREATE TABLE market_data_cache (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticker     TEXT NOT NULL,
    provider   TEXT NOT NULL,             -- 'polygon', 'alphavantage', 'yahoo'
    data_type  TEXT NOT NULL,             -- 'ohlcv', 'fundamentals', 'news', 'indicators'
    timeframe  TEXT,                      -- '1d', '1h', '5m'
    date_from  DATE,
    date_to    DATE,
    data       JSONB NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX idx_mdc_lookup ON market_data_cache(ticker, data_type, provider, date_from, date_to);
```

## System Domain

### `audit_log`

```sql
CREATE TABLE audit_log (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type TEXT NOT NULL,             -- 'pipeline_start', 'order_submit', 'circuit_breaker', etc.
    entity_type TEXT,
    entity_id  UUID,
    actor      TEXT DEFAULT 'system',
    details    JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
) PARTITION BY RANGE (created_at);
```

### `configurations`

```sql
CREATE TABLE configurations (
    key        TEXT PRIMARY KEY,
    value      JSONB NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

## Migration Strategy

- Use `golang-migrate/migrate` with numbered SQL migration files
- All schema changes are versioned and reversible
- Migration directory: `migrations/`
- Naming: `000001_create_strategies.up.sql` / `000001_create_strategies.down.sql`

---

**Related:** [[system-architecture]] · [[memory-and-learning]] · [[go-project-structure]]
