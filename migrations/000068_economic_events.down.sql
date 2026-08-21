LOCK TABLE economic_event_normalizations, economic_source_events, option_contract_terms
    IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM economic_event_normalizations) OR
       EXISTS (SELECT 1 FROM economic_source_events) OR
       EXISTS (SELECT 1 FROM option_contract_terms) OR
       EXISTS (SELECT 1 FROM ledger_transactions WHERE origin_type = 'economic_source_event') THEN
        RAISE EXCEPTION 'cannot roll back migration 68 while economic event facts exist';
    END IF;
END;
$$;

DROP TABLE economic_event_normalizations;
DROP TABLE option_contract_terms;
DROP TABLE economic_source_events;

DROP FUNCTION validate_option_contract_terms_insert();
DROP FUNCTION validate_economic_normalization_row();
DROP FUNCTION assert_economic_normalization(UUID);
DROP FUNCTION assert_economic_posting(UUID, UUID, TEXT, TEXT, TEXT, TEXT, TEXT, NUMERIC);
DROP FUNCTION reject_economic_event_mutation();
DROP FUNCTION economic_deterministic_uuid(TEXT, TEXT[]);
