---
title: "P2 backup/restore rehearsal report"
date: 2026-07-19
tags: [report, operations, database, rehearsal, p2]
type: report
---

# P2 backup/restore rehearsal report

## Summary

The rehearsal was safe for production, but restore verification is **BLOCKED**. A custom-format backup was produced and validated for readability, yet both restore attempts failed on the same duplicate primary-key class. The restored copy is therefore not faithful, and production restore readiness is not proven.

## Timeline

1. Source live DB stayed online, read-only, and healthy after rehearsal: `{"status":"ok","db":"ok","redis":"ok"}`.
2. Source details recorded: image `timescale/timescaledb:2.17.2-pg17`, schema version `54`, `strategies` `121`, `kalshi_market_snapshots` `10,751,665`, `pg_database_size` `23,959,547,027` bytes.
3. Custom-format `pg_dump` completed with Timescale internal circular-FK warnings; dump size was `1,640,697,854` bytes.
4. Host did not have `pg_restore`, so archive listing was validated successfully with a disposable same-image container.
5. Isolated target was created with container `augr-p2-restore-20260720`, volume `augr-p2-restore-data-20260720`, network mode `none`, ports `{}`, same image, and no production traffic.
6. Attempt 1: normal full `pg_restore` failed creating `historical_ohlcv_pkey` because the target already had duplicate BFAM stock-chain 1d key data.
7. Source read-only duplicate inventory found zero duplicate groups and zero extra rows.
8. Attempt 2: fresh isolated volume/container; pre-data restore, `timescaledb_pre_restore()`, data restore, post-data restore, and planned post_restore were attempted, but the same PK class failed again.
9. Partial target restore contained `6,261,857` `historical_ohlcv` rows, matching source total rows, but an example target key count was `2` versus source key count `1`.
10. Partial target DB size was `21,787,470,995` bytes.
11. Cleanup completed: isolated container, volume, and sensitive dump deleted. No production writes occurred.

## Pass/fail matrix

| Check | Result | Notes |
| --- | --- | --- |
| Source stayed healthy/read-only | PASS | `{"status":"ok","db":"ok","redis":"ok"}` |
| Backup created | PASS | Custom-format dump completed |
| Backup readability validated | PASS | Archive listing validated in disposable same-image container |
| Isolated target avoided production traffic | PASS | Network `none`, ports `{}` |
| Restore fidelity | FAIL | Duplicate primary-key class prevented faithful restore |
| Source-vs-restored parity | FAIL | Key-count mismatch observed |
| Schema validation completion | BLOCKED | Could not complete after restore failure |
| Production restore readiness | FAIL/BLOCKED | Not proven |

## Blocker impact

The restore is not yet trustworthy for production use. The evidence supports only that the dump process can complete and that the rehearsal can be isolated safely; it does **not** prove that the archive restores cleanly into an equivalent target.

## Cleanup

- isolated container removed
- isolated volume removed
- sensitive dump deleted
- no production data or settings changed

## No-change statement

No production restore was approved or executed.
