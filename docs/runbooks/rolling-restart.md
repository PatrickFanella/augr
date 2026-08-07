---
title: "Rolling restart procedure"
date: 2026-03-30
updated: 2026-08-07
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

4. For a process-only Docker Compose restart, replace the app from an already
   verified immutable image so PostgreSQL and Redis remain up. Do not build from
   an operator worktree during a production restart:

   ```bash
   docker compose up -d --no-build --no-deps app
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

## Commit-specific NUC release with migrations

Use this sequence for the tracked `docker-compose.nuc.yml` stack. It is stricter
than a process-only restart and must follow the release-readiness and Git
reconciliation runbooks.

1. Record the gate-verified local commit and prove the configured remote ref
   resolves to the same object. Confirm the release tree is still clean and
   record the current app/web image names for rollback:

   ```bash
   previous_app_image=$(docker inspect -f '{{.Config.Image}}' \
     "$(docker compose -f docker-compose.nuc.yml ps -q app)")
   previous_web_image=$(docker inspect -f '{{.Config.Image}}' \
     "$(docker compose -f docker-compose.nuc.yml ps -q web)")
   test -n "$previous_app_image"
   test -n "$previous_web_image"
   ```

   Persist both references in the release record before continuing; do not rely
   on shell history or Compose defaults during rollback.
2. Build app and web images before touching the running stack. Tag both with the
   verified commit and pass the complete commit through the app build metadata:

   ```bash
   release_commit=$(git rev-parse HEAD)
   release_tag="audit-$(git rev-parse --short=12 HEAD)"
   candidate_app_image="augr-app:$release_tag"
   candidate_web_image="augr-web:$release_tag"
   build_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)
   AUGR_APP_IMAGE="$candidate_app_image" \
   AUGR_WEB_IMAGE="$candidate_web_image" \
   BUILD_COMMIT="$release_commit" \
   BUILD_VERSION="$release_tag" \
   BUILD_TIME="$build_time" \
     docker compose -f docker-compose.nuc.yml build app web
   ```

   Building images is not permission to replace a production service. Inspect
   both image IDs/tags and keep the running stack unchanged until every
   predeployment check below passes.
3. Capture health, schema version, scheduler/job state, live/paper/dry-run
   controls, and exact financial counts/hashes. Stop only `app` to quiesce
   application writes; leave PostgreSQL, Redis, OpenCode, and web intact.
4. Take a custom-format schema-60 production backup from the explicit NUC
   manifest and restore it into an isolated, explicitly named validation
   database using `pg_restore --exit-on-error`. Verify restored schema 60 and the critical financial table
   counts, then drop only that validation database. Follow [Database backup and
   restore](database-backup-restore.md); a nonempty dump or `pg_restore -l`
   listing alone is not sufficient.
5. Recheck that production is still clean schema 60, then apply migrations once
   through the tracked tools service:

   ```bash
   docker compose -f docker-compose.nuc.yml --profile tools run --rm migrate
   ```

   Require clean schema 62 before starting the candidate app. Do not start the
   old image against schema 62.
6. Replace app and web together exactly once from the prebuilt immutable tags:

   ```bash
   AUGR_APP_IMAGE="$candidate_app_image" \
   AUGR_WEB_IMAGE="$candidate_web_image" \
     docker compose -f docker-compose.nuc.yml \
       up -d --no-build --no-deps app web
   ```

   Do not issue a second `up`, rebuild, or opportunistic restart to compensate
   for a failed candidate. Use the rollback procedure instead.
7. Prove the running app/web image names and both
   `org.opencontainers.image.revision` labels match `release_commit`; also
   require the app's runtime build commit to match. Confirm schema 62,
   API/database/Redis/web/OpenCode health,
   scheduler readiness, zero orphan nonterminal rows, live trading off,
   Alpaca/Binance paper modes on, Kalshi dry-run on, Polymarket off, and exact
   pre/post financial invariants before beginning postdeployment canaries.

## Verification

- `curl -sS "${TRADINGAGENT_API_URL:-http://127.0.0.1:8080}/healthz"` reports application, database, and Redis health without a degraded dependency.
- `SELECT version FROM schema_migrations` returns the expected post-migration version when schema changes were part of the rollout.
- `risk status` responds successfully and shows the expected kill switch and circuit breaker state.
- Logs contain a clean shutdown/startup sequence and no repeated crash loop.

## Rollback

1. Keep the instance out of rotation and stop application writes. If the restart introduced trading risk, leave the kill switch active.
2. Compare the previous image's required schema version with the live database. Do not start an older image against a newer schema: this runtime rejects that state even when the migrations are additive.
3. Before running down migrations, prove that reverting them is lossless. Check every table, column, or other structure introduced by the release for post-migration writes. If any new structure contains data, stop; use the verified predeployment backup and the database recovery procedure instead of silently dropping it.
4. When the lossless check passes, run the exact required down migrations and verify a clean schema at the previous image's required version. For the NUC stack, restore the exact recorded previous images with `deploy/docker-compose.nuc.rollback.yml` layered after the primary manifest. The override deliberately keeps the old scheduler and every live-market mode off because an older binary may not restore newer durable job controls or auto-disable behavior. It does not select image names: supply both recorded references explicitly and never accept Compose's default/local tags.

   ```bash
   AUGR_APP_IMAGE="$previous_app_image" \
   AUGR_WEB_IMAGE="$previous_web_image" \
     docker compose \
       -f docker-compose.nuc.yml \
       -f deploy/docker-compose.nuc.rollback.yml \
       up -d --no-build --no-deps app web
   ```
5. Inspect both running containers and require their image references to equal
   `previous_app_image` and `previous_web_image`; a merely healthy container on
   a different tag is not a successful rollback.
6. If backup restoration is required, follow [Database backup and restore](database-backup-restore.md) with writes halted and explicit operator control.
7. Verify application health, authenticated read-only access, schema compatibility, risk controls, and financial invariants. Keep the rollback scheduler disabled; do not clear the kill switch or return automation traffic until the release fault has been resolved and a separately approved recovery deployment passes its own gate.
