LOCK TABLE capacity_v1_tiers,capacity_v1_families,capacity_v1_comparisons,capacity_v1_contracts IN ACCESS EXCLUSIVE MODE;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM capacity_v1_comparisons) OR EXISTS(SELECT 1 FROM capacity_v1_contracts) THEN RAISE EXCEPTION 'cannot roll back migration 87 while capacity v1 evidence exists';END IF;END$$;
DROP TABLE capacity_v1_tiers,capacity_v1_families,capacity_v1_comparisons,capacity_v1_contracts;
DROP FUNCTION reject_capacity_v1_mutation();DROP FUNCTION validate_capacity_v1_comparison();
