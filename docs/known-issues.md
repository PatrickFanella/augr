---
title: "Known Issues"
description: "Current implementation gaps, repo-health problems, and behavioral caveats for get-rich-quick."
status: "canonical"
updated: "2026-04-08T16:00:00Z"
tags: [known-issues, limitations]
---

# Known Issues

This page is intentionally blunt. It exists so contributors and operators do not lose time assuming the happy path is more complete than it really is.

## Product and control-plane gaps

### ~~WebSocket authentication is not enforced~~ ✓ Fixed

`GET /ws` now enforces authentication before upgrading the connection. Clients
pass credentials via the standard `Authorization: Bearer <token>` or `X-API-Key`
headers, or via `?token=<jwt>` / `?api_key=<key>` query parameters (for browser
WebSocket clients that cannot send custom headers).

### ~~Settings edits are in-memory only~~ ✓ Fixed

Non-secret settings (model selections, provider base URLs, risk thresholds) are now
persisted to the `app_settings` table (migration 000024). `PUT /api/v1/settings`
saves to Postgres on every successful update and restores on startup via
`MemorySettingsService.WithPersister`. API keys are never stored.

### ~~There is no user registration flow~~ ✓ Fixed

`POST /api/v1/auth/register` now accepts `{username, password}`, creates the user,
and returns a token pair. Duplicate usernames return `409 Conflict`.

### ~~Current-user and API key management endpoints are missing~~ ✓ Fixed

- `GET /api/v1/me` returns the authenticated user's profile (id, username, timestamps).
- `GET /api/v1/api-keys` lists all API keys (metadata only — raw key is never re-exposed).
- `POST /api/v1/api-keys` creates a new API key; returns the plaintext key once alongside metadata.
- `DELETE /api/v1/api-keys/{id}` revokes a key.

## Runtime and execution caveats

### ~~Backtest capability exists below the product surface~~ ✓ Fixed

Backtests are now fully exposed: `GET/POST /api/v1/backtests/configs`,
`POST /api/v1/backtests/configs/{id}/run`, `GET /api/v1/backtests/runs`.
Configs with a `schedule_cron` field are automatically scheduled and run by
the built-in cron engine.

### ~~Polymarket support is incomplete~~ ✓ More complete than documented

The production strategy runner now handles `market_type: polymarket` strategies through the retail Polymarket US API:
- Preserves trader-selected outcome side through execution and maps YES/NO plus Up/Down/Over/Under intents for supported retail markets
- Routes live orders through `polymarketexecution.Broker` when `POLYMARKET_KEY_ID` and `POLYMARKET_SECRET_KEY` are set
- Falls back to local paper broker when `is_paper: true` (Polymarket has no native paper mode)
- Enforces per-market exposure, liquidity, spread, and resolution-timeline risk limits

Configure with `POLYMARKET_KEY_ID`, `POLYMARKET_SECRET_KEY`, optional `POLYMARKET_API_BASE_URL`/`POLYMARKET_GATEWAY_BASE_URL`, and the `POLYMARKET_*` risk limit variables. Legacy data and signal jobs may still read `POLYMARKET_CLOB_URL` during the migration. See `docs/reference/configuration.md`.

### Social and news coverage are uneven

The `DataProvider` abstraction includes OHLCV, fundamentals, news, and social sentiment, but coverage varies by provider:

| Provider | OHLCV | Fundamentals | News | Social Sentiment |
| --- | --- | --- | --- | --- |
| Yahoo | ✓ | — | — | — |
| Polygon | ✓ | ✓ | — | — |
| Finnhub | partial (free tier 403s on bulk US stocks) | ✓ | ✓ | ✓ (Reddit + Twitter) |
| FMP | ✓ | ✓ | — | — |
| AlphaVantage | ✓ (25 req/day free) | ✓ | — | — |
| NewsAPI | — | — | ✓ | — |
| Binance | ✓ (crypto) | — | — | — |

Impact:

- Social sentiment data requires `FINNHUB_API_KEY` — social signals are absent without it
- StockTwits trending/sentiment is available via the automation job engine but is **not** part of the `DataProvider` chain

### ~~API-toggle kill-switch state was lost on restart~~ ✓ Fixed

Kill-switch activations via `POST /api/v1/risk/killswitch` are now persisted to the
`risk_state` table (migration 000025) and restored on startup. An operator-activated
kill-switch survives process restarts. File-flag and environment-variable mechanisms
are unchanged and always re-evaluated at runtime.

### ~~Whole-pipeline timeout is not currently enforced~~ ✓ Fixed

`runtimePipelineTimeout` now derives a finite wall-clock budget from the per-phase
timeout settings: `(analysts × analysis_timeout) + (2 × rounds × debate_timeout) + overhead`.
Falls back to 30 minutes when any constituent is unconfigured.

### ~~Stale runs remained stuck at `running` forever~~ ✓ Fixed

The runtime now starts a stale-run reconciler that sweeps `pipeline_runs` for
`status='running'` rows older than `STALE_RUN_TTL` (default `30m`), marks them
`failed`, writes a `pipeline_run.stale_reconciled` audit entry, increments
`tradingagent_stale_runs_reconciled_total`, and best-effort cancels any still-registered
in-process run context.

### ~~Pagination `total` field never populated~~ ✓ Fixed (except memories, conversation messages)

`GET /api/v1/strategies`, `/runs`, `/runs/{id}/decisions`, `/backtests/configs`, `/backtests/runs`,
`/audit-log`, `/orders`, `/portfolio/positions`, `/portfolio/positions/open`, `/trades`, `/events`,
`/conversations`, `/api-keys`, and `/discovery/results` all return a `total` field.
Each calls a `SELECT COUNT(*)` with the same filter conditions as the page query. Count
errors are logged but do not fail the list response. Remaining without `total`: memories
(full-text search semantics differ), `/conversations/{id}/messages` (synthetic message
injection from decisions makes a DB count inaccurate).

### ~~Operator actions were not audited~~ ✓ Fixed

`internal/api/handlers.go` now writes an `AuditLogEntry` (best-effort, never blocks the handler)
for every critical operator action:

| Event type | Trigger |
| --- | --- |
| `kill_switch.activated` / `.deactivated` | `POST /api/v1/risk/killswitch` |
| `market_kill_switch.activated` / `.deactivated` | `POST /api/v1/risk/markets/{type}/stop\|resume` |
| `settings.updated` | `PUT /api/v1/settings` |
| `strategy.manual_run` | `POST /api/v1/strategies/{id}/run` |
| `strategy.paused` / `.resumed` | `POST /api/v1/strategies/{id}/pause\|resume` |
| `strategy.skip_next` | `POST /api/v1/strategies/{id}/skip-next` |
| `user.registered` | `POST /api/v1/auth/register` |
| `api_key.created` / `.revoked` | `POST/DELETE /api/v1/api-keys` |
| `backtest.run` | `POST /api/v1/backtests/configs/{id}/run` |

Entries are queryable via `GET /api/v1/audit-log`.

## Documentation caveats

### Older design docs can overstate maturity

`docs/design/` contains valuable architecture intent, but parts of it describe the target system more cleanly than the currently wired system deserves.

Impact:

- prefer [Reference](reference/README.md) for implementation truth
- use design docs for rationale and direction

## Practical advice

Before debugging anything complicated:

1. Check whether the file area is currently in a conflicted state.
2. Verify the route or page is actually mounted in the current server/router.
3. Confirm whether the feature is persisted or merely in-memory.
4. Confirm whether the provider/integration is present only in config/types or actually instantiated in runtime wiring.
