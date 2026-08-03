\pset format unaligned
\pset tuples_only on
\pset pager off
\set ON_ERROR_STOP on
SET search_path = public, pg_catalog;

SET enable_indexscan = off;
SET enable_indexonlyscan = off;
SET enable_bitmapscan = off;

WITH duplicate_keys AS (
    SELECT ticker, provider, timeframe, bar_time,
           count(*) AS row_count,
           count(DISTINCT ROW(open, high, low, close, volume)) AS payload_variants
    FROM historical_ohlcv
    GROUP BY ticker, provider, timeframe, bar_time
    HAVING count(*) > 1
), historical AS (
    SELECT count(*)::integer AS groups,
           coalesce(sum(row_count - 1), 0)::integer AS excess,
           count(*) FILTER (WHERE payload_variants > 1)::integer AS conflicts
    FROM duplicate_keys
), universe_keys AS (
    SELECT ticker, count(*) AS row_count,
           count(DISTINCT ROW(name, exchange, index_group, watch_score, last_scanned, active)) AS payload_variants
    FROM universe_tickers
    GROUP BY ticker
    HAVING count(*) > 1
), universe AS (
    SELECT count(*)::integer AS groups,
           coalesce(sum(row_count - 1), 0)::integer AS excess,
           count(*) FILTER (WHERE payload_variants > 1)::integer AS conflicts,
           count(*) FILTER (WHERE ticker = 'BF.B')::integer AS bf_groups,
           count(*) FILTER (WHERE ticker = 'BIO.B')::integer AS bio_groups,
           count(*) FILTER (WHERE ticker NOT IN ('BF.B', 'BIO.B'))::integer AS unexpected_groups
    FROM universe_keys
), news_group_stats AS (
    SELECT n.guid, count(*) AS row_count,
           count(DISTINCT ROW(source, title, description, link, published_at)) AS identity_variants,
           count(DISTINCT ROW(category, sentiment, relevance, summary, tickers)) AS enrichment_variants,
           bool_and(embedding IS NULL) AS embeddings_all_null
    FROM news_feed n
    GROUP BY n.guid
    HAVING count(*) > 1
), latest_news AS (
    SELECT guid, category, sentiment, relevance, summary
    FROM (
        SELECT n.*,
               count(*) OVER (PARTITION BY guid) AS group_count,
               row_number() OVER (PARTITION BY guid ORDER BY created_at DESC, id DESC) AS row_rank
        FROM news_feed n
    ) ranked
    WHERE group_count > 1 AND row_rank = 1
), news AS (
    SELECT count(*)::integer AS groups,
           coalesce(sum(s.row_count - 1), 0)::integer AS excess,
           count(*) FILTER (WHERE s.identity_variants > 1)::integer AS identity_conflicts,
           count(*) FILTER (WHERE s.enrichment_variants > 1)::integer AS enrichment_conflicts,
           count(*) FILTER (WHERE s.embeddings_all_null)::integer AS all_null_embedding_groups,
           count(*) FILTER (
               WHERE l.category IS NOT NULL AND btrim(l.category) <> ''
                 AND l.sentiment IS NOT NULL AND btrim(l.sentiment) <> ''
                 AND l.relevance IS NOT NULL
                 AND l.summary IS NOT NULL AND btrim(l.summary) <> ''
           )::integer AS latest_complete_groups
    FROM news_group_stats s
    JOIN latest_news l USING (guid)
), evidence AS (
    SELECT h.groups AS historical_groups,
           h.excess AS historical_excess,
           h.conflicts AS historical_conflicts,
           u.groups AS universe_groups,
           u.excess AS universe_excess,
           u.conflicts AS universe_conflicts,
           u.bf_groups,
           u.bio_groups,
           u.unexpected_groups,
           n.groups AS news_groups,
           n.excess AS news_excess,
           n.identity_conflicts AS news_identity_conflicts,
           n.enrichment_conflicts AS news_enrichment_conflicts,
           n.all_null_embedding_groups,
           n.latest_complete_groups
    FROM historical h CROSS JOIN universe u CROSS JOIN news n
)
SELECT *,
       historical_groups = 1253 AND historical_excess = 1253 AND historical_conflicts = 0
       AND universe_groups = 2 AND universe_excess = 2 AND universe_conflicts = 1
       AND bf_groups = 1 AND bio_groups = 1 AND unexpected_groups = 0
       AND news_groups = 40 AND news_excess = 40 AND news_identity_conflicts = 0
       AND news_enrichment_conflicts = 40 AND all_null_embedding_groups = 40
       AND latest_complete_groups = 40 AS preflight_ok
FROM evidence;

SELECT ticker, name, exchange, index_group, watch_score, last_scanned, active,
       created_at, updated_at, ctid
FROM universe_tickers
WHERE ticker IN ('BF.B', 'BIO.B')
ORDER BY ticker, updated_at DESC NULLS LAST, created_at DESC, ctid DESC;
