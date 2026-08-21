DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM financial_job_effect_claims) OR EXISTS (SELECT 1 FROM financial_job_lease_events) OR EXISTS (SELECT 1 FROM financial_job_occurrences) OR EXISTS (SELECT 1 FROM financial_job_definitions) THEN
        RAISE EXCEPTION 'migration 98 rollback refused: financial scheduler evidence exists';
    END IF;
END $$;

DROP TABLE financial_job_effect_claims;
DROP TABLE financial_job_lease_events;
DROP TABLE financial_job_occurrences;
DROP TABLE financial_job_definitions;
DROP FUNCTION validate_financial_job_effect_claim();
DROP FUNCTION validate_financial_job_lease_event();
DROP FUNCTION reject_financial_scheduler_mutation();
