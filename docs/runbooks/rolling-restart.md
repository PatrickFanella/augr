---
title: "Rolling restart procedure"
date: 2026-03-30
tags: [runbook, operations, deployment]
type: runbook
---

# Rolling restart procedure

## Context

Use this runbook for config changes, routine deploys, or process-level recovery when you want the API to shut down cleanly. The server supports graceful shutdown and gives in-flight requests up to 30 seconds to complete before exit.

## Steps

1. Confirm the reason for the restart and whether trading should continue during the change. If the restart is incident-driven, consider activating the kill switch first.
2. Capture the current application health:

   ```bash
   curl -sS http://127.0.0.1:8080/healthz
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" risk status
   ```

3. For the Docker Compose stack in this repository, rebuild and replace only the app container so PostgreSQL and Redis remain up:

   ```bash
   docker compose up -d --build --no-deps app
   ```

4. If you are operating a multi-instance deployment outside local Compose, drain one instance from the load balancer, wait for in-flight traffic to finish, restart that instance, verify health, then continue to the next instance.
5. Follow logs until the new process reports that the API server is listening:

   ```bash
   docker compose logs --tail=100 -f app
   ```

6. Repeat health and risk checks before you route normal traffic back to the restarted instance.

## Verification

- `/healthz` returns `ok`.
- `risk status` responds successfully and shows the expected kill switch and circuit breaker state.
- Logs contain a clean shutdown/startup sequence and no repeated crash loop.

## Rollback

1. If the new process fails health checks, redeploy the previous known-good image or revision.
2. Keep the instance out of rotation until health checks recover.
3. If the restart introduced trading risk, leave the kill switch active until the rollback is complete and the restored instance passes verification.
