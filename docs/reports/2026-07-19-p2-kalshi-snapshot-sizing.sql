-- Read-only sizing, growth, and benchmark queries for kalshi_market_snapshots.
-- No mutations, retention activation, compression activation, or DDL.

\echo 'warning: the final benchmark may exceed a nominal 120s client timeout; run it only when the operator can wait for completion.'

SELECT pg_size_pretty(pg_total_relation_size('kalshi_market_snapshots')) AS total_size,
       pg_size_pretty(pg_relation_size('kalshi_market_snapshots')) AS heap_size,
       pg_size_pretty(pg_indexes_size('kalshi_market_snapshots')) AS index_size,
       count(*) AS row_count,
       min(captured_at) AS oldest_snapshot_at,
       max(captured_at) AS newest_snapshot_at
FROM kalshi_market_snapshots;

SELECT date_trunc('day', captured_at) AS day,
       count(*) AS snapshots,
       round(avg(pg_column_size(raw))) AS avg_raw_bytes,
       round(sum(pg_column_size(raw))) AS total_raw_bytes
FROM kalshi_market_snapshots
WHERE captured_at >= NOW() - INTERVAL '90 days'
GROUP BY 1
ORDER BY 1;

SELECT ticker,
       count(*) AS snapshots_30d,
       round(avg(pg_column_size(raw))) AS avg_raw_bytes,
       max(captured_at) AS last_seen_at
FROM kalshi_market_snapshots
WHERE captured_at >= NOW() - INTERVAL '30 days'
GROUP BY ticker
ORDER BY snapshots_30d DESC, ticker
LIMIT 50;

EXPLAIN (ANALYZE, BUFFERS)
SELECT *
FROM kalshi_market_snapshots
WHERE ticker = 'KX2028DRUN-28-JTAL'
ORDER BY captured_at DESC
LIMIT 1;

EXPLAIN (ANALYZE, BUFFERS)
SELECT count(*)
FROM kalshi_market_snapshots
WHERE captured_at >= NOW() - INTERVAL '7 days';

EXPLAIN (ANALYZE, BUFFERS)
SELECT ticker, count(*)
FROM kalshi_market_snapshots
WHERE captured_at >= NOW() - INTERVAL '30 days'
GROUP BY ticker;
