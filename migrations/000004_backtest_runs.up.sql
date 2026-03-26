CREATE TABLE backtest_runs (
    id                 UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    backtest_config_id UUID        NOT NULL REFERENCES backtest_configs (id) ON DELETE CASCADE,
    metrics            JSONB       NOT NULL,
    trade_log          JSONB       NOT NULL,
    equity_curve       JSONB       NOT NULL,
    run_timestamp      TIMESTAMPTZ NOT NULL,
    duration_ns        BIGINT      NOT NULL CHECK (duration_ns >= 0),
    prompt_version     TEXT        NOT NULL,
    prompt_version_hash TEXT       NOT NULL,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_backtest_runs_run_timestamp ON backtest_runs (run_timestamp);
CREATE INDEX idx_backtest_runs_prompt_version ON backtest_runs (prompt_version);
CREATE INDEX idx_backtest_runs_prompt_version_hash ON backtest_runs (prompt_version_hash);
CREATE INDEX idx_backtest_runs_config_run_ts_id
    ON backtest_runs (backtest_config_id, run_timestamp DESC, id DESC);
CREATE INDEX idx_backtest_runs_config_prompt_run_ts_id
    ON backtest_runs (backtest_config_id, prompt_version, run_timestamp DESC, id DESC);
CREATE INDEX idx_backtest_runs_config_prompt_hash_run_ts_id
    ON backtest_runs (backtest_config_id, prompt_version_hash, run_timestamp DESC, id DESC);
