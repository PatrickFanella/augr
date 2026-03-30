---
title: "Adding a new strategy"
date: 2026-03-30
tags: [runbook, operations, strategies]
type: runbook
---

# Adding a new strategy

## Context

Use this runbook to create a new strategy record in the local trading agent API. Strategy creation is available through the CLI and the API. Valid market types are `stock`, `crypto`, and `polymarket`.

## Steps

1. Gather the required inputs:
   - strategy name
   - ticker
   - market type (`stock`, `crypto`, or `polymarket`)
   - whether the strategy should start active
   - whether it must run in paper mode
   - optional cron schedule and JSON config payload
2. Create the strategy through the CLI:

   ```bash
   tradingagent --api-url "$TRADINGAGENT_API_URL" --api-key "$TRADINGAGENT_API_KEY" \
     strategies create \
     --name "AAPL breakout" \
     --description "US equities canary strategy" \
     --ticker AAPL \
     --market-type stock \
     --schedule-cron "0 */4 * * 1-5" \
     --config '{"risk_profile":"canary","lookback_days":20}' \
     --active=true \
     --paper=true
   ```

3. If you need a machine-readable copy of the created record, rerun the command with `--format json` or fetch the full list from the API.
4. Record the returned strategy ID in the change ticket so future investigations can trace runs back to the initial setup.
5. If this strategy will ever trade live, leave it in paper mode until you have observed at least one clean run.

## Verification

- `tradingagent ... strategies list` includes the new strategy with the expected `ticker`, `market_type`, active state, and paper flag.
- The JSON config is preserved exactly as provided.
- If your environment supports manual runs, trigger one approved paper run and confirm a run record is created.

## Rollback

1. If the strategy was created with the wrong parameters, delete it through the API:

   ```bash
   curl -sS \
     -X DELETE \
     -H "X-API-Key: $TRADINGAGENT_API_KEY" \
     "$TRADINGAGENT_API_URL/api/v1/strategies/$STRATEGY_ID"
   ```

2. If the strategy already ran, review and clean up any related schedules, positions, or paper orders before recreating it.
3. Recreate the strategy only after correcting the inputs that caused the bad configuration.
