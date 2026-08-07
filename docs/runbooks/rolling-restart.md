---
title: "Rolling restart procedure"
date: 2026-03-30
tags: [runbook, operations, deployment]
type: runbook
---

# Rolling restart procedure

## Context

Use this runbook for config changes, routine deploys, or process-level recovery when you want the API to shut down cleanly. The server supports graceful shutdown and gives in-flight requests up to 10 seconds to complete before exit.

## Steps

1. Confirm the reason for the restart and whether trading should continue during the change. If the restart is incident-driven, consider activating the kill switch first.
2. Capture the current application health:

   ```bash
   curl -sS "${TRADINGAGENT_API_URL:-http://127.0.0.1:8080}/healthz"
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" risk status
   ```

3. If the deploy includes pending schema changes, record the current schema version and exact app image, take a verified database backup, and read both the up and down migrations before changing anything. The runtime requires an exact schema match, fails fast when the database is either behind or ahead, and requires a fresh restart after migrations.

   ```bash
   task migrate:up
   ```

4. For the Docker Compose stack in this repository, rebuild and replace only the app container so PostgreSQL and Redis remain up:

   ```bash
   docker compose up -d --build --no-deps app
   ```

5. If you are operating a multi-instance deployment outside local Compose, drain one instance from the load balancer, wait for in-flight traffic to finish, restart that instance, verify health, then continue to the next instance.
6. Follow logs until the new process reports that the API server is listening:

   ```bash
   docker compose logs --tail=100 -f app
   ```

7. Verify the live schema version after the restart when schema changes were part of the rollout:

   ```bash
   docker compose exec -T postgres psql -U "${POSTGRES_USER:-postgres}" -d "${POSTGRES_DB:-tradingagent}" -At -c 'SELECT version FROM schema_migrations ORDER BY version DESC LIMIT 1'
   ```

8. Repeat health and risk checks before you route normal traffic back to the restarted instance.

## Verification

- `curl -sS "${TRADINGAGENT_API_URL:-http://127.0.0.1:8080}/healthz"` returns `{"status":"all-ok"}`.
- `SELECT version FROM schema_migrations` returns the expected post-migration version when schema changes were part of the rollout.
- `risk status` responds successfully and shows the expected kill switch and circuit breaker state.
- Logs contain a clean shutdown/startup sequence and no repeated crash loop.

## Rollback

1. Keep the instance out of rotation and stop application writes. If the restart introduced trading risk, leave the kill switch active.
2. Compare the previous image's required schema version with the live database. Do not start an older image against a newer schema: this runtime rejects that state even when the migrations are additive.
3. Before running down migrations, prove that reverting them is lossless. Check every table, column, or other structure introduced by the release for post-migration writes. If any new structure contains data, stop; use the verified predeployment backup and the database recovery procedure instead of silently dropping it.
4. When the lossless check passes, run the exact required down migrations, verify a clean schema at the previous image's required version, and only then restore the exact previous image.
5. If backup restoration is required, follow [Database backup and restore](database-backup-restore.md) with writes halted and explicit operator control.
6. Verify application health, authenticated read-only access, schema compatibility, risk controls, and financial invariants before clearing the kill switch or returning traffic.
