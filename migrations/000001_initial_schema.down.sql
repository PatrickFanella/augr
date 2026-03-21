-- 000001_initial_schema.down.sql
-- Rollback: drop all tables, indexes, partitions, triggers, functions, and enum types.

-- ============================================================================
-- DROP TRIGGERS AND FUNCTIONS
-- ============================================================================

-- Wrapped in a DO block so the DROP TRIGGER does not error when the table is
-- already gone (IF EXISTS only guards the trigger, not the relation lookup).
DO $$
BEGIN
    IF to_regclass('public.agent_memories') IS NOT NULL THEN
        EXECUTE 'DROP TRIGGER IF EXISTS trg_agent_memories_tsv ON agent_memories;';
    END IF;
END
$$;
DROP FUNCTION IF EXISTS agent_memories_tsv_trigger();

-- ============================================================================
-- DROP TABLES (order matters due to FK dependencies)
-- ============================================================================

DROP TABLE IF EXISTS audit_log CASCADE;
DROP TABLE IF EXISTS market_data_cache CASCADE;
DROP TABLE IF EXISTS agent_memories CASCADE;
DROP TABLE IF EXISTS trades CASCADE;
DROP TABLE IF EXISTS positions CASCADE;
DROP TABLE IF EXISTS orders CASCADE;
DROP TABLE IF EXISTS agent_decisions CASCADE;
DROP TABLE IF EXISTS pipeline_runs CASCADE;
DROP TABLE IF EXISTS strategies CASCADE;

-- ============================================================================
-- DROP ENUM TYPES
-- ============================================================================

DROP TYPE IF EXISTS position_side;
DROP TYPE IF EXISTS market_type;
DROP TYPE IF EXISTS order_type;
DROP TYPE IF EXISTS trade_side;
DROP TYPE IF EXISTS order_status;
DROP TYPE IF EXISTS pipeline_status;
