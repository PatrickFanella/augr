-- Speeds up stale-run reconciler query (pipeline_runs WHERE status = 'running')
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pipeline_runs_status_started
    ON pipeline_runs(status, started_at)
    WHERE status = 'running';

-- Speeds up automation health endpoint queries (recent runs per job)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_automation_job_runs_job_name_started
    ON automation_job_runs(job_name, started_at DESC);

-- Speeds up run detail loading (agent decisions by run)
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_agent_decisions_run_id
    ON agent_decisions(run_id);
