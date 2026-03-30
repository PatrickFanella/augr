---
title: "Circuit breaker investigation and reset"
date: 2026-03-30
tags: [runbook, operations, risk]
type: runbook
---

# Circuit breaker investigation and reset

## Context

Use this runbook when the risk engine reports `circuit_breaker.state=tripped` and trading has stopped because daily loss, drawdown, or consecutive-loss thresholds were exceeded. The breaker state is held in process memory, auto-resets after the configured cooldown, and is exposed through `GET /api/v1/risk/status`.

## Steps

1. Freeze further risk while you investigate. If the breaker tripped during a broader incident, activate the kill switch first using the [Emergency kill switch activation](emergency-kill-switch.md) runbook.
2. Capture the current breaker state:

   ```bash
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" risk status
   ```

3. Save the full JSON snapshot so you preserve `reason`, `tripped_at`, and `cooldown_end`:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/risk/status" | tee /tmp/risk-status.json
   ```

4. Identify the trades or positions that pushed the system over the threshold:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/orders?limit=50&offset=0"

   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/trades?limit=50&offset=0"
   ```

5. Compare the breaker reason against the configured risk thresholds in `.env` or the running settings payload:
   - `RISK_MAX_DAILY_LOSS_PCT`
   - `RISK_MAX_DRAWDOWN_PCT`
   - `RISK_CIRCUIT_BREAKER_COOLDOWN`
6. Decide how to reset:
   - **Preferred:** wait for `cooldown_end` and query `risk status` again. The breaker auto-resets the next time status or a pre-trade check is evaluated after cooldown expires.
   - **Escalated:** if you must clear the in-memory breaker sooner and you have approval, perform the [Rolling restart procedure](rolling-restart.md). There is no public CLI or HTTP reset endpoint in the current service build.
7. If the root cause is bad data or a broken strategy, disable or pause the underlying strategy before allowing the breaker to clear.

## Verification

- `risk status` shows `circuit_breaker.state=open`.
- `risk_status` returns to `normal` or `warning`; it should no longer be `breached`.
- A small paper-trading strategy or approved canary run completes without immediately re-tripping the breaker.

## Rollback

1. If the breaker trips again during verification, re-enable the kill switch and keep trading halted.
2. Revert any temporary threshold or strategy changes you made during diagnosis.
3. If you used a restart to clear the breaker and the service comes back unhealthy, roll back to the previous application version and leave the kill switch active until investigation is complete.
