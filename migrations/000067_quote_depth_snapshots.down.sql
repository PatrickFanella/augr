DROP TABLE IF EXISTS quote_depth_levels;
DROP TABLE IF EXISTS quote_snapshots;

DROP FUNCTION IF EXISTS validate_quote_depth_level_row();
DROP FUNCTION IF EXISTS validate_quote_snapshot_depth_row();
DROP FUNCTION IF EXISTS assert_quote_snapshot_depth(UUID);
DROP FUNCTION IF EXISTS validate_quote_snapshot_venue_contract();
DROP FUNCTION IF EXISTS reject_quote_snapshot_mutation();
