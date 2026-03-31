-- Store raw pipeline input snapshots for post-hoc analysis and debugging.
-- pipeline_run_id intentionally has no formal FK because pipeline_runs is
-- partitioned with a composite primary key that includes the partition key.
CREATE TABLE pipeline_run_snapshots (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    pipeline_run_id UUID        NOT NULL,
    data_type       TEXT        NOT NULL CHECK (data_type IN ('market', 'news', 'fundamentals', 'social')),
    payload         JSONB       NOT NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_pipeline_run_snapshots_pipeline_run_id ON pipeline_run_snapshots (pipeline_run_id);
