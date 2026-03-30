---
title: "Investigating a bad trade"
date: 2026-03-30
tags: [runbook, operations, investigation]
type: runbook
---

# Investigating a bad trade

## Context

Use this runbook when a fill, order, or final signal looks wrong. The API exposes the full trail needed for diagnosis: pipeline runs, agent decisions, orders, trades, positions, and agent memories.

## Steps

1. If the issue may still be producing new bad orders, activate the kill switch before gathering evidence.
2. Identify the affected ticker, approximate timestamp, strategy ID, run ID, order ID, or position ID from the alert or broker ticket.
3. Pull recent run history for the ticker:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/runs?ticker=$TICKER&limit=20&offset=0"
   ```

4. Inspect the suspected run and its decision trail:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/runs/$RUN_ID"

   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/runs/$RUN_ID/decisions?limit=200&offset=0"
   ```

5. Inspect order state and fills:

   ```bash
   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/orders/$ORDER_ID"

   curl -sS \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/trades?order_id=$ORDER_ID&limit=100&offset=0"
   ```

6. Search for stored memories that may have influenced the run:

   ```bash
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" \
     memories search "$TICKER"
   ```

7. Correlate the timeline with application logs by grepping for the run ID or order ID:

   ```bash
   docker compose logs --since=2h app | grep "$RUN_ID"
   ```

8. Write down the first bad decision, the first bad external response, and whether the root cause came from strategy logic, LLM output, market data, risk gating, or broker execution.

## Verification

- You can account for the full sequence from pipeline run to decision to order to trade.
- The identified root cause explains both the bad signal and the observed order/fill state.
- Any mitigation you applied, such as pausing a strategy or enabling the kill switch, is visible through the API.

## Rollback

1. If you paused strategies, changed settings, or activated the kill switch during the investigation, restore the pre-investigation state only after the root cause is addressed.
2. If evidence is incomplete, leave mitigations in place and escalate rather than resuming trading on partial information.
3. Preserve exported JSON and logs in the incident record until postmortem review is complete.
