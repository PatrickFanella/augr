---
title: "P2 backup/restore rehearsal"
date: 2026-07-19
tags: [runbook, operations, database, rehearsal, p2]
type: runbook
---

# P2 backup/restore rehearsal

## Status

**Current restore verification: BLOCKED.** The dump procedure was exercised, but the restore was not proven faithful. Do not use this rehearsal as evidence that production restore is safe.

## Purpose

Rehearse backup and restore against an isolated, non-production target only. This runbook records the safe isolation pattern and the Timescale restore-mode sequence used during the rehearsal.

## Safe isolation procedure

Use a disposable target that cannot reach production traffic:

- separate container and volume
- network mode `none`
- no published ports
- same database image as source
- no access path from production services

For the recorded rehearsal, the target was:

- container: `augr-p2-restore-20260720`
- volume: `augr-p2-restore-data-20260720`
- network mode: `none`
- ports: `{}`
- image: `timescale/timescaledb:2.17.2-pg17`

## Restore sequencing

When restoring a Timescale dump into an isolated target, use the restore-mode sequence:

1. restore pre-data
2. enter restore mode with `timescaledb_pre_restore()`
3. restore data
4. restore post-data
5. run the planned post-restore step

This sequence is documented here for rehearsal fidelity only. It did **not** resolve the duplicate primary-key failure observed in this run.

## Required verification

- source system remains healthy and read-only after rehearsal
- backup archive can be listed or otherwise validated before restore
- restored copy matches source schema version and row parity
- restored copy supports read-only checks without production traffic

## Approval gate

**Approval checkpoint D:** no production restore may be attempted until the rehearsal report is reviewed and explicitly approved by the operator.

## Next diagnostic steps

- inspect archive TOC for duplicate hypertable data entries
- compare the Timescale-supported backup approach and version-specific restore guidance
- test a schema/data migration excluding internal schemas only in a non-production copy
- add a CI/rehearsal gate that requires primary-key creation and source-vs-restored parity before promotion

Do **not** rely on destructive source changes, `--disable-triggers`, or dropping constraints as a success criterion.
