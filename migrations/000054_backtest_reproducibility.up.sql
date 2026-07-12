ALTER TABLE backtest_runs
    ADD COLUMN IF NOT EXISTS simulation_version TEXT NOT NULL DEFAULT 'legacy-unversioned',
    ADD COLUMN IF NOT EXISTS input_hash TEXT NOT NULL DEFAULT '';

CREATE INDEX IF NOT EXISTS idx_backtest_runs_input_hash ON backtest_runs(input_hash) WHERE input_hash <> '';
