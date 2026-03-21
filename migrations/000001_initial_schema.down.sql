-- 000001_initial_schema.down.sql
-- Rollback: drop all tables, indexes, partitions, and enum types.

-- ============================================================================
-- DROP TABLES (order matters due to dependencies)
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

DROP TYPE IF EXISTS market_type;
DROP TYPE IF EXISTS order_type;
DROP TYPE IF EXISTS trade_side;
DROP TYPE IF EXISTS order_status;
DROP TYPE IF EXISTS pipeline_status;
