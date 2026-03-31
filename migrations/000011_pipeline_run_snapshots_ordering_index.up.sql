-- Add an ordering index for fetching snapshots by run ordered by created_at, id.
CREATE INDEX idx_pipeline_run_snapshots_pipeline_run_id_created_at_id
    ON pipeline_run_snapshots (pipeline_run_id, created_at, id);
