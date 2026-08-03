\pset format unaligned
\pset tuples_only on
\pset pager off
\set ON_ERROR_STOP on
SET search_path = public, pg_catalog;

SELECT (
    :'reindex_approved' = '1'
    AND :'repair_completed' = '1'
    AND length(:'operator') > 0
) AS all_approved
\gset

\if :all_approved
REINDEX TABLE historical_ohlcv;
REINDEX TABLE universe_tickers;
REINDEX TABLE news_feed;

DO $verify$
DECLARE
    invalid_indexes integer;
    h_dups integer;
    u_dups integer;
    n_dups integer;
    bf_rows integer;
    bio_rows integer;
BEGIN
    SELECT count(*) INTO invalid_indexes
    FROM pg_index i
    JOIN pg_class t ON t.oid = i.indrelid
    JOIN pg_namespace n ON n.oid = t.relnamespace
    WHERE n.nspname = 'public'
      AND t.relname IN ('historical_ohlcv', 'universe_tickers', 'news_feed')
      AND (NOT i.indisvalid OR NOT i.indisready);

    SET LOCAL enable_indexscan = off;
    SET LOCAL enable_indexonlyscan = off;
    SET LOCAL enable_bitmapscan = off;
    SELECT count(*) INTO h_dups FROM (
        SELECT 1 FROM historical_ohlcv GROUP BY ticker, provider, timeframe, bar_time HAVING count(*) > 1
    ) d;
    SELECT count(*) INTO u_dups FROM (
        SELECT 1 FROM universe_tickers GROUP BY ticker HAVING count(*) > 1
    ) d;
    SELECT count(*) INTO n_dups FROM (
        SELECT 1 FROM news_feed GROUP BY guid HAVING count(*) > 1
    ) d;

    SET LOCAL enable_seqscan = off;
    SET LOCAL enable_indexscan = on;
    SET LOCAL enable_indexonlyscan = on;
    SET LOCAL enable_bitmapscan = on;
    SELECT count(*) INTO bf_rows FROM universe_tickers WHERE ticker = 'BF.B';
    SELECT count(*) INTO bio_rows FROM universe_tickers WHERE ticker = 'BIO.B';

    IF invalid_indexes <> 0 OR ROW(h_dups, u_dups, n_dups) <> ROW(0, 0, 0)
       OR ROW(bf_rows, bio_rows) <> ROW(1, 1) THEN
        RAISE EXCEPTION 'affected-table reindex validation failed';
    END IF;
END
$verify$;

SELECT t.relname AS table_name, c.relname AS index_name, i.indisvalid, i.indisready
FROM pg_index i
JOIN pg_class c ON c.oid = i.indexrelid
JOIN pg_class t ON t.oid = i.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
WHERE n.nspname = 'public'
  AND t.relname IN ('historical_ohlcv', 'universe_tickers', 'news_feed')
ORDER BY 1, 2;
\else
\echo 'reindex approvals incomplete; no statements executed'
\quit 1
\endif
