-- Read-only collation and index inventory.
-- No collation refresh, reindex, or DDL.

SELECT current_database() AS datname,
       d.datcollate,
       d.datctype,
       d.datcollversion,
       pg_database_collation_actual_version(d.oid) AS actual_database_collversion,
       (d.datcollversion IS DISTINCT FROM pg_database_collation_actual_version(d.oid)) AS database_mismatch,
       n.nspname AS schema_name,
       collname,
       collversion,
       pg_collation_actual_version(c.oid) AS actual_version
FROM pg_collation c
JOIN pg_namespace n ON n.oid = c.collnamespace
JOIN pg_database d ON d.datname = current_database()
ORDER BY datname, schema_name, collname;

WITH indexed_columns AS (
    SELECT DISTINCT
           tbl_nsp.nspname AS schemaname,
           tbl.relname AS tablename,
           idx.relname AS indexname,
           a.attname AS column_name,
           t.typname AS type_name,
           coll.collname AS collation_name,
           CASE WHEN i.indcollation[array_position(i.indkey, a.attnum)] <> 0 THEN true ELSE false END AS uses_collation
    FROM pg_index i
    JOIN pg_class idx ON idx.oid = i.indexrelid
    JOIN pg_class tbl ON tbl.oid = i.indrelid
    JOIN pg_namespace tbl_nsp ON tbl_nsp.oid = tbl.relnamespace
    JOIN pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = ANY(i.indkey)
    JOIN pg_type t ON t.oid = a.atttypid
    LEFT JOIN pg_collation coll ON coll.oid = a.attcollation
    WHERE tbl.relkind IN ('r','p','m','f')
      AND idx.relkind = 'i'
      AND a.attnum > 0
      AND t.typcategory IN ('S','V')
)
SELECT schemaname,
       tablename,
       indexname,
       column_name,
       type_name,
       collation_name,
       uses_collation
FROM indexed_columns
ORDER BY schemaname, tablename, indexname, column_name;

SELECT EXISTS (
    SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'
) AS pg_stat_statements_available;

SELECT count(*) FILTER (WHERE collversion IS DISTINCT FROM pg_collation_actual_version(c.oid)) AS mismatched_collations,
       count(*) AS total_collations,
       count(DISTINCT collname) FILTER (WHERE collversion IS DISTINCT FROM pg_collation_actual_version(c.oid)) AS mismatched_collation_names
FROM pg_collation c;
