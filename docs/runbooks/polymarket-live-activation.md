---
title: "Polymarket live activation"
date: 2026-06-14
tags: [runbook, operations, polymarket, trading]
type: runbook
---

# Polymarket live activation

> **Retired deployment path:** Polymarket activation is prohibited by policy
> for the US deployment. Keep `ENABLE_POLYMARKET_AUTOMATION=false`, do not add
> Polymarket to a live broker allowlist, and use Kalshi for supported event-market
> data, paper execution, and settlement. The material below is retained only as
> historical architecture documentation and is not an active operating procedure.

## Context

Use this runbook to move a Polymarket strategy from paper trading to live execution. Paper remains the default. Live trading requires the strategy itself to be switched out of paper mode (`is_paper=false`), an explicit global flag, strategy allowlist, broker allowlist, and Polymarket credentials. Runtime now also bootstraps open Polymarket stop-guards at startup and runs audit-only reconciliation on a schedule; those jobs do not change trading semantics.

## Required environment

Set these values in the deployment environment:

```text
ENABLE_LIVE_TRADING=true
LIVE_TRADING_ALLOWED_STRATEGIES=<strategy-uuid>
LIVE_TRADING_ALLOWED_BROKERS=polymarket
POLYMARKET_KEY_ID=<key-id>
POLYMARKET_SECRET_KEY=<secret-key>
POLYMARKET_PASSPHRASE=<api-passphrase>
POLYMARKET_ADDRESS=<polygon-signer-address>
```

`POLYMARKET_KEY_ID`, `POLYMARKET_SECRET_KEY`, and `POLYMARKET_PASSPHRASE`
are Polymarket CLOB L2 API credentials created or derived from L1 wallet
authentication. A wallet address alone is not sufficient: the L1 flow signs an
EIP-712 message with the wallet private key and returns `{apiKey, secret,
passphrase}`. The app also supports existing local env names
`POLYMARKET_API_KEY` and `POLYMARKET_SECRET` as aliases for the L2 apiKey and
secret, but only when they already contain derived L2 credentials.

Optional Polymarket endpoint overrides:

```text
POLYMARKET_API_BASE_URL
POLYMARKET_GATEWAY_BASE_URL
POLYMARKET_CLOB_URL
```

Risk controls used by Polymarket execution:

```text
RISK_POLYMARKET_MAX_SINGLE_EXPOSURE_PCT
RISK_POLYMARKET_MAX_TOTAL_EXPOSURE_PCT
RISK_POLYMARKET_MAX_POSITION_USDC
RISK_POLYMARKET_MIN_LIQUIDITY_USDC
RISK_POLYMARKET_MAX_SPREAD_PCT
RISK_POLYMARKET_MIN_DAYS_TO_RESOLUTION
```

Emergency rollback / kill switch:

```text
TRADING_AGENT_KILL=true
```

## Readiness gates

- Paper trading is the default until live trading is explicitly enabled.
- The strategy record must be explicitly set to `is_paper=false`; strategies with `is_paper=true` stay on paper even when the env gates pass.
- The strategy must be in `LIVE_TRADING_ALLOWED_STRATEGIES`.
- The broker must be `polymarket` and listed in `LIVE_TRADING_ALLOWED_BROKERS`.
- Polymarket L2 auth requires address, apiKey/key ID, secret, and passphrase.
- Complete at least 60 days of paper burn-in before activation. This is a manual/operator readiness gate unless `CheckLiveReadiness` is wired into runtime later.
- Confirm no outstanding validation failures remain for the strategy or market setup. This is a manual/operator readiness gate unless `CheckLiveReadiness` is wired into runtime later.

## Activation steps

1. Confirm the strategy has completed 60 days of paper trading with stable fills and no unresolved validation failures.
2. Update the strategy record to `is_paper=false` for the exact strategy ID being promoted.
3. Verify the deployment environment includes the required env values above.
4. Restart or redeploy the trading agent so the new environment is loaded.
5. Confirm the strategy now routes through the live path for Polymarket.
6. Confirm startup logs show stop-guard bootstrap and scheduled Polymarket reconciliation registration.
7. Monitor order submission, broker responses, risk checks, and reconciliation audit events closely for the first live session.

## Dry-run / burn-in acceptance criteria

- Paper runs must stay paper-only during the burn-in period.
- The strategy should remain stable for the full 60-day window.
- No unresolved validation failures should be present at activation time.
- Only strategies explicitly allowlisted for live trading may proceed.

## Rollback to paper

1. Set the strategy record back to `is_paper=true`.
2. Set `ENABLE_LIVE_TRADING=false`.
3. Remove the strategy from `LIVE_TRADING_ALLOWED_STRATEGIES` if you want a hard block.
4. Keep or clear `TRADING_AGENT_KILL=true` depending on whether you need a full stop.
5. Restart or redeploy the trading agent.
6. Verify the strategy is back on paper execution before resuming monitoring.
