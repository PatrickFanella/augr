---
title: "Kalshi snapshot retention migration"
date: 2026-08-09
tags: [runbook, operations, database, timescaledb, kalshi]
type: runbook
---

# Kalshi snapshot retention migration

## Purpose and safety boundary

Migration 104 creates an empty, one-day-chunk Timescale hypertable for Kalshi
snapshots. It applies compression after seven days and retention after 30 days.
It does not copy, rename, or delete the existing table. Those destructive
steps are intentionally operator-controlled.

The repository's full-database restore rehearsal is blocked by unrelated
duplicate rows in `historical_ohlcv`, `universe`, and `news`. Do not weaken the
restore procedure or use a failed full restore as rollback proof. This runbook
uses a table-scoped archive and table-scoped restore rehearsal, and makes no
claim about full-database restore fidelity.

Never use `VACUUM FULL` or an in-place rewrite for this table. Keep the app
stopped from the final archive through row-parity verification and deployment
of the schema-104 image.

## Preconditions

- Explicit approval exists for the archive, cutover, old-table drop, and
  30-day retention window.
- The exact schema-104 application image is built and its tests pass.
- The destination host has enough space for both the compressed archive and an
  isolated restore.
- `pg_restore -l` succeeds for the archive, and the table-only restore has
  completed with row and timestamp parity.
- No unrelated deployment is consuming Nuc's remaining disk margin.
- The old table is a regular PostgreSQL table and the migration-104 staging
  table is an empty Timescale hypertable.

## Archive the retained window off-host

Run this on the database host while the app is still running for completed UTC
days. The script writes one zstd-compressed CSV per day plus a checksum and row
count manifest. It is resumable and never stores the archive on Nuc.

```bash
scripts/archive-kalshi-snapshots.sh \
  almaz:/mnt/impuls/backups/nuc/augr/RELEASE/kalshi-retained-30d \
  2026-07-10 \
  2026-08-08
```

After stopping the app, rerun through the current UTC day. Existing complete
days are checksum-recorded again; an interrupted `.tmp` file is replaced.

Create a final table-only custom-format rollback archive while the app is
stopped. Stream it directly off-host, validate its catalog, and record its
checksum and byte size in the release evidence. Do not print database
credentials.

```bash
docker exec augr-postgres-1 sh -ec \
  'exec pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --no-owner --table=public.kalshi_market_snapshots' |
  ssh almaz 'dd of=/mnt/impuls/backups/nuc/augr/RELEASE/kalshi-market-snapshots-final.dump.tmp status=none'
ssh almaz 'test -s /mnt/impuls/backups/nuc/augr/RELEASE/kalshi-market-snapshots-final.dump.tmp && \
  mv /mnt/impuls/backups/nuc/augr/RELEASE/kalshi-market-snapshots-final.dump.tmp \
     /mnt/impuls/backups/nuc/augr/RELEASE/kalshi-market-snapshots-final.dump'
```

## Isolated rehearsal

Use the same PostgreSQL and Timescale versions as production. Restore only
`public.kalshi_market_snapshots` from the full archive into an isolated
database. Apply migration 104 to a separate empty database, then use the daily
restore script against that schema. Require all of the following:

- archived and restored row counts match for every UTC day;
- minimum and maximum `captured_at` values match;
- every archive checksum matches the manifest;
- chunks older than seven days are compressed;
- compression and retention jobs exist with seven- and 30-day intervals;
- inserts and latest-by-ticker reads work after compression.

## Production cutover

1. Record the current app image, schema version, old table row estimate, exact
   byte size, minimum timestamp, and maximum timestamp.
2. Stop `augr-app-1` and prove it is stopped. Confirm there are no other writers
   to `kalshi_market_snapshots`.
3. Finish the current-day daily archive and the final table-only dump. Verify
   catalogs, checksums, sizes, and off-host paths.
4. Starting from clean schema `103`, apply migration 104 from the reviewed
   clean worktree. Confirm schema `104`, `dirty=false`, an empty staging
   hypertable, and both policies.
5. Perform the metadata cutover in one transaction:

   ```sql
   BEGIN;
   LOCK TABLE public.kalshi_market_snapshots IN ACCESS EXCLUSIVE MODE;
   ALTER TABLE public.kalshi_market_snapshots
     RENAME TO kalshi_market_snapshots_pre_partition;
   ALTER TABLE public.kalshi_market_snapshots_partitioned
     RENAME TO kalshi_market_snapshots;
   COMMIT;
   ```

6. Recheck the off-host checksum evidence and confirm the app is still stopped.
   Only then drop `public.kalshi_market_snapshots_pre_partition`. This releases
   the old relation files without an in-place rewrite.
7. Restore the daily archives in chronological order. The restore script
   verifies each checksum and row count and compresses eligible closed chunks:

   ```bash
   scripts/restore-kalshi-snapshots.sh \
     almaz:/mnt/impuls/backups/nuc/augr/RELEASE/kalshi-retained-30d
   ```

8. Confirm total row parity with the manifest, timestamp bounds, per-day chunk
   counts, compressed chunk count, both policy jobs, database size, and free
   filesystem space.
9. Recreate only the app service with the exact schema-104 image and `--no-build`.
   Verify `/healthz`, schema version, the latest Kalshi snapshot query, and one
   capped collector run before ending maintenance.

## Abort and rollback

- Before the old table is dropped, reverse the two renames in one transaction
  and revert migration 104.
- After the old table is dropped, keep the app stopped. Recreate the canonical
  table from the checksum-verified table-only archive or repeat the partitioned
  restore from the daily archives. Do not start a schema-103 or earlier image
  against schema 104.
- If a checksum, row count, timestamp bound, policy, health check, or disk-space
  gate fails, stop. Preserve both tables when they still exist and preserve all
  off-host archives.
