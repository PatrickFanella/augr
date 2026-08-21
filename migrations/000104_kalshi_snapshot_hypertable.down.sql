DO $migration$
BEGIN
    IF to_regclass('public.kalshi_market_snapshots_partitioned') IS NULL THEN
        RAISE EXCEPTION
            'migration 104 cannot be reversed automatically after cutover; restore the archived table using the retention runbook';
    END IF;

    DROP TABLE kalshi_market_snapshots_partitioned;
END
$migration$;
