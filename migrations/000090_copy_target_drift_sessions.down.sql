LOCK TABLE copy_target_drift_runs IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM copy_target_drift_runs) THEN
        RAISE EXCEPTION 'cannot roll back copy target drift sessions with retained evidence';
    END IF;
END;
$$;

DROP INDEX idx_copy_target_drift_origin_session;
DROP TRIGGER copy_target_drift_legs_append_only ON copy_target_drift_legs;
DROP TRIGGER copy_target_drift_runs_append_only ON copy_target_drift_runs;
DROP FUNCTION reject_copy_target_drift_mutation();
DROP TRIGGER copy_target_drift_graph_guard ON copy_target_drift_runs;
DROP FUNCTION validate_copy_target_drift_graph();
DROP TRIGGER copy_target_drift_leg_guard ON copy_target_drift_legs;
DROP FUNCTION validate_copy_target_drift_leg();
DROP TRIGGER copy_target_drift_run_guard ON copy_target_drift_runs;
DROP FUNCTION validate_copy_target_drift_run();
DROP TABLE copy_target_drift_legs;
DROP TABLE copy_target_drift_runs;

