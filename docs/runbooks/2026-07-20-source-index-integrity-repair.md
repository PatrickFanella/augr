---
title: "Source index integrity repair runbook"
date: 2026-07-20
tags: [runbook, operations, postgres, integrity, repair]
type: runbook
---

# Source index integrity repair runbook

## Purpose

Repair source-index integrity before any recovery can pass. This is a gated, read-only-first repair runbook; no mutation may run until the operator explicitly approves the repair window, maintenance window, active-writer quiescence, exact rollback artifact, and `-v execute_repair=1`.

## Required operator decisions

1. Approve verified physical rollback artifact: cold `tar.zst` backup of `augr_postgres_data` under `/srv/server/backups`, SHA256 recorded, cloned into a separate volume/container, and restore-tested in isolation before mutation.
2. Approve read-only preflight results and duplicate classification.
3. Approve the `BF.B` merge rule: keep latest survivor, merge max(nonzero `watch_score`), max `last_scanned`, `bool_or(active)`, min `created_at`, max `updated_at`, latest non-null identity metadata.
4. Approve the affected-object-only reindex package; broader database/collation refresh remains out of scope for this artifact set.
5. Approve production maintenance window, downtime, and explicit active-writer checks.

## Stop / rollback conditions

- Stop if any duplicate group has conflicting payload unless the runbook says the payloads must be identical and they are not.
- Stop if the physical backup is missing, has a checksum mismatch, or cannot be restore-tested in isolation.
- Stop if app/write quiescence cannot be achieved or any active writer remains.
- Stop if forced sequential-scan checks do not exactly reproduce historical `1253` groups/extras, universe `2` groups/extras, news `40` groups/extras, or zero conflicting OHLCV payload groups.
- Stop if `BF.B` is not the sole universe conflict or if `BIO.B`/news identity assertions fail closed.
- Stop if post-repair parity or reindex validation fails.
- Roll back to the verified physical backup if mutation begins and any transactional step aborts.

## Downtime steps

1. Announce write quiescence and maintenance window.
2. Stop app writers, schedulers, ingestion jobs, and verify no active writers remain.
3. Capture and verify the cold physical rollback artifact.
4. Export exact duplicate source rows and planned survivor/deletion mappings as CSV, verify SHA256, then run read-only preflight SQL and archive the output.
5. Obtain explicit operator approval variables and launch mutation only with `-v execute_repair=1`.
6. Enter a single transactional repair session.
7. Run `2026-07-20-source-index-integrity-repair.reindex.sql` for the three affected tables only.
8. Run a fresh logical dump / isolated restore and full parity checks.
9. Keep broader database reindex and collation refresh in the separately approved item-8 window.
10. If anything fails, restore the verified backup before retrying.

## Repair rules

- Historical duplicates: assert exact payload groups, then keep one row using locked-transaction `ctid` rank.
- `BF.B`/`BIO.B`: keep `updated_at DESC NULLS LAST`, `created_at DESC`, `ctid DESC` survivor.
- `BF.B`: merge max(nonzero `watch_score`), max `last_scanned`, `bool_or(active)`, min `created_at`, max `updated_at`, latest non-null identity metadata.
- news identity: keep `created_at DESC, id DESC` survivor; assert `source/title/description/link/published_at` equal; latest non-null/nonempty `category/sentiment/relevance/summary`; newest embedding if present, else remain null; `tickers` as sorted distinct union; delete non-survivors.
- Audit all source rows and chosen survivors.
- Use forced sequential scans to prove duplicate groups and compare source vs survivor inventory.
- Before transaction, export exact duplicate source rows and planned survivor/deletion mappings as CSV into an operator-supplied protected audit directory and record SHA256, backup artifact ID/checksum, operator, script git hash, and UTC timestamps.

## Required external audit exports

With the app stopped, export all duplicate source rows before repair. Store the files in a mode-700 operator directory outside the database and hash them:

```bash
install -d -m 700 "$AUDIT_DIR"
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "\copy (select h.ctid as source_ctid,h.* from public.historical_ohlcv h join (select ticker,provider,timeframe,bar_time from public.historical_ohlcv group by 1,2,3,4 having count(*)>1) d using (ticker,provider,timeframe,bar_time) order by 2,3,4,5,h.ctid) to stdout with csv header" \
  > "$AUDIT_DIR/historical_duplicates.csv"
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "\copy (select ctid as source_ctid,u.* from public.universe_tickers u where ticker in ('BF.B','BIO.B') order by ticker,updated_at desc nulls last,created_at desc,ctid desc) to stdout with csv header" \
  > "$AUDIT_DIR/universe_duplicates.csv"
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "\copy (select n.ctid as source_ctid,n.* from public.news_feed n join (select guid from public.news_feed group by guid having count(*)>1) d using (guid) order by guid,created_at desc,id desc) to stdout with csv header" \
  > "$AUDIT_DIR/news_duplicates.csv"
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "\copy (select ticker,source_ctid,survivor_ctid,case when source_ctid=survivor_ctid then 'survive' else 'delete' end as planned_action from (select ticker,ctid as source_ctid,first_value(ctid) over(partition by ticker order by updated_at desc nulls last,created_at desc,ctid desc) as survivor_ctid from public.universe_tickers where ticker in ('BF.B','BIO.B')) m order by ticker,planned_action desc,source_ctid) to stdout with csv header" \
  > "$AUDIT_DIR/universe_survivor_mapping.csv"
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "\copy (select guid,source_ctid,survivor_ctid,case when source_ctid=survivor_ctid then 'survive' else 'delete' end as planned_action from (select guid,ctid as source_ctid,first_value(ctid) over(partition by guid order by created_at desc,id desc) as survivor_ctid from public.news_feed where guid in(select guid from public.news_feed group by guid having count(*)>1)) m order by guid,planned_action desc,source_ctid) to stdout with csv header" \
  > "$AUDIT_DIR/news_survivor_mapping.csv"
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
  -c "\copy (select ticker,provider,timeframe,bar_time,source_ctid,survivor_ctid,case when source_ctid=survivor_ctid then 'survive' else 'delete' end as planned_action from (select ticker,provider,timeframe,bar_time,ctid as source_ctid,first_value(ctid) over(partition by ticker,provider,timeframe,bar_time order by ctid) as survivor_ctid from public.historical_ohlcv where (ticker,provider,timeframe,bar_time) in(select ticker,provider,timeframe,bar_time from public.historical_ohlcv group by 1,2,3,4 having count(*)>1)) m order by 1,2,3,4,planned_action desc,source_ctid) to stdout with csv header" \
  > "$AUDIT_DIR/historical_survivor_mapping.csv"
sha256sum "$AUDIT_DIR"/*.csv > "$AUDIT_DIR/SHA256SUMS"
sha256sum -c "$AUDIT_DIR/SHA256SUMS"
```

The execute script requires the resulting aggregate audit checksum and records it with the backup identity, operator, script hash, timestamps, and exact delete counts.

## Approval-gated operator commands

```bash
export REPAIR_APPROVED=0
export REPAIR_WRITE_QUIESCE_APPROVED=0
export REPAIR_PHYSICAL_BACKUP_APPROVED=0
export EXTERNAL_AUDIT_VERIFIED=0
```

Mutation is blocked until `execute_repair=1` and every explicit approval variable required for that section is `1`.

## Cold backup / restore-test runbook

The active NUC Compose volume is `augr_postgres_data`; verify it with `docker volume inspect` before continuing.

```bash
docker compose -f docker-compose.nuc.yml stop app
docker compose -f docker-compose.nuc.yml exec -T postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc \
  "select count(*) from pg_stat_activity where datname=current_database() and pid<>pg_backend_pid() and backend_type='client backend' and state<>'idle'"
# Require output: 0. Then stop PostgreSQL for a cold archive.
docker compose -f docker-compose.nuc.yml stop postgres
ts=$(date -u +%Y%m%dT%H%M%SZ)
archive="/srv/server/backups/augr_postgres_data_${ts}.tar.zst"
docker run --rm -v augr_postgres_data:/source:ro -v /srv/server/backups:/backups \
  alpine:3.20 sh -lc "apk add --no-cache zstd >/dev/null && tar -C /source -I 'zstd -T0' -cpf /backups/$(basename "$archive") ."
sha256sum "$archive" > "${archive}.sha256"
sha256sum -c "${archive}.sha256"

docker volume create augr_postgres_data_rehearsal
docker run --rm -v augr_postgres_data_rehearsal:/target -v /srv/server/backups:/backups:ro \
  alpine:3.20 sh -lc "apk add --no-cache zstd >/dev/null && tar -C /target -I zstd -xpf /backups/$(basename "$archive")"
docker run -d --name augr-postgres-physical-rehearsal --network none \
  -v augr_postgres_data_rehearsal:/var/lib/postgresql/data \
  timescale/timescaledb:2.17.2-pg17 postgres -D /var/lib/postgresql/data -c shared_preload_libraries=timescaledb
docker exec augr-postgres-physical-rehearsal pg_isready -U "$POSTGRES_USER" -d "$POSTGRES_DB"
docker exec augr-postgres-physical-rehearsal psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Atc \
  "select version, dirty from schema_migrations order by version desc limit 1"
docker rm -f augr-postgres-physical-rehearsal
docker volume rm augr_postgres_data_rehearsal

# Restart only source PostgreSQL. Keep the app stopped through repair/reindex validation.
docker compose -f docker-compose.nuc.yml up -d postgres
```

Rollback is physical: stop app and PostgreSQL, move the failed source volume aside, create a clean `augr_postgres_data`, extract the checksum-verified archive into it, start PostgreSQL alone, verify schema/queryability and duplicate evidence, then start the app. Do not extract over a populated volume and do not start app/schedulers until validation passes.
