-- Add lifecycle status fields to strategies while preserving is_active temporarily
-- for backward compatibility with existing application code.
ALTER TABLE strategies
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN skip_next_run BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE strategies
SET status = CASE
    WHEN is_active THEN 'active'
    ELSE 'inactive'
END;

COMMENT ON COLUMN strategies.is_active IS 'Deprecated: use status instead.';
