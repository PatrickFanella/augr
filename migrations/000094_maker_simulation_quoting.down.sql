LOCK TABLE maker_quote_candidates IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM maker_quote_candidates) THEN RAISE EXCEPTION 'cannot roll back maker simulation and quoting with retained evidence'; END IF; END; $$;
DROP FUNCTION reject_maker_quote_mutation() CASCADE;
DROP FUNCTION validate_maker_quote_graph() CASCADE;
DROP FUNCTION validate_maker_quote_scenario() CASCADE;
DROP FUNCTION validate_maker_quote_parent() CASCADE;
DROP TABLE maker_quote_scenarios;
DROP TABLE maker_quote_candidates;
