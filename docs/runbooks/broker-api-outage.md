---
title: "Broker API outage handling"
date: 2026-03-30
tags: [runbook, operations, broker]
type: runbook
---

# Broker API outage handling

## Context

Use this runbook when Alpaca, Binance, or another configured broker stops accepting orders, reports stale positions, or returns intermittent API failures. The settings payload reports which brokers are configured, and strategy records determine whether each strategy trades in paper mode.

## Steps

1. If there is any chance of duplicate fills, uncertain order state, or delayed broker acknowledgements, activate the kill switch immediately.
2. Capture the current broker and strategy state:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/settings" | tee /tmp/settings-broker-backup.json

   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/strategies?limit=100&offset=0" | tee /tmp/strategies-backup.json
   ```

3. Confirm which strategies are routed to the affected market and whether they are already paper-only.
4. For each impacted live strategy, fetch the full strategy document, save it locally, then either set `is_active=false` or `is_paper=true` and `PUT` the full updated record back to `/api/v1/strategies/{id}`.
5. Review in-flight orders and fills so you know what must be reconciled at the broker:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/orders?limit=100&offset=0"
   ```

6. Reconcile the API response with the broker dashboard before you clear the incident. Do not assume an order is cancelled unless the broker confirms it.
7. If only live execution is down but analysis is still useful, leave strategies active in paper mode until the broker stabilizes.

## Verification

- `GET /api/v1/strategies` shows the affected strategies are paused or paper-only.
- No new live orders are submitted while the outage is active.
- Broker-side order state matches the platform’s order and trade records after reconciliation.

## Rollback

1. When the broker is healthy again, restore the saved strategy documents or re-enable the affected strategies one at a time.
2. Clear the kill switch only after a human confirms outstanding orders and positions are reconciled.
3. Watch the first canary strategy closely; if order acknowledgements or fills look wrong, re-apply the pause and reopen the incident.
