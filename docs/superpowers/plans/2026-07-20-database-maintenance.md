# Database Maintenance Plan: Kalshi Snapshot Retention + PostgreSQL Collation

> **For agentic workers:** Execute this plan step-by-step. This document is planning-only until explicit approvals pass. Use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prepare item 7 (Kalshi snapshot retention/compression) and item 8 (PostgreSQL collation maintenance) as safe, gated workstreams with explicit evidence, rehearsal, rollback, and approval checkpoints.

**Architecture:**
- Item 7 must remain read-only until the backup/restore recovery gate from item 1 passes; no retention deletion, compression activation, or production reorganization is allowed before that gate.
- Item 8 must treat the collation warning as a maintenance-window problem: rehearse first, then targeted/full `REINDEX`, then query/planner checks, and only refresh collation version last.
- No policy is enabled in this plan.

**Tech Stack:** PostgreSQL, existing backup/restore runbooks, SQL-only discovery, maintenance-window shell commands, and future migrations/runbooks/tests that are created only after approvals.

**Evidence already known:**
- `kalshi_market_snapshots`: 10.75M rows / 20.81GB.
- Latest snapshot query: 2.5ms.
- 7d count: 15s.
- 30d group: 122s with spill.
- Forecast: 34.5-58.4GiB additional growth over 90d.
- Collation: `datcollate`/`datctype` = `en_US.utf8`, recorded version = `2.36`, actual = null, 156 user indexes on collatable columns.
- Backup restore fidelity is blocked, so item 7 retention/compression and item 8 production maintenance remain blocked until item 1 recovery gate passes.

---

## Hard safety gates

- [ ] Do **not** delete, compress, backfill, or otherwise mutate `kalshi_market_snapshots` in production before the item 1 recovery gate passes.
- [ ] Do **not** enable any retention/compression policy before the operator approves the final proposal and backup restore fidelity is confirmed.
- [ ] Do **not** create future migrations, runbooks, or tests for production execution until the relevant approval checkpoint is reached.
- [ ] Do **not** run `REFRESH COLLATION VERSION` or `REINDEX` in production outside an approved maintenance window.
- [ ] Do **not** treat the restored copy as production traffic.

---

## Item 7: Kalshi snapshot retention/compression

**Purpose:** Define a safe retention window for `kalshi_market_snapshots`, compare implementation options, and rehearse compression/retention behavior without deleting production data until the recovery gate passes.

**Files (future work, only after approvals):**
- Create: `docs/superpowers/plans/2026-07-20-database-maintenance.md` (this plan)
- Future create only after approval: `docs/runbooks/kalshi-snapshot-retention.md`
- Future create only after approval: `docs/reports/2026-07-20-kalshi-snapshot-retention.md`
- Future create only after approval: `docs/sql/kalshi-snapshot-retention-preview.sql`
- Future create only after approval: `docs/sql/kalshi-snapshot-retention-rehearsal.sql`
- Future create only after approval: `docs/tests/kalshi-snapshot-retention.test.sh`
- Future create only after approval: `migrations/000060_kalshi_snapshot_retention.up.sql`
- Future create only after approval: `migrations/000060_kalshi_snapshot_retention.down.sql`
- Future create only after approval: `internal/repository/postgres/schema_version.go` with `RequiredSchemaVersion = 60`
- Migration `000060_kalshi_snapshot_retention` must be approved, implemented, applied, and reflected in `RequiredSchemaVersion = 60` before any `000061` / `000062` work begins.
- If item 7 is rejected, reserve and apply an explicit no-op `000060` migration that records the rejection decision so migration numbering remains contiguous; if that is not feasible, use an equally concrete safe mechanism that still preserves contiguous numbering and an auditable decision record.

### Step 1: Confirm actual consumer lookbacks before any retention decision.

- [ ] Inventory real readers of `kalshi_market_snapshots` and determine the longest actual lookback used by production jobs, dashboards, backfills, and ad hoc operational queries.
- [ ] Base the retention window on observed consumer lookbacks, not on table age or storage pressure alone.
- [ ] Record the minimum retention window that preserves all required consumers, plus a separate archive horizon if one is needed.

### Step 2: Compare the three implementation options.

- [ ] Compare **plain-table partitioning**, **Timescale hypertable conversion**, and a **summary/archive table** design.
- [ ] Evaluate each option against these criteria:
  - retention/deletion safety
  - restore fidelity
  - query performance for latest snapshot, 7d count, and 30d grouped aggregation
  - operational complexity
  - migration risk
  - rollback difficulty
- [ ] Record the exact decision criteria for selecting one option: choose the design that best preserves backup/restore fidelity, keeps the simplest rollback path, and meets the 7d/30d query requirements with the least operational risk.
- [ ] Document which option is preferred for the short term and which is safest if the recovery gate remains blocked.
- [ ] Do not reserve migration `000060_kalshi_snapshot_retention` until after this option-selection approval is complete and the ADR explicitly names the chosen approach.
- [ ] Create an ADR first that selects exactly one of: plain partitioning, Timescale conversion, or summary/archive, and use that ADR as the approval record before any migration reservation or production file drafting.

### Step 3: Capture the exact SQL needed for read-only measurement.

```sql
SELECT pg_size_pretty(pg_total_relation_size('kalshi_market_snapshots')) AS total_size,
       pg_size_pretty(pg_relation_size('kalshi_market_snapshots')) AS heap_size,
       pg_size_pretty(pg_indexes_size('kalshi_market_snapshots')) AS index_size,
       count(*) AS row_count,
       min(captured_at) AS oldest_snapshot_at,
       max(captured_at) AS newest_snapshot_at
FROM kalshi_market_snapshots;

SELECT date_trunc('day', captured_at) AS day,
       count(*) AS snapshots
FROM kalshi_market_snapshots
WHERE captured_at >= NOW() - INTERVAL '90 days'
GROUP BY 1
ORDER BY 1;

SELECT ticker,
       count(*) AS snapshots_30d,
       max(captured_at) AS last_seen_at
FROM kalshi_market_snapshots
WHERE captured_at >= NOW() - INTERVAL '30 days'
GROUP BY ticker
ORDER BY snapshots_30d DESC, ticker
LIMIT 50;

EXPLAIN (ANALYZE, BUFFERS)
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

### Step 4: Rehearse the chosen design without deleting production data.

- [ ] Build the migration rehearsal in a restored or isolated copy only.
- [ ] For partitioning: validate partition bounds, pruning, and detach/drop behavior on the copy.
- [ ] For Timescale hypertable conversion: validate chunking, compression eligibility, and retention policy semantics on the copy.
- [ ] For summary/archive: validate backfill, dual-write or cutover behavior, and downstream read path compatibility on the copy.
- [ ] Measure whether the 7d and 30d queries meet acceptable performance after the rehearsal design is applied.

### Step 5: Define compression/retention tests.

- [ ] Add tests that verify:
  - latest snapshot lookup remains fast
  - 7d counts do not regress beyond the current baseline
  - 30d grouped aggregation no longer spills, or spills within an accepted bound
  - retained rows exactly match the approved window
  - archive rows remain recoverable and queryable through the approved path
- [ ] Include rollback tests that prove the table can be restored to the pre-change read path on the rehearsal copy.

### Step 6: Draft rollback and no-deletion safeguards.

- [ ] Document a rollback path that preserves the original table until the approved cutover is complete.
- [ ] Require backup verification before any production deletion or compression activation.
- [ ] Explicitly prohibit dropping old data before the approved backup exists and has been validated.

### Step 7: Approval checkpoint A.

- [ ] Operator approves the retention window, preferred implementation option, and rehearsal results before any production-facing migration, runbook, or test file is created.
- [ ] Operator separately approves the ADR choice before reserving migration `000060_kalshi_snapshot_retention` or creating `000060_kalshi_snapshot_retention.up.sql` / `000060_kalshi_snapshot_retention.down.sql`; if item 7 is rejected, still reserve/apply a no-op `000060` migration (or equivalent contiguous-numbering safeguard) before any later migration numbering is consumed.
- [ ] If approval is withheld, stop after the read-only analysis and do not produce production DDL.

---

## Item 8: PostgreSQL collation maintenance

**Purpose:** Reconcile the collation warning with a safe maintenance sequence that preserves planner correctness and minimizes downtime.

**Files (future work, only after approvals):**
- Future create only after approval: `docs/runbooks/postgres-collation-maintenance.md`
- Future create only after approval: `docs/reports/2026-07-20-postgres-collation-inventory.sql`
- Future create only after approval: `docs/reports/2026-07-20-postgres-collation-inventory.md`
- Future create only after approval: `docs/tests/postgres-collation-maintenance.test.sh`

### Step 1: Inventory impacted objects.

- [ ] Confirm the collations in scope: `datcollate`/`datctype = en_US.utf8`, recorded version `2.36`, actual version null.
- [ ] Inventory the 156 user indexes on collatable columns and identify which tables, indexes, and queries are most likely to require reindexing.
- [ ] Capture the exact SQL inventory for affected indexes and planner-sensitive queries.

### Step 2: Prepare the maintenance sequence.

- [ ] Write the sequence as: **rehearsal -> targeted/full `REINDEX` -> query/planner checks -> `REFRESH COLLATION VERSION` last**.
- [ ] Estimate lock and downtime impact for each phase.
- [ ] Separate targeted `REINDEX` candidates from full database `REINDEX` candidates based on inventory and risk.

### Step 3: Rehearse on a restored copy before production.

- [ ] Restore a non-production copy and repeat the affected queries.
- [ ] Validate index rebuild behavior and check that query plans remain sensible after the rehearse-only reindex.
- [ ] Record restore duration, reindex duration, and any query-plan deltas.

### Step 4: Validate planner and query health.

- [ ] Run representative lookups, joins, sorts, and prefix/pattern searches that depend on collatable columns.
- [ ] Compare pre/post plans and row estimates.
- [ ] Confirm no unexpected sequential-scan regression appears for the indexed paths.

### Step 5: Draft the production maintenance commands as future-only.

```sql
-- FUTURE APPROVED MAINTENANCE WINDOW ONLY
REINDEX DATABASE tradingagent;
-- Run verification checks after REINDEX DATABASE tradingagent; and before refresh:
--   1) inspect query plans for collatable paths
--   2) confirm no unexpected sequential-scan regression
--   3) confirm lock/window impact remains acceptable
ALTER DATABASE tradingagent REFRESH COLLATION VERSION;
```

- [ ] Note that the exact commands are future-only, isolated to the `tradingagent` database, and that `ALTER DATABASE tradingagent REFRESH COLLATION VERSION;` must happen only after `REINDEX DATABASE tradingagent;` plus the documented checks above.

### Step 6: Rollback plan.

- [ ] If query health regresses, revert to the pre-maintenance restored backup and halt before refreshing collation version.
- [ ] If `REFRESH COLLATION VERSION` reveals unexpected issues, document the recovery state and the exact index set requiring repeat maintenance.

### Step 7: Approval checkpoint B.

- [ ] Operator approves the maintenance window, object list, and estimated downtime/lock impact before scheduling production execution.

---

## Cross-item sequencing and approvals

- [ ] Approval checkpoint C: item 1 recovery gate must pass before any item 7 production retention/compression work or item 8 production maintenance work begins.
- [ ] Approval checkpoint D: operator approves the backup-restore rehearsal results before any production deletion, compression activation, or collation refresh is allowed.
- [ ] Approval checkpoint E: operator approves the final retention/compression implementation file set before any production migration is drafted.
- [ ] Approval checkpoint F: operator approves the final collation maintenance runbook before any maintenance window is scheduled.
- [ ] Backup fidelity item 1 remains a hard prerequisite for all item 7 and item 8 production actions.

## Acceptance criteria

- [ ] The retention window is explicitly derived from actual consumer lookbacks.
- [ ] The plan compares plain-table partitioning, Timescale hypertable conversion, and summary/archive table approaches.
- [ ] No retention policy is enabled in production before approval.
- [ ] Collation maintenance follows rehearsal -> targeted/full `REINDEX` -> planner checks -> `REFRESH COLLATION VERSION` last.
- [ ] Downtime/lock impact and rollback are documented for the collation path.
- [ ] The plan names exact future file paths only, with no placeholder names such as `some_index_name` or `0000XX`.
- [ ] Future migrations/runbooks/tests are not created until the relevant approvals pass.
