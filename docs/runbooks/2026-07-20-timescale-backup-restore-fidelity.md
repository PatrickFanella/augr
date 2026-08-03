---
title: "Timescale backup/restore fidelity"
date: 2026-07-20
tags: [runbook, operations, database, timescale, backup, restore]
type: runbook
---

# Timescale backup/restore fidelity

## Status

**BLOCKED pending source repair approval.** Approval checkpoint A was granted and the isolated schema-59 rehearsal completed. It proved the source heap contains duplicate keys hidden by inconsistent unique indexes; the restore correctly failed while rebuilding those indexes.

Current evidence:

- forced sequential scans find `1,253` duplicate `historical_ohlcv` keys, `2` duplicate universe tickers, and `40` duplicate news GUIDs
- affected source unique indexes report valid/ready but index-backed lookups can miss duplicate heap rows
- source and target forced-scan duplicate inventories match exactly
- restore failed rebuilding `historical_ohlcv_pkey`, `universe_tickers_pkey`, and `idx_news_feed_guid`
- no production restore is approved

## Purpose

Define the evidence and safety gates required to repair source integrity, rebuild affected indexes, and rerun the same-version clean-target restore rehearsal.

## Safety constraints

- do not delete duplicate rows, reindex, refresh collation metadata, or stop production without a separately approved maintenance plan
- take and verify a physical pre-repair rollback artifact before changing source data
- do not modify compose files
- do not claim logical recovery readiness until a post-repair rehearsal passes all parity gates

## Read-only source inspection

Until repair, source duplicate checks must force heap scans:

```sql
SET enable_indexscan = off;
SET enable_indexonlyscan = off;
SET enable_bitmapscan = off;
```

Capture:

- backup archive TOC (`pg_restore -l`) once the source artifact path is approved
- source-only duplicate inventory
- row counts, hypertables, chunks, indexes, constraints, and function signature parity artifacts

## Parity artifacts

Generate the following read-only files from source and target once a clean rehearsal target exists:

- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.row-counts.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.row-counts.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.timescale-hypertables.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.timescale-hypertables.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.timescale-chunks.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.timescale-chunks.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.public-indexes.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.public-indexes.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.public-constraints.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.public-constraints.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.source.public-function-signatures.tsv`
- `docs/reports/2026-07-20-timescale-backup-restore-fidelity.target.public-function-signatures.tsv`

## Decision rule

Repair must use explicit, table-specific survivor rules: historical OHLCV duplicates have identical payloads; universe conflicts require an operator-approved winner; news conflicts require an operator-approved merge/winner rule. After repair, rebuild affected indexes, verify heap/index agreement, then rerun the clean-target rehearsal. Do not attempt a production restore until every parity gate passes.
