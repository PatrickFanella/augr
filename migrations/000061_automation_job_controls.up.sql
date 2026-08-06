CREATE TABLE IF NOT EXISTS automation_job_controls (
    job_name TEXT PRIMARY KEY CHECK (btrim(job_name) <> ''),
    enabled BOOLEAN NOT NULL,
    updated_by TEXT NOT NULL CHECK (btrim(updated_by) <> ''),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
