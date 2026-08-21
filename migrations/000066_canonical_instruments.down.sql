DROP TABLE IF EXISTS instrument_identity_quarantine;
DROP TABLE IF EXISTS corporate_actions;
DROP TABLE IF EXISTS venue_contracts;
DROP TABLE IF EXISTS instrument_alias_events;
DROP TABLE IF EXISTS instruments;

DROP FUNCTION IF EXISTS validate_venue_contract_window();
DROP FUNCTION IF EXISTS validate_instrument_alias_event_transition();
DROP FUNCTION IF EXISTS reject_instrument_reference_mutation();
