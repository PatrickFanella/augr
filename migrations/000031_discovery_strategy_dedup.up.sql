-- Collapse existing duplicates for discovery-generated paper strategies,
-- preserving the earliest created row for each logical strategy key.
CREATE TEMP TABLE _strategy_dedup_map ON COMMIT DROP AS
WITH ranked AS (
    SELECT id,
           FIRST_VALUE(id) OVER (
               PARTITION BY ticker, market_type, is_paper, name
               ORDER BY created_at ASC, id ASC
           ) AS keeper_id,
           ROW_NUMBER() OVER (
               PARTITION BY ticker, market_type, is_paper, name
               ORDER BY created_at ASC, id ASC
           ) AS rn
      FROM strategies
     WHERE is_paper = true
       AND (name LIKE 'discovery:%' OR name LIKE 'options:%')
)
SELECT id AS duplicate_id, keeper_id
  FROM ranked
 WHERE rn > 1;

UPDATE backtest_configs bc
   SET strategy_id = d.keeper_id
  FROM _strategy_dedup_map d
 WHERE bc.strategy_id = d.duplicate_id;

UPDATE orders o
   SET strategy_id = d.keeper_id
  FROM _strategy_dedup_map d
 WHERE o.strategy_id = d.duplicate_id;

UPDATE positions p
   SET strategy_id = d.keeper_id
  FROM _strategy_dedup_map d
 WHERE p.strategy_id = d.duplicate_id;

UPDATE report_artifacts ra
   SET strategy_id = d.keeper_id
  FROM _strategy_dedup_map d
 WHERE ra.strategy_id = d.duplicate_id;

UPDATE pipeline_runs pr
   SET strategy_id = d.keeper_id
  FROM _strategy_dedup_map d
 WHERE pr.strategy_id = d.duplicate_id;

UPDATE agent_events ae
   SET strategy_id = d.keeper_id
  FROM _strategy_dedup_map d
 WHERE ae.strategy_id = d.duplicate_id;

DELETE FROM strategies s
 USING _strategy_dedup_map d
 WHERE s.id = d.duplicate_id;

-- Enforce idempotency for discovery deploy runs.
CREATE UNIQUE INDEX IF NOT EXISTS idx_strategies_discovery_unique
    ON strategies (ticker, market_type, is_paper, name)
    WHERE is_paper = true
      AND (name LIKE 'discovery:%' OR name LIKE 'options:%');
