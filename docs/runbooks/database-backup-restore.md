---
title: "Database backup and restore"
date: 2026-03-30
updated: 2026-08-07
tags: [runbook, operations, database]
type: runbook
---

# Database backup and restore

## Context

Use this runbook before risky schema work, before restoring production-like data into a lower environment, or when recovering from corruption or operator error. The local stack runs PostgreSQL in Docker Compose as the `postgres` service, and schema migrations live in `migrations/`.

## Steps

1. Halt writes before a restore. For the application stack in this repository, either activate the kill switch or stop the app container:

   ```bash
   docker compose stop app
   ```

2. Create a timestamped backup directory on the operator workstation:

   ```bash
   mkdir -p backups
   export BACKUP_FILE="backups/tradingagent-$(date -u +%Y%m%dT%H%M%SZ).dump"
   ```

3. Create a PostgreSQL custom-format backup from the running `postgres` container:

   ```bash
   docker compose exec -T postgres pg_dump \
     -U "${POSTGRES_USER:-postgres}" \
     -d "${POSTGRES_DB:-tradingagent}" \
     --format=custom \
     --clean \
     --if-exists \
     --no-owner > "$BACKUP_FILE"
   ```

4. Check the backup catalog before you touch the target database:

   ```bash
   pg_restore -l "$BACKUP_FILE" | head
   ```

   This is an early corruption check, not restore proof. Before production
   schema work, restore the exact backup into an isolated validation database
   as described below.

5. If you are restoring from another dump, take one more safety backup of the current database first using steps 2 through 4.
6. Restore the chosen backup into the target database:

   ```bash
   docker compose exec -T postgres pg_restore \
     -U "${POSTGRES_USER:-postgres}" \
     -d "${POSTGRES_DB:-tradingagent}" \
     --exit-on-error \
     --clean \
     --if-exists \
     --no-owner < "$BACKUP_FILE"
   ```

7. Re-apply migrations so the schema matches the current application build before you start or restart the app. The runtime fails fast on schema mismatch and needs a fresh restart after migrations. When running `migrate` from the operator workstation, use a host-resolvable connection string instead of the container hostname in `.env`, and substitute the real database credentials for your environment:

   ```bash
   export LOCAL_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT:-5432}/${POSTGRES_DB:-tradingagent}?sslmode=disable"
   migrate -path migrations -database "$LOCAL_DATABASE_URL" up
   ```

8. Start or restart the app after migrations complete:

   ```bash
   docker compose up -d app
   ```

9. Verify the schema version before returning traffic:

   ```bash
   docker compose exec -T postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tradingagent}" -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1'
   ```

## Required pre-migration restore proof

For production schema changes, listing a dump is insufficient. With application
writes quiesced, restore the exact predeployment dump into a new validation
database in the same PostgreSQL service, using a name derived only from the
verified release commit. Validate the name before using it in create/drop
commands; never target the production database. The NUC commands below name the
authoritative manifest explicitly so a default development Compose project
cannot be backed up by mistake.

```bash
mkdir -p backups
BACKUP_FILE="backups/augr-predeploy-$(date -u +%Y%m%dT%H%M%SZ).dump"
baseline_schema=$(docker compose -f docker-compose.nuc.yml exec -T postgres \
  sh -ec 'psql -X -qAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "SELECT concat_ws(chr(124), version::text, dirty::text) FROM schema_migrations"' \
  | tr -d '[:space:]')
baseline_counts=$(docker compose -f docker-compose.nuc.yml exec -T postgres \
  sh -ec 'psql -X -qAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "SELECT concat_ws(chr(124), (SELECT count(*) FROM orders)::text, (SELECT count(*) FROM trades)::text, (SELECT count(*) FROM positions)::text)"' \
  | tr -d '[:space:]')
docker compose -f docker-compose.nuc.yml exec -T postgres \
  sh -ec 'pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" --format=custom --no-owner' \
  > "$BACKUP_FILE"
test -s "$BACKUP_FILE"
pg_restore -l "$BACKUP_FILE" >/dev/null
backup_sha256=$(sha256sum "$BACKUP_FILE" | awk '{print $1}')
test -n "$backup_sha256"

release_short=$(git rev-parse --short=12 HEAD)
restore_db="augr_restore_check_$release_short"
case "$restore_db" in
  augr_restore_check_[0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f][0-9a-f]) ;;
  *) echo "invalid restore database name" >&2; exit 1 ;;
esac
docker compose -f docker-compose.nuc.yml exec -T -e RESTORE_DB="$restore_db" postgres \
  sh -ec 'createdb -U "$POSTGRES_USER" "$RESTORE_DB"'
docker compose -f docker-compose.nuc.yml exec -T -e RESTORE_DB="$restore_db" postgres \
  sh -ec 'pg_restore -U "$POSTGRES_USER" -d "$RESTORE_DB" --clean --if-exists --single-transaction --exit-on-error --no-owner' \
  < "$BACKUP_FILE"
docker compose -f docker-compose.nuc.yml exec -T -e RESTORE_DB="$restore_db" postgres \
  sh -ec 'psql -X -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$RESTORE_DB" \
    -c "SELECT version, dirty FROM schema_migrations" \
    -c "SELECT (SELECT count(*) FROM orders) AS orders, (SELECT count(*) FROM trades) AS trades, (SELECT count(*) FROM positions) AS positions"'
restored_schema=$(docker compose -f docker-compose.nuc.yml exec -T -e RESTORE_DB="$restore_db" postgres \
  sh -ec 'psql -X -qAt -U "$POSTGRES_USER" -d "$RESTORE_DB" \
    -c "SELECT concat_ws(chr(124), version::text, dirty::text) FROM schema_migrations"' \
  | tr -d '[:space:]')
restored_counts=$(docker compose -f docker-compose.nuc.yml exec -T -e RESTORE_DB="$restore_db" postgres \
  sh -ec 'psql -X -qAt -U "$POSTGRES_USER" -d "$RESTORE_DB" \
    -c "SELECT concat_ws(chr(124), (SELECT count(*) FROM orders)::text, (SELECT count(*) FROM trades)::text, (SELECT count(*) FROM positions)::text)"' \
  | tr -d '[:space:]')
test "$restored_schema" = "$baseline_schema"
test "$restored_counts" = "$baseline_counts"
docker compose -f docker-compose.nuc.yml exec -T -e RESTORE_DB="$restore_db" postgres \
  sh -ec 'dropdb -U "$POSTGRES_USER" "$RESTORE_DB"'
```

Require the expected clean schema version and exact agreement with the
pre-backup critical-table counts. If creation, restore, validation, or cleanup
fails, stop the release and retain the dump; do not migrate production.
Record `BACKUP_FILE`, `backup_sha256`, the schema/count baseline, and the
successful validation database result in the release evidence.

## NUC production restore during rollback

This is a destructive recovery operation and requires explicit operator
authorization after the guarded down-migration path has refused or failed. Use
only the checksum-recorded, isolated-restore-proven predeployment dump. Keep app
stopped and validate the exact Compose target before sending the dump:

```bash
: "${BACKUP_FILE:?BACKUP_FILE is required}"
: "${EXPECTED_BACKUP_SHA256:?EXPECTED_BACKUP_SHA256 is required}"
test -s "$BACKUP_FILE"
test "$(sha256sum "$BACKUP_FILE" | awk '{print $1}')" = "$EXPECTED_BACKUP_SHA256"
test -z "$(docker compose -f docker-compose.nuc.yml ps --status running -q app)" || {
  echo "refusing restore while app is running" >&2; exit 1;
}
postgres_container=$(docker compose -f docker-compose.nuc.yml ps -q postgres)
test -n "$postgres_container"
test "$(docker inspect -f '{{ index .Config.Labels "com.docker.compose.project" }}' "$postgres_container")" = "augr"
test "$(docker inspect -f '{{ index .Config.Labels "com.docker.compose.service" }}' "$postgres_container")" = "postgres"
docker compose -f docker-compose.nuc.yml exec -T postgres \
  sh -ec 'pg_restore -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    --clean --if-exists --single-transaction --exit-on-error --no-owner' \
  < "$BACKUP_FILE"
restored_production_schema=$(docker compose -f docker-compose.nuc.yml exec -T postgres \
  sh -ec 'psql -X -qAt -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "SELECT concat_ws(chr(124), version::text, dirty::text) FROM schema_migrations"' \
  | tr -d '[:space:]')
test "$restored_production_schema" = "60|f" || {
  echo "restored production schema mismatch: $restored_production_schema" >&2; exit 1;
}
```

Reconcile restored critical counts with the recorded predeployment baseline
before starting the exact old images. If `pg_restore` fails, its single
transaction must roll back; keep app stopped, preserve all output and the dump,
and escalate instead of retrying with weaker flags or another target.

## Verification

- `pg_restore -l "$BACKUP_FILE"` succeeds for the backup you intend to use.
- For production schema changes, an isolated `pg_restore --clean --if-exists
  --single-transaction --exit-on-error`
  succeeds and restored schema/critical counts match the pre-backup baseline.
- `docker compose exec postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tradingagent}" -c '\dt'` lists the expected tables after restore.
- `SELECT version FROM schema_migrations` returns the expected current application schema version after `migrate ... up`.
- `curl -sS "${TRADINGAGENT_API_URL:-http://127.0.0.1:8080}/healthz"` reports healthy application, database, and Redis dependencies after the app is back up.
- An authenticated read-only API call such as `GET /api/v1/strategies` succeeds.

## Rollback

1. If the restore fails or the application starts with schema errors, stop the app again.
2. Re-run step 6 using the safety backup you took immediately before the failed restore.
3. Re-run `migrate -path migrations -database "$LOCAL_DATABASE_URL" up`.
4. Bring the app back up and verify health before clearing the incident.
