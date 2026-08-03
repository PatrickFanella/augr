\pset format unaligned
\pset tuples_only on
\pset pager off
\set ON_ERROR_STOP on
SET search_path = public, pg_catalog;

SELECT (
    :'execute_repair' = '1'
    AND :'repair_approved' = '1'
    AND :'repair_write_quiesce_approved' = '1'
    AND :'repair_physical_backup_approved' = '1'
    AND :'external_audit_verified' = '1'
    AND length(:'backup_artifact_id') > 0
    AND length(:'backup_checksum') > 0
    AND length(:'external_audit_checksum') > 0
    AND length(:'operator') > 0
    AND length(:'script_git_hash') > 0
    AND length(:'utc_started_at') > 0
) AS all_approved
\gset

\if :all_approved
BEGIN;
SET LOCAL enable_indexscan = off;
SET LOCAL enable_indexonlyscan = off;
SET LOCAL enable_bitmapscan = off;
LOCK TABLE historical_ohlcv IN ACCESS EXCLUSIVE MODE;
LOCK TABLE universe_tickers IN ACCESS EXCLUSIVE MODE;
LOCK TABLE news_feed IN ACCESS EXCLUSIVE MODE;

DO $preflight$
DECLARE
    h_groups integer;
    h_excess integer;
    h_conflicts integer;
    u_groups integer;
    u_excess integer;
    u_conflicts integer;
    u_bf integer;
    u_bio integer;
    u_unexpected integer;
    n_groups integer;
    n_excess integer;
    n_identity_conflicts integer;
    n_enrichment_conflicts integer;
    n_embedding_null integer;
    n_latest_complete integer;
BEGIN
    SELECT count(*)::integer,
           coalesce(sum(row_count - 1), 0)::integer,
           count(*) FILTER (WHERE payload_variants > 1)::integer
    INTO h_groups, h_excess, h_conflicts
    FROM (
        SELECT count(*) AS row_count,
               count(DISTINCT ROW(open, high, low, close, volume)) AS payload_variants
        FROM historical_ohlcv
        GROUP BY ticker, provider, timeframe, bar_time
        HAVING count(*) > 1
    ) d;

    SELECT count(*)::integer,
           coalesce(sum(row_count - 1), 0)::integer,
           count(*) FILTER (WHERE payload_variants > 1)::integer,
           count(*) FILTER (WHERE ticker = 'BF.B')::integer,
           count(*) FILTER (WHERE ticker = 'BIO.B')::integer,
           count(*) FILTER (WHERE ticker NOT IN ('BF.B', 'BIO.B'))::integer
    INTO u_groups, u_excess, u_conflicts, u_bf, u_bio, u_unexpected
    FROM (
        SELECT ticker, count(*) AS row_count,
               count(DISTINCT ROW(name, exchange, index_group, watch_score, last_scanned, active)) AS payload_variants
        FROM universe_tickers
        GROUP BY ticker
        HAVING count(*) > 1
    ) d;

    WITH stats AS (
        SELECT n.guid, count(*) AS row_count,
               count(DISTINCT ROW(source, title, description, link, published_at)) AS identity_variants,
               count(DISTINCT ROW(category, sentiment, relevance, summary, tickers)) AS enrichment_variants,
               bool_and(embedding IS NULL) AS embeddings_all_null
        FROM news_feed n
        GROUP BY n.guid
        HAVING count(*) > 1
    ), latest AS (
        SELECT guid, category, sentiment, relevance, summary
        FROM (
            SELECT n.*,
                   count(*) OVER (PARTITION BY guid) AS group_count,
                   row_number() OVER (PARTITION BY guid ORDER BY created_at DESC, id DESC) AS row_rank
            FROM news_feed n
        ) ranked
        WHERE group_count > 1 AND row_rank = 1
    )
    SELECT count(*)::integer,
           coalesce(sum(s.row_count - 1), 0)::integer,
           count(*) FILTER (WHERE s.identity_variants > 1)::integer,
           count(*) FILTER (WHERE s.enrichment_variants > 1)::integer,
           count(*) FILTER (WHERE s.embeddings_all_null)::integer,
           count(*) FILTER (
               WHERE l.category IS NOT NULL AND btrim(l.category) <> ''
                 AND l.sentiment IS NOT NULL AND btrim(l.sentiment) <> ''
                 AND l.relevance IS NOT NULL
                 AND l.summary IS NOT NULL AND btrim(l.summary) <> ''
           )::integer
    INTO n_groups, n_excess, n_identity_conflicts, n_enrichment_conflicts,
         n_embedding_null, n_latest_complete
    FROM stats s JOIN latest l USING (guid);

    IF ROW(h_groups, h_excess, h_conflicts) <> ROW(1253, 1253, 0)
       OR ROW(u_groups, u_excess, u_conflicts, u_bf, u_bio, u_unexpected) <> ROW(2, 2, 1, 1, 1, 0)
       OR ROW(n_groups, n_excess, n_identity_conflicts, n_enrichment_conflicts, n_embedding_null, n_latest_complete)
          <> ROW(40, 40, 0, 40, 40, 40) THEN
        RAISE EXCEPTION 'source-index repair preflight drifted; aborting';
    END IF;
END
$preflight$;

CREATE TEMP TABLE repair_historical_rank ON COMMIT DROP AS
SELECT row_ctid, row_rank
FROM (
    SELECT h.ctid AS row_ctid,
           count(*) OVER (PARTITION BY h.ticker, h.provider, h.timeframe, h.bar_time) AS group_count,
           row_number() OVER (
               PARTITION BY h.ticker, h.provider, h.timeframe, h.bar_time ORDER BY h.ctid
           ) AS row_rank
    FROM historical_ohlcv h
) ranked
WHERE group_count > 1;

CREATE TEMP TABLE repair_universe_rank ON COMMIT DROP AS
SELECT row_ctid, ticker, name, exchange, index_group, watch_score, last_scanned,
       active, created_at, updated_at, row_rank
FROM (
    SELECT u.ctid AS row_ctid, u.*,
           count(*) OVER (PARTITION BY ticker) AS group_count,
           row_number() OVER (
               PARTITION BY ticker
               ORDER BY updated_at DESC NULLS LAST, created_at DESC, u.ctid DESC
           ) AS row_rank
    FROM universe_tickers u
) ranked
WHERE group_count > 1;

CREATE TEMP TABLE repair_universe_merge ON COMMIT DROP AS
SELECT r.ticker,
       r.row_ctid AS survivor_ctid,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT x.name FROM repair_universe_rank x WHERE x.ticker = r.ticker AND x.name IS NOT NULL
        ORDER BY x.updated_at DESC NULLS LAST, x.created_at DESC, x.row_ctid DESC LIMIT 1) ELSE r.name END AS name,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT x.exchange FROM repair_universe_rank x WHERE x.ticker = r.ticker AND x.exchange IS NOT NULL
        ORDER BY x.updated_at DESC NULLS LAST, x.created_at DESC, x.row_ctid DESC LIMIT 1) ELSE r.exchange END AS exchange,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT x.index_group FROM repair_universe_rank x WHERE x.ticker = r.ticker AND x.index_group IS NOT NULL
        ORDER BY x.updated_at DESC NULLS LAST, x.created_at DESC, x.row_ctid DESC LIMIT 1) ELSE r.index_group END AS index_group,
       CASE WHEN r.ticker = 'BF.B' THEN coalesce(
           (SELECT max(x.watch_score) FROM repair_universe_rank x WHERE x.ticker = r.ticker AND x.watch_score > 0),
           (SELECT max(x.watch_score) FROM repair_universe_rank x WHERE x.ticker = r.ticker),
           0
       ) ELSE r.watch_score END AS watch_score,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT max(x.last_scanned) FROM repair_universe_rank x WHERE x.ticker = r.ticker) ELSE r.last_scanned END AS last_scanned,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT bool_or(x.active) FROM repair_universe_rank x WHERE x.ticker = r.ticker) ELSE r.active END AS active,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT min(x.created_at) FROM repair_universe_rank x WHERE x.ticker = r.ticker) ELSE r.created_at END AS created_at,
       CASE WHEN r.ticker = 'BF.B' THEN (SELECT max(x.updated_at) FROM repair_universe_rank x WHERE x.ticker = r.ticker) ELSE r.updated_at END AS updated_at
FROM repair_universe_rank r
WHERE r.row_rank = 1;

CREATE TEMP TABLE repair_news_rank ON COMMIT DROP AS
SELECT row_ctid, id, guid, source, title, description, link, published_at, tickers,
       category, sentiment, relevance, summary, created_at, embedding, row_rank
FROM (
    SELECT n.ctid AS row_ctid, n.*,
           count(*) OVER (PARTITION BY guid) AS group_count,
           row_number() OVER (PARTITION BY guid ORDER BY created_at DESC, id DESC) AS row_rank
    FROM news_feed n
) ranked
WHERE group_count > 1;

CREATE TEMP TABLE repair_news_merge ON COMMIT DROP AS
SELECT r.guid,
       r.row_ctid AS survivor_ctid,
       (SELECT x.category FROM repair_news_rank x WHERE x.guid = r.guid
          AND x.category IS NOT NULL AND btrim(x.category) <> ''
        ORDER BY x.created_at DESC, x.id DESC LIMIT 1) AS category,
       (SELECT x.sentiment FROM repair_news_rank x WHERE x.guid = r.guid
          AND x.sentiment IS NOT NULL AND btrim(x.sentiment) <> ''
        ORDER BY x.created_at DESC, x.id DESC LIMIT 1) AS sentiment,
       (SELECT x.relevance FROM repair_news_rank x WHERE x.guid = r.guid AND x.relevance IS NOT NULL
        ORDER BY x.created_at DESC, x.id DESC LIMIT 1) AS relevance,
       (SELECT x.summary FROM repair_news_rank x WHERE x.guid = r.guid
          AND x.summary IS NOT NULL AND btrim(x.summary) <> ''
        ORDER BY x.created_at DESC, x.id DESC LIMIT 1) AS summary,
       (SELECT x.embedding FROM repair_news_rank x WHERE x.guid = r.guid AND x.embedding IS NOT NULL
        ORDER BY x.created_at DESC, x.id DESC LIMIT 1) AS embedding,
       (SELECT array_agg(DISTINCT ticker ORDER BY ticker)
        FROM repair_news_rank x
        CROSS JOIN LATERAL unnest(coalesce(x.tickers, ARRAY[]::text[])) ticker
        WHERE x.guid = r.guid) AS tickers
FROM repair_news_rank r
WHERE r.row_rank = 1;

CREATE TEMP TABLE repair_delete_counts (
    historical_deleted integer NOT NULL,
    universe_deleted integer NOT NULL,
    news_deleted integer NOT NULL
) ON COMMIT DROP;

WITH historical_deleted AS (
    DELETE FROM historical_ohlcv h USING repair_historical_rank r
    WHERE h.ctid = r.row_ctid AND r.row_rank > 1 RETURNING 1
), universe_deleted AS (
    DELETE FROM universe_tickers u USING repair_universe_rank r
    WHERE u.ctid = r.row_ctid AND r.row_rank > 1 RETURNING 1
), news_deleted AS (
    DELETE FROM news_feed n USING repair_news_rank r
    WHERE n.ctid = r.row_ctid AND r.row_rank > 1 RETURNING 1
)
INSERT INTO repair_delete_counts
SELECT (SELECT count(*) FROM historical_deleted),
       (SELECT count(*) FROM universe_deleted),
       (SELECT count(*) FROM news_deleted);

UPDATE universe_tickers u
SET name = m.name,
    exchange = m.exchange,
    index_group = m.index_group,
    watch_score = m.watch_score,
    last_scanned = m.last_scanned,
    active = m.active,
    created_at = m.created_at,
    updated_at = m.updated_at
FROM repair_universe_merge m
WHERE u.ctid = m.survivor_ctid;

UPDATE news_feed n
SET category = m.category,
    sentiment = m.sentiment,
    relevance = m.relevance,
    summary = m.summary,
    embedding = m.embedding,
    tickers = m.tickers
FROM repair_news_merge m
WHERE n.ctid = m.survivor_ctid;

DO $verify$
DECLARE
    counts repair_delete_counts%ROWTYPE;
    h_dups integer;
    u_dups integer;
    n_dups integer;
    bf_score numeric;
BEGIN
    SELECT * INTO counts FROM repair_delete_counts;
    SELECT count(*) INTO h_dups FROM (
        SELECT 1 FROM historical_ohlcv GROUP BY ticker, provider, timeframe, bar_time HAVING count(*) > 1
    ) d;
    SELECT count(*) INTO u_dups FROM (
        SELECT 1 FROM universe_tickers GROUP BY ticker HAVING count(*) > 1
    ) d;
    SELECT count(*) INTO n_dups FROM (
        SELECT 1 FROM news_feed GROUP BY guid HAVING count(*) > 1
    ) d;
    SELECT watch_score INTO bf_score FROM universe_tickers WHERE ticker = 'BF.B';

    IF ROW(counts.historical_deleted, counts.universe_deleted, counts.news_deleted) <> ROW(1253, 2, 40)
       OR ROW(h_dups, u_dups, n_dups) <> ROW(0, 0, 0)
       OR bf_score <> 8.6698 THEN
        RAISE EXCEPTION 'source-index repair verification failed; rolling back';
    END IF;
END
$verify$;

INSERT INTO audit_log (event_type, entity_type, actor, details)
SELECT 'source_index_integrity_repair', 'maintenance', :'operator',
       jsonb_build_object(
           'backup_artifact_id', :'backup_artifact_id',
           'backup_checksum', :'backup_checksum',
           'external_audit_checksum', :'external_audit_checksum',
           'script_git_hash', :'script_git_hash',
           'utc_started_at', :'utc_started_at',
           'utc_finished_at', now(),
           'historical_deleted', historical_deleted,
           'universe_deleted', universe_deleted,
           'news_deleted', news_deleted
       )
FROM repair_delete_counts;

COMMIT;
\else
\echo 'repair approvals incomplete; no statements executed'
\quit 1
\endif
