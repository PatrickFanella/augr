# P2 Safe Operations Implementation Plan

> **For agentic workers:** Execute this plan task-by-task. Keep this work read-only until each approval gate explicitly passes. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce a safe-operations report pack for P2 without mutating production strategy state, retention policy, collation state, or database contents.

**Architecture:** This plan is intentionally split into read-only discovery, reporting, and rehearsal-only validation. Production inventory, Kalshi snapshot sizing/growth analysis, collation/index inventory, and backup/restore rehearsal may run now. Strategy mutations, retention/deletion/compression activation, `REFRESH COLLATION VERSION`, and `REINDEX` remain hard-gated until separate approval.

**Tech Stack:** Go report/query helpers where needed, PostgreSQL, Docker Compose, existing strategy/control-plane tables, and database backup/restore runbooks.

---

## Hard safety gates

- [ ] Do **not** pause, disable, delete, or edit any production strategy, schedule, snapshot row, index, or collation object during the discovery phase.
- [ ] Do **not** execute any retention deletion, compression policy activation, or snapshot backfill without a separate written approval checkpoint.
- [ ] Do **not** run `REFRESH COLLATION VERSION` or `REINDEX` outside an approved maintenance window.
- [ ] Do **not** use the rehearsal restore target for production traffic.
- [ ] Approval checkpoint A: operator signs off on read-only inventory scope before any SQL beyond `SELECT` is executed.
- [ ] Approval checkpoint B: operator signs off on the retention/compression proposal before any policy DDL is drafted for implementation.
- [ ] Approval checkpoint C: operator signs off on the collation maintenance runbook before any maintenance-window execution is scheduled.
- [ ] Approval checkpoint D: operator signs off on the backup/restore rehearsal results before any production restore is considered.

---

## Task 1: Read-only strategy hygiene report

**Purpose:** Produce a current inventory of active strategies that appear unscheduled or duplicated, without changing any strategy rows.

**Files:**
- Create: `docs/reports/2026-07-19-p2-strategy-hygiene.md` (report output only; no mutations)
- Create: `docs/reports/2026-07-19-p2-strategy-hygiene.sql` (read-only query bundle)
- Create: `docs/reports/2026-07-19-p2-strategy-hygiene.test.sh` (report validation harness)

- [ ] **Step 1: Capture the exact read-only strategy inventory queries.**

```sql
-- Active strategies with no weekly run in the last 7 days.
SELECT s.id,
       s.name,
       s.ticker,
       s.market_type,
       s.schedule_cron,
       s.status,
       s.skip_next_run,
       s.is_paper,
       s.is_active,
       s.updated_at,
       max(pr.started_at) AS last_run_at
FROM strategies s
LEFT JOIN pipeline_runs pr ON pr.strategy_id = s.id
WHERE s.is_active = TRUE
GROUP BY s.id, s.name, s.ticker, s.market_type, s.schedule_cron, s.status, s.skip_next_run, s.is_paper, s.is_active, s.updated_at
HAVING max(pr.started_at) IS NULL OR max(pr.started_at) < NOW() - INTERVAL '7 days'
ORDER BY last_run_at NULLS FIRST, s.updated_at DESC;

-- Duplicate active ticker groups; this is report-only and does not disable anything.
SELECT market_type,
       ticker,
       count(*) AS active_strategy_count,
       array_agg(id ORDER BY updated_at DESC) AS strategy_ids,
       array_agg(name ORDER BY updated_at DESC) AS strategy_names,
       array_agg(status ORDER BY updated_at DESC) AS strategy_statuses,
       array_agg(schedule_cron ORDER BY updated_at DESC) AS strategy_schedule_crons,
       array_agg(skip_next_run ORDER BY updated_at DESC) AS strategy_skip_next_runs,
       array_agg(is_paper ORDER BY updated_at DESC) AS strategy_is_papers,
       array_agg(is_active ORDER BY updated_at DESC) AS strategy_is_actives
FROM strategies
WHERE is_active = TRUE
GROUP BY market_type, ticker
HAVING count(*) > 1
ORDER BY active_strategy_count DESC, market_type, ticker;

-- Report-only inventory for paused/unscheduled combinations.
SELECT id, name, ticker, market_type, schedule_cron, status, skip_next_run, is_paper, is_active, updated_at
FROM strategies
WHERE is_active = TRUE AND (status <> 'active' OR COALESCE(schedule_cron, '') = '')
ORDER BY updated_at DESC, name;
```

- [ ] **Step 2: Run the read-only inventory and preserve raw output.**

```bash
psql "$DATABASE_URL" -v ON_ERROR_STOP=1 -f docs/reports/2026-07-19-p2-strategy-hygiene.sql \
  > docs/reports/2026-07-19-p2-strategy-hygiene.raw.txt
```

- [ ] **Step 3: Produce the hygiene report summary.** Include counts, the top 20 affected rows per query, and a clear distinction between intentional variants and accidental duplicates.

- [ ] **Step 4: Validate the report is read-only.** Confirm the SQL file contains only `SELECT` statements and that the harness rejects any `UPDATE`, `DELETE`, `INSERT`, `ALTER`, or `DROP` keywords.

```bash
./docs/reports/2026-07-19-p2-strategy-hygiene.test.sh docs/reports/2026-07-19-p2-strategy-hygiene.sql
```

- [ ] **Step 5: Approval checkpoint A.** Review the report with the operator; proceed only after they confirm no strategy mutation is authorized yet.

- [ ] **Step 6: Commit the report artifacts.**

```bash
git add docs/reports/2026-07-19-p2-strategy-hygiene.md docs/reports/2026-07-19-p2-strategy-hygiene.sql docs/reports/2026-07-19-p2-strategy-hygiene.test.sh
git commit -m "docs(ops): add read-only strategy hygiene report"
```

---

## Task 2: Kalshi snapshot size, growth, and query benchmarks

**Purpose:** Measure `kalshi_market_snapshots` size, growth rate, and target query performance; forecast 90-day storage only.

**Files:**
- Create: `docs/reports/2026-07-19-p2-kalshi-snapshot-sizing.md`
- Create: `docs/reports/2026-07-19-p2-kalshi-snapshot-sizing.sql`
- Create: `docs/reports/2026-07-19-p2-kalshi-snapshot-bench.test.sh`

- [ ] **Step 1: Capture the exact sizing and growth queries.**

```sql
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
```

- [ ] **Step 2: Benchmark the retained query patterns.** Measure the actual latency and plan shape for:
  - latest snapshot by ticker
  - time-range snapshot scan
  - snapshot count by ticker

```sql
EXPLAIN (ANALYZE, BUFFERS)
\set ticker 'AAPL'
SELECT *
FROM kalshi_market_snapshots
WHERE ticker = :'ticker'
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
```

- [ ] **Step 3: Produce a 90-day forecast.** Use the last 30/60/90 days of daily ingestion to estimate row growth, heap growth, and index growth with a conservative 1.25x safety factor.

- [ ] **Step 4: Draft a retention/compression proposal only.** Include proposed thresholds, expected savings, and restore implications; do not enable any policy.

- [ ] **Step 5: Approval checkpoint B.** Operator must approve the proposal before any retention or compression DDL is written for a later change set.

- [ ] **Step 6: Validate no activation occurred.** Confirm no `ALTER TABLE ... SET (timescaledb.compress...)`, `add_retention_policy`, `drop_chunks`, or similar policy activation appears in the report SQL.

- [ ] **Step 7: Commit the measurement artifacts.**

```bash
git add docs/reports/2026-07-19-p2-kalshi-snapshot-sizing.md docs/reports/2026-07-19-p2-kalshi-snapshot-sizing.sql docs/reports/2026-07-19-p2-kalshi-snapshot-bench.test.sh
git commit -m "docs(ops): add kalshi snapshot sizing report"
```

---

## Task 3: PostgreSQL collation and index inventory

**Purpose:** Inventory affected indexes and queries, then publish a maintenance runbook only; no `REFRESH COLLATION VERSION` or `REINDEX` yet.

**Files:**
- Create: `docs/runbooks/postgres-collation-maintenance.md`
- Create: `docs/reports/2026-07-19-p2-collation-inventory.sql`
- Create: `docs/reports/2026-07-19-p2-collation-inventory.md`

- [ ] **Step 1: Collect the collation warning surface and impacted indexes.**

```sql
SELECT current_database() AS datname,
       n.nspname AS schema_name,
       collname,
       collversion,
       pg_collation_actual_version(c.oid) AS actual_version
FROM pg_collation c
JOIN pg_namespace n ON n.oid = c.collnamespace
ORDER BY datname, schema_name, collname;

SELECT schemaname,
       tablename,
       indexname,
       indexdef
FROM pg_indexes
WHERE indexdef ILIKE '%COLLATE%'
   OR indexdef ILIKE '%text_pattern_ops%'
   OR indexdef ILIKE '%varchar_pattern_ops%'
ORDER BY schemaname, tablename, indexname;

-- Optional: run only when pg_stat_statements is installed and preloaded.
SELECT EXISTS (
    SELECT 1 FROM pg_extension WHERE extname = 'pg_stat_statements'
) AS pg_stat_statements_available;
```

If `pg_stat_statements_available` is true, run the query inventory in a separate
conditional psql step. Otherwise record that workload-level query inventory is
unavailable and use repository SQL plus `EXPLAIN` on known collation-sensitive
queries; do not create or preload the extension during this read-only phase.

- [ ] **Step 2: Document the maintenance runbook with explicit gates.** The runbook must say:
  - take a fresh database backup first
  - rehearse on a non-production restored copy
  - verify planner and query health on the restored copy
  - only then schedule production maintenance
  - `REFRESH COLLATION VERSION` and `REINDEX` require a separate maintenance-window approval

- [ ] **Step 3: Include the exact production maintenance commands as future gated actions, not executable now.**

```sql
-- FUTURE APPROVED MAINTENANCE WINDOW ONLY
ALTER DATABASE tradingagent REFRESH COLLATION VERSION;
REINDEX DATABASE tradingagent;
```

- [ ] **Step 4: Add validation that the runbook is non-executing.** Confirm the runbook labels those statements as future-only and does not embed an active shell script that runs them.

- [ ] **Step 5: Approval checkpoint C.** Operator must approve the maintenance window and target objects before any live execution is scheduled.

- [ ] **Step 6: Commit the inventory and runbook.**

```bash
git add docs/runbooks/postgres-collation-maintenance.md docs/reports/2026-07-19-p2-collation-inventory.sql docs/reports/2026-07-19-p2-collation-inventory.md
git commit -m "docs(ops): add collation maintenance runbook"
```

---

## Task 4: Backup/restore rehearsal on a non-production restored copy

**Purpose:** Rehearse backup and restore on a restored copy only; validate schema and read-only access without touching production traffic.

**Files:**
- Create: `docs/runbooks/p2-backup-restore-rehearsal.md`
- Create: `docs/reports/2026-07-19-p2-backup-restore-rehearsal.md`

- [ ] **Step 1: Use the existing backup/restore runbook to create a fresh dump.** Confirm the backup file is readable with `pg_restore -l` before any restore.

- [ ] **Step 2: Restore into a non-production target.** Use a separate database, container, or isolated Compose project name; never the live production service.

- [ ] **Step 3: Validate the restored copy.**

```bash
psql "$RESTORED_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM strategies;'
psql "$RESTORED_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'SELECT count(*) FROM kalshi_market_snapshots;'
psql "$RESTORED_DATABASE_URL" -v ON_ERROR_STOP=1 -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1;'
```

- [ ] **Step 4: Run a read-only smoke test against the restored copy.** Verify a health endpoint or equivalent read-only query path succeeds against the restored environment.

- [ ] **Step 5: Record restore duration, backup size, restore size, and any missing-object observations.** Include whether the restored copy matches the expected schema version.

- [ ] **Step 6: Approval checkpoint D.** Operator signs off on the rehearsal results before any production restore is contemplated.

- [ ] **Step 7: Commit the rehearsal artifacts.**

```bash
git add docs/runbooks/p2-backup-restore-rehearsal.md docs/reports/2026-07-19-p2-backup-restore-rehearsal.md
git commit -m "docs(ops): add backup restore rehearsal"
```

---

## Final acceptance

- [ ] Strategy hygiene report is complete and explicitly read-only.
- [ ] Kalshi snapshot size, growth, query benchmarks, and 90-day forecast are documented.
- [ ] Retention/compression is only proposed, never activated.
- [ ] Collation/index inventory and maintenance runbook exist, with `REFRESH COLLATION VERSION` and `REINDEX` deferred behind approval.
- [ ] Backup/restore rehearsal completed on a non-production restored copy.
- [ ] No production strategy mutation occurred anywhere in this plan.
