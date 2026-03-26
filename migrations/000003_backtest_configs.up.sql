CREATE TABLE backtest_configs (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    strategy_id       UUID        NOT NULL REFERENCES strategies (id),
    name              TEXT        NOT NULL,
    description       TEXT        NOT NULL DEFAULT '',
    start_date        DATE        NOT NULL,
    end_date          DATE        NOT NULL,
    simulation_params JSONB       NOT NULL DEFAULT '{}',
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_backtest_configs_strategy_id ON backtest_configs (strategy_id);
CREATE INDEX idx_backtest_configs_created_at ON backtest_configs (created_at);
