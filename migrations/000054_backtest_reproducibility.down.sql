DROP INDEX IF EXISTS idx_backtest_runs_input_hash;
ALTER TABLE backtest_runs
    DROP COLUMN IF EXISTS input_hash,
    DROP COLUMN IF EXISTS simulation_version;
