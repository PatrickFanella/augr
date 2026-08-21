LOCK TABLE venue_reconciliation_incidents, venue_reconciliation_results, venue_reconciliation_runs,
    venue_local_snapshot_issues, venue_local_snapshot_fills, venue_local_snapshot_positions,
    venue_local_snapshot_transactions, venue_local_snapshots, venue_provider_snapshot_fills,
    venue_provider_snapshot_positions, venue_provider_snapshot_pages, venue_provider_snapshots,
    venue_reconciliation_policy_artifacts IN ACCESS EXCLUSIVE MODE;

DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM venue_reconciliation_policy_artifacts) OR
       EXISTS (SELECT 1 FROM venue_provider_snapshots) OR EXISTS (SELECT 1 FROM venue_local_snapshots) OR
       EXISTS (SELECT 1 FROM venue_reconciliation_runs) THEN
        RAISE EXCEPTION 'cannot roll back migration 75 while venue reconciliation evidence exists';
    END IF;
END;
$$;

DROP TABLE venue_reconciliation_incidents;
DROP TABLE venue_reconciliation_results;
DROP TABLE venue_reconciliation_runs;
DROP TABLE venue_local_snapshot_issues;
DROP TABLE venue_local_snapshot_fills;
DROP TABLE venue_local_snapshot_positions;
DROP TABLE venue_local_snapshot_transactions;
DROP TABLE venue_local_snapshots;
DROP TABLE venue_provider_snapshot_fills;
DROP TABLE venue_provider_snapshot_positions;
DROP TABLE venue_provider_snapshot_pages;
DROP TABLE venue_provider_snapshots;
DROP TABLE venue_reconciliation_policy_artifacts;
DROP FUNCTION validate_venue_reconciliation_graph();
DROP FUNCTION venue_reconciliation_result_identity(TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT,TEXT);
DROP FUNCTION venue_reconciliation_go_json_string(TEXT);
DROP FUNCTION reject_venue_reconciliation_mutation();
