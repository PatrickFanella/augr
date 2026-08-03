-- Read-only parity inventory for Timescale backup/restore fidelity.
-- Run with psql against source and clean rehearsal target, then diff the emitted TSVs.

\pset format unaligned
\pset tuples_only on
\pset pager off
\set ON_ERROR_STOP on

-- row counts for application tables
SELECT format(
  'SELECT %L AS table_name, count(*) AS row_count FROM %I.%I;',
  n.nspname || '.' || c.relname,
  n.nspname,
  c.relname
)
FROM pg_namespace n
JOIN pg_class c ON c.relnamespace = n.oid
WHERE n.nspname = 'public'
  AND c.relkind = 'r'
  AND c.relname IN (SELECT tablename FROM pg_tables WHERE schemaname = 'public')
ORDER BY 1
\gexec

-- Timescale hypertable inventory
SELECT hypertable_schema, hypertable_name
FROM timescaledb_information.hypertables
ORDER BY 1, 2;

-- Timescale chunk inventory
SELECT hypertable_schema, hypertable_name, chunk_schema, chunk_name
FROM timescaledb_information.chunks
ORDER BY 1, 2, 3, 4;

-- Public indexes
SELECT schemaname, tablename, indexname, indexdef
FROM pg_indexes
WHERE schemaname = 'public'
ORDER BY 1, 2, 3;

-- Public constraints
SELECT n.nspname AS schemaname, rel.relname AS tablename, c.conname, pg_get_constraintdef(c.oid) AS constraintdef
FROM pg_constraint c
JOIN pg_class rel ON rel.oid = c.conrelid
JOIN pg_namespace n ON n.oid = rel.relnamespace
WHERE n.nspname = 'public'
ORDER BY 1, 2, 3;

-- Public function signatures
SELECT n.nspname AS schemaname,
       p.proname AS function_name,
       pg_get_function_identity_arguments(p.oid) AS signature
FROM pg_proc p
JOIN pg_namespace n ON n.oid = p.pronamespace
WHERE n.nspname = 'public'
ORDER BY 1, 2, 3;
