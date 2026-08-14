---
title: "Emergency kill switch activation"
date: 2026-03-30
tags: [runbook, operations, risk]
type: runbook
---

# Emergency kill switch activation

## Context

Use this runbook when trading must stop immediately because of a bad deployment, runaway strategy behavior, market data corruption, broker instability, or manual incident response. An active kill switch puts execution into **verified reduce-only mode**: new or increasing risk is blocked, while a close order may proceed only when the execution manager has found the matching open position, clamped the order to owned quantity, and attached an explicit close intent. A bare `SELL` is never inferred to be reduce-only. This service checks three mechanisms: API toggle, local file flag at `/tmp/tradingagent_kill`, and the `TRADING_AGENT_KILL=true` process environment variable.

If persisted risk state cannot be loaded, startup fails closed into the same reduce-only mode. API activation and deactivation return an error when their state cannot be saved; the in-process brake still activates before that error is returned.

## Steps

1. Record the incident in the on-call channel and write a short reason string you can reuse in audit trails, for example `incident-2026-03-30 bad fills from alpaca`.
2. Check current risk state:

   ```bash
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" risk status
   ```

3. Activate the primary kill switch through the authenticated CLI:

   ```bash
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" \
     risk kill --reason "incident-2026-03-30 bad fills from alpaca"
   ```

4. If the API is reachable but you need an explicit machine-readable confirmation, query the status endpoint and verify `kill_switch.active=true`:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/risk/status"
   ```

5. If the API path is degraded but the app process is still running, set the out-of-band file flag on the running host or container:

   ```bash
   docker compose exec app sh -lc 'touch /tmp/tradingagent_kill && ls -l /tmp/tradingagent_kill'
   ```

6. If you are intentionally starting the service in a halted state for maintenance, set `TRADING_AGENT_KILL=true` in the deployment environment before the process starts. Do not rely on exporting that variable in a separate shell to affect an already-running process.
7. Notify trading, incident response, and any downstream consumers that new or increasing risk is halted until the kill switch is cleared. Do not assume a close will pass: it must meet the verified reduce-only contract.

## Verification

- `tradingagent ... risk status` shows `kill_switch.active=true`.
- The status payload lists the mechanism you used:
  - `api_toggle` for the CLI/API path
  - `file_flag` for `/tmp/tradingagent_kill`
  - `env_var` for `TRADING_AGENT_KILL=true`
- Any opening or risk-increasing pre-trade check is rejected while the switch is active.
- A verified close intent is admitted, and its quantity cannot exceed the matching owned position.
- Run the non-mutating automated drill from a repository checkout:

  ```bash
  ./scripts/emergency-brake-drill.sh
  ```

  This exercises API, file, and environment activation; entry rejection; reduce-only admission; persistence failure; and restart restoration without toggling the live service.

## Rollback

1. Confirm the triggering incident is mitigated, reconciliation is clean, and explicit approval to resume trading is documented. A hard emergency halt must never be cleared by a timer.
2. Clear the API toggle:

   ```bash
   curl -sS \
     -X POST \
     -H "Content-Type: application/json" \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     -d '{"active":false}' \
     "$TRADINGAGENT_API_URL/api/v1/risk/killswitch"
   ```

3. Remove the file flag if it was used:

   ```bash
   docker compose exec app sh -lc 'rm -f /tmp/tradingagent_kill'
   ```

4. Remove `TRADING_AGENT_KILL=true` from deployment configuration and restart the affected instance if that mechanism was used.
5. Re-run `tradingagent ... risk status` and verify `kill_switch.active=false` and the mechanism list is empty before resuming trading.
