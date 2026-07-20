# P2 PostgreSQL collation inventory

## Measured production state
- The database reports a collation-version warning on connect: `database "tradingagent" has no actual collation version, but a version was recorded`.
- Database collation/locale: `en_US.utf8` for both `datcollate` and `datctype`.
- Recorded `datcollversion`: `2.36`; the actual-version query returned null, so the database-level mismatch remains unresolved until gated reindexing and final version refresh.
- Collations scanned: 815 total.
- Mismatched collations: 809 at the catalog surface.
- `pg_stat_statements` is not available in the live database.
- User indexes on collatable text/varchar columns: 126 tables, 156 indexes, 173 indexed columns.
- No indexes matched the explicit `COLLATE` / `text_pattern_ops` / `varchar_pattern_ops` filter, so the impacted object set is broader than that filter but far smaller than the full collation catalog.

## Maintenance proposal only
- Take a fresh backup first.
- Rehearse on a non-production restored copy.
- Inventory affected objects and confirm the intended maintenance scope.
- Reindex affected objects or the database first.
- Verify planner and query health after reindexing.
- Refresh collation version only after the reindex/rebuild checks pass.

## FUTURE APPROVED MAINTENANCE WINDOW ONLY
```sql
REINDEX DATABASE tradingagent;
ALTER DATABASE tradingagent REFRESH COLLATION VERSION;
```

## Gating
- These commands are intentionally future-only and must not be run from this runbook as-is.
