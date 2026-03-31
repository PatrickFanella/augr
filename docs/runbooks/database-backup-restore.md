---
title: "Database backup and restore"
date: 2026-03-30
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

4. Validate that the backup is readable before you touch the target database:

   ```bash
   pg_restore -l "$BACKUP_FILE" | head
   ```

5. If you are restoring from another dump, take one more safety backup of the current database first using steps 2 through 4.
6. Restore the chosen backup into the target database:

   ```bash
   docker compose exec -T postgres pg_restore \
     -U "${POSTGRES_USER:-postgres}" \
     -d "${POSTGRES_DB:-tradingagent}" \
     --clean \
     --if-exists \
     --no-owner < "$BACKUP_FILE"
   ```

7. Re-apply migrations so the schema matches the current application build. When running `migrate` from the operator workstation, use a host-resolvable connection string instead of the container hostname in `.env`, and substitute the real database credentials for your environment:

   ```bash
   export LOCAL_DATABASE_URL="postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT:-5432}/${POSTGRES_DB:-tradingagent}?sslmode=disable"
   migrate -path migrations -database "$LOCAL_DATABASE_URL" up
   ```

8. Start the app again if you stopped it:

   ```bash
   docker compose up -d app
   ```

## Verification

- `pg_restore -l "$BACKUP_FILE"` succeeds for the backup you intend to use.
- `docker compose exec postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tradingagent}" -c '\dt'` lists the expected tables after restore.
- `curl -sS "${TRADINGAGENT_API_URL:-http://127.0.0.1:8080}/healthz"` returns `{"status":"all-ok"}` after the app is back up.
- An authenticated read-only API call such as `GET /api/v1/strategies` succeeds.

## Rollback

1. If the restore fails or the application starts with schema errors, stop the app again.
2. Re-run step 6 using the safety backup you took immediately before the failed restore.
3. Re-run `migrate -path migrations -database "$LOCAL_DATABASE_URL" up`.
4. Bring the app back up and verify health before clearing the incident.
