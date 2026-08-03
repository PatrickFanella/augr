---
title: "Source index integrity repair report"
date: 2026-07-20
tags: [report, operations, postgres, integrity, repair]
type: report
---

# Source index integrity repair report

## Summary

Accepted evidence shows source-index duplicates that block recovery unless repaired under strict operator approval. The repair must stay read-only until the physical rollback artifact is verified, writes are quiesced, active writers are absent, `-v execute_repair=1` is set, and all explicit approval variables are present.

## Accepted evidence

- historical duplicate groups: 1,253 exact-payload groups
- historical conflicting OHLCV payload groups: 0
- universe duplicate groups: 2, with explicit `BF.B` and `BIO.B` rows
- universe conflicts: only `BF.B`
- news duplicate groups: 40 identity-column-equality groups
- news identity columns are equal across duplicate groups
- per-field conflict counts are required for the universe/news groups
- latest rows must be complete; `news_feed.embedding` is currently all null
- forced sequential-scan checks confirmed index-backed queries can miss these rows
- physical rollback artifact must exist before mutation

## Oracle guidance folded into proposal

- historical duplicates: exact-payload only; keep one via locked-transaction `ctid` rank
- `BIO.B`: keep `updated_at DESC NULLS LAST`, `created_at DESC`, `ctid DESC`
- `BF.B`: keep latest survivor and merge max(nonzero `watch_score`), max `last_scanned`, `bool_or(active)`, min `created_at`, max `updated_at`, latest non-null identity metadata
- news identity: keep `created_at DESC, id DESC`; assert `source/title/description/link/published_at` equal; latest non-null/nonempty `category/sentiment/relevance/summary`; embedding remains null until backfilled; sorted distinct union of tickers; delete non-survivors
- audit every source row and chosen survivor
- reindex affected objects after commit; broader `REINDEX DATABASE` and collation refresh are separate approval, refresh last
- verify parity with a fresh logical dump and isolated restore after repair
- canonical read-only preflight passed against production and returned `preflight_ok=true`
- transactional repair plus affected-table reindex passed against an isolated 1,253/2/40 fixture; no production mutation was run

## Proposal status

This is an approval gate proposal only. No SQL mutation is authorized by this document. Item 1 remains blocked until the physical rollback rehearsal, external CSV audit, completed repair, affected-table reindex, and a fresh logical restore/parity rehearsal all pass.
