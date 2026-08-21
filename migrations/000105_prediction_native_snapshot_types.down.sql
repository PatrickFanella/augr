ALTER TABLE pipeline_run_snapshots
    DROP CONSTRAINT IF EXISTS pipeline_run_snapshots_data_type_check;

ALTER TABLE pipeline_run_snapshots
    ADD CONSTRAINT pipeline_run_snapshots_data_type_check
    CHECK (data_type IN ('market', 'news', 'fundamentals', 'social'));
