ALTER TABLE backtest_configs
    ADD COLUMN IF NOT EXISTS schedule_cron TEXT NOT NULL DEFAULT '';
