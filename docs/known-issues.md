---
title: "Known Issues"
description: "Current implementation gaps, repo-health problems, and behavioral caveats for get-rich-quick."
status: "canonical"
updated: "2026-04-08"
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

### Polymarket support is incomplete

`polymarket` exists as a market type and there is a Polymarket execution package, but the main production strategy runner does not present live Polymarket execution as a complete, operator-friendly supported path.

Impact:

- treat Polymarket as partial support, not finished support

### Social and news coverage are uneven

The `DataProvider` abstraction includes OHLCV, fundamentals, news, and social sentiment, but not every provider implements every surface. `newsapi` is now wired into the runtime provider chain for stock news (`NEWSAPI_API_KEY`). Finnhub is registered in the provider registry for OHLCV/social sentiment.

Impact:

- “feature exists in interface” does not always mean “feature is active in production runtime wiring”

### ~~Whole-pipeline timeout is not currently enforced~~ ✓ Fixed

`runtimePipelineTimeout` now derives a finite wall-clock budget from the per-phase
timeout settings: `(analysts × analysis_timeout) + (2 × rounds × debate_timeout) + overhead`.
Falls back to 30 minutes when any constituent is unconfigured.

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
