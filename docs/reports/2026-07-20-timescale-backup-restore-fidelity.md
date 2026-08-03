---
title: "Timescale backup/restore fidelity report"
date: 2026-07-20
tags: [report, operations, database, timescale, backup, restore]
type: report
---

# Timescale backup/restore fidelity report

## Summary

**FAILED rehearsal with root cause identified.** A complete schema-59 custom archive was restored into a fresh same-image, network-isolated target. Restore failed while rebuilding three unique indexes because the source heap contains duplicate keys hidden by inconsistent source indexes. Production restore readiness is not proven.

Current rehearsal evidence:

- source container: `augr-postgres-1`; source stayed online and read-only
- archive: complete custom-format schema-59 dump, compression level 1, `2,337.5 MiB`
- TOC: captured; each affected table has one `TABLE DATA` entry
- target: fresh `timescale/timescaledb:2.17.2-pg17`, named volume, `--network none`, no published ports
- restore: non-parallel, with `timescaledb_pre_restore()` before restore
- no restore target or dump was retained; temporary container, volume, in-container copy, and host dump were deleted and cleanup was verified

## Source-read-only evidence

The prior rehearsal report recorded the following clean-source facts:

| Evidence | Value |
| --- | --- |
| source health | `{"status":"ok","db":"ok","redis":"ok"}` |
| image | `timescale/timescaledb:2.17.2-pg17` |
| schema version during prior failed rehearsal | `54` |
| current production schema version | `59` |
| `strategies` rows | `121` |
| `kalshi_market_snapshots` rows | `10,751,665` |
| prior duplicate result | incorrectly reported `0` because the damaged indexes were trusted |

Current schema-59 source refresh at Wave 0:

| Evidence | Value |
| --- | ---: |
| `schema_migrations` | `59`, `dirty=false` |
| `historical_ohlcv` rows at initial Wave-0 snapshot | `6,268,140` |
| forced sequential-scan duplicate historical key groups | `1,253` |
| forced sequential-scan duplicate historical extra rows | `1,253` |
| duplicate historical groups with conflicting OHLCV payload | `0` |
| forced sequential-scan duplicate universe ticker groups / extra rows | `2 / 2` |
| duplicate universe groups with conflicting payload | `1` (`BF.B`) |
| forced sequential-scan duplicate news GUID groups / extra rows | `40 / 40` |
| duplicate news groups with conflicting payload | `40` |
| `kalshi_market_snapshots` rows | `11,419,191` |

## Failed restore evidence

The current approved rehearsal failed rebuilding exactly three unique indexes:

1. `historical_ohlcv_pkey`: duplicate `(BFAM, stock-chain, 1d, 2021-12-22 14:30:00+00)`
2. `universe_tickers_pkey`: duplicate ticker `BF.B`
3. `idx_news_feed_guid`: duplicate GUID `WP-WSJ-0002349775`

Forced sequential scans produced the same complete duplicate inventory on source and target:

| Duplicate class | Source groups / extras | Target groups / extras |
| --- | ---: | ---: |
| historical primary key | `1,253 / 1,253` | `1,253 / 1,253` |
| universe primary key | `2 / 2` | `2 / 2` |
| news GUID unique index | `40 / 40` | `40 / 40` |

The dump itself is not duplicating table data. The archive faithfully exposes source heap duplicates when the target rebuilds unique indexes. Index-backed source queries can miss these rows, so future integrity checks must disable index, index-only, and bitmap scans until indexes are repaired.

## Validation state

- TOC inspection: captured in `docs/reports/2026-07-20-timescale-backup-restore-fidelity.toc.txt`
- full restore log: captured in `docs/reports/2026-07-20-timescale-backup-restore-fidelity.restore.log`
- parity SQL: prepared in `docs/reports/2026-07-20-timescale-backup-restore-fidelity.sql`
- validator: prepared in `docs/reports/2026-07-20-timescale-backup-restore-fidelity.test.sh`
- cleanup: verified; temporary container, volume, and dump were removed

## Blocked decision

Restore remains **BLOCKED** until source duplicates are repaired under an approved maintenance plan, affected unique indexes are rebuilt, collation/index integrity is verified, and a new same-version rehearsal passes all parity gates. A physical snapshot/base backup is required as the pre-repair rollback artifact because the current logical dump cannot restore cleanly.
