ALTER TABLE strategies
    DROP COLUMN IF EXISTS skip_next_run,
    DROP COLUMN IF EXISTS status;

COMMENT ON COLUMN strategies.is_active IS NULL;
