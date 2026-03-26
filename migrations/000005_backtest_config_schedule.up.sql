ALTER TABLE backtest_configs
    ADD COLUMN schedule_cron TEXT NOT NULL DEFAULT '';
