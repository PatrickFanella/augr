# PostgreSQL collation maintenance runbook

## Status
Future-only. Do not execute `REFRESH COLLATION VERSION` or `REINDEX` from this document without explicit maintenance-window approval.

## Preconditions
1. Take a fresh backup.
2. Restore it to a non-production target.
3. Inventory the affected objects and confirm scope.
4. Reindex affected objects or the database on the restored copy.
5. Verify planner behavior and query health after reindexing.

## Production maintenance window
```sql
-- FUTURE APPROVED MAINTENANCE WINDOW ONLY
REINDEX DATABASE tradingagent;
ALTER DATABASE tradingagent REFRESH COLLATION VERSION;
```

## Post-checks
- Confirm no unexpected collation warnings remain.
- Verify application queries and indexes behave as expected.
- Record any objects requiring follow-up review.

## Guardrails
- Never use this runbook to change production outside an approved window.
- Never run it against a restored rehearsal target serving traffic.
