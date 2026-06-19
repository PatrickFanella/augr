---
title: "Kalshi live readiness"
date: 2026-06-19
tags: [runbook, operations, kalshi, trading]
type: runbook
---

# Kalshi live readiness

## Purpose

Use this runbook only when preparing a future Kalshi strategy for live trading.
Current default remains paper/data only. Discovery creates paper strategies,
and Sprint C still does **not** enable live submission because
`newBrokerForStrategy` blocks at `kalshi live client is not initialised` until a
real client is wired.

## Required gates before any live order

- `ENABLE_LIVE_TRADING=true`
- `LIVE_TRADING_ALLOWED_BROKERS=kalshi`
- `LIVE_TRADING_ALLOWED_STRATEGIES=<strategy uuid>`
- `KALSHI_API_KEY_ID`
- `KALSHI_PRIVATE_KEY_PEM_B64`
- A real Kalshi live client wired and initialised

## Preflight checks

Run these before any live activation work:

```bash
curl -sf http://10.0.0.56:3030/healthz
curl -sf http://10.0.0.56:3029/kalshi
# Authenticated API check, run with an operator token/session if available:
curl -sf http://10.0.0.56:3030/api/v1/kalshi/summary
```

Check the latest discovery run and watched market state in the dashboard/API
before changing strategy mode. Confirm the active paper strategy is healthy and
has the expected market/ticker history.

Focused validation:

```bash
rtk go test ./internal/execution/kalshi -run 'Broker|Map|Reconciler' -count=1
rtk go test ./cmd/tradingagent -run 'Kalshi|LiveGate|Broker|Paper|Strategy' -count=1
```

## First live strategy procedure

1. Clone one proven paper strategy.
2. Keep max sizing tiny.
3. Set `is_paper=false` only after every gate above is satisfied and the live
   client is ready.
4. Run the strategy manually once.
5. Verify the submitted order, broker response, and reconciliation/position
   state.

## Rollback procedure

1. Remove `kalshi` from `LIVE_TRADING_ALLOWED_BROKERS`.
2. Remove the strategy UUID from `LIVE_TRADING_ALLOWED_STRATEGIES`.
3. Set the strategy back to `is_paper=true`.
4. Restart the app.
5. Verify `http://10.0.0.56:3030/healthz` is still healthy.

## Safety notes

- Do not place secrets in this document.
- Do not auto-promote paper strategies to live.
- Do not enable discovery to create live strategies.
- Keep paper/data defaults as the normal operating mode until a deliberate
  live change is approved and wired end-to-end.
