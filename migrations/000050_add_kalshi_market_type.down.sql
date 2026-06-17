-- PostgreSQL enum values cannot be safely removed without rebuilding the enum.
-- Rollback is intentionally a no-op; strategies with market_type='kalshi'
-- must be removed or migrated before attempting a manual enum rebuild.
SELECT 1;
