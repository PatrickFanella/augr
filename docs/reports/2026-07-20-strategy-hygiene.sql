-- Read-only strategy hygiene inventory for production tradingagent.
-- SELECT-only bundle for stale active strategies and duplicate active ticker groups.

WITH active_strategies AS (
  SELECT
    s.id,
    s.name,
    s.ticker,
    s.market_type,
    s.schedule_cron,
    s.status,
    s.skip_next_run,
    s.is_paper,
    s.is_active,
    s.created_at,
    s.updated_at,
    max(pr.started_at) AS last_run_at
  FROM strategies s
  LEFT JOIN pipeline_runs pr ON pr.strategy_id = s.id
  WHERE s.is_active = TRUE
  GROUP BY
    s.id,
    s.name,
    s.ticker,
    s.market_type,
    s.schedule_cron,
    s.status,
    s.skip_next_run,
    s.is_paper,
    s.is_active,
    s.created_at,
    s.updated_at
)
SELECT
  'stale_active_strategies' AS section,
  id,
  name,
  ticker,
  market_type,
  schedule_cron,
  status,
  skip_next_run,
  is_paper,
  is_active,
  updated_at,
  last_run_at
FROM active_strategies
WHERE last_run_at IS NULL OR last_run_at < NOW() - INTERVAL '7 days'
ORDER BY last_run_at NULLS FIRST, updated_at DESC, id;

WITH duplicate_active_groups AS (
  SELECT
    market_type,
    ticker,
    count(*) AS active_strategy_count,
    array_agg(id ORDER BY updated_at DESC) AS strategy_ids,
    array_agg(name ORDER BY updated_at DESC) AS strategy_names,
    array_agg(status ORDER BY updated_at DESC) AS strategy_statuses,
    array_agg(schedule_cron ORDER BY updated_at DESC) AS strategy_schedule_crons,
    array_agg(skip_next_run ORDER BY updated_at DESC) AS strategy_skip_next_runs,
    array_agg(is_paper ORDER BY updated_at DESC) AS strategy_is_papers,
    array_agg(created_at ORDER BY updated_at DESC) AS strategy_created_ats,
    array_agg(updated_at ORDER BY updated_at DESC) AS strategy_updated_ats,
    array_agg(is_active ORDER BY updated_at DESC) AS strategy_is_actives
  FROM strategies
  WHERE is_active = TRUE
  GROUP BY market_type, ticker
  HAVING count(*) > 1
)
SELECT
  'duplicate_active_tickers' AS section,
  market_type,
  ticker,
  active_strategy_count,
  strategy_ids,
  strategy_names,
  strategy_statuses,
  strategy_schedule_crons,
  strategy_skip_next_runs,
  strategy_is_papers,
  strategy_is_actives
FROM duplicate_active_groups
ORDER BY active_strategy_count DESC, market_type, ticker;
