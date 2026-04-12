ALTER TABLE automation_job_runs
    DROP COLUMN IF EXISTS last_error_at,
    DROP COLUMN IF EXISTS consecutive_failures;
