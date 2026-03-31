-- Add lifecycle status fields to strategies while preserving is_active temporarily
-- for backward compatibility with existing application code.
ALTER TABLE strategies
    ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
    ADD COLUMN skip_next_run BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE strategies
SET status = 'inactive'
WHERE is_active = FALSE;

CREATE OR REPLACE FUNCTION sync_strategy_status_with_is_active() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        IF NEW.status = 'active' AND NEW.is_active = FALSE THEN
            NEW.status := 'inactive';
        END IF;
        RETURN NEW;
    END IF;

    IF NEW.status IS NOT DISTINCT FROM OLD.status AND NEW.is_active IS DISTINCT FROM OLD.is_active THEN
        NEW.status := CASE
            WHEN NEW.is_active THEN 'active'
            ELSE 'inactive'
        END;
    ELSIF NEW.is_active IS NOT DISTINCT FROM OLD.is_active AND NEW.status IS DISTINCT FROM OLD.status AND NEW.status IN ('active', 'inactive') THEN
        NEW.is_active := (NEW.status = 'active');
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_strategies_sync_status_with_is_active
    BEFORE INSERT OR UPDATE OF is_active, status ON strategies
    FOR EACH ROW EXECUTE FUNCTION sync_strategy_status_with_is_active();

COMMENT ON COLUMN strategies.is_active IS 'Deprecated: use status instead.';
