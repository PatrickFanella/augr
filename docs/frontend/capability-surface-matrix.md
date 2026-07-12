---
title: "Frontend Capability Surface Matrix"
status: "active"
updated: "2026-07-12"
tags: [frontend, api, operations, safety]
---

# Frontend capability surface matrix

This matrix is the Phase 4 contract between the HTTP API and the operator UI.
An HTTP route is not evidence of a complete product workflow. `Exposed` means
the current shell has a typed, validated, authenticated surface with explicit
loading, empty, error, and unavailable behavior. `API-only` means the route is
supported for automation or expert diagnostics but deliberately has no current
UI control. Live trading remains disabled.

| Capability | Current surface | API coverage | Classification | Completion boundary |
|---|---|---|---|---|
| Runtime settings and readiness | `/settings` | `GET /settings` | Exposed, read-only | Effective environment, schema, broker mode, LLM readiness, and risk limits are visible. Secret values and mutation are excluded. |
| Core strategies, runs, events, orders, trades, portfolio, and risk | Existing current-shell routes | Primary operator endpoints | Exposed | These are the validated operator MVP. Mutating actions remain paper-gated or safety-confirmed. |
| Event-market overview | `/event-markets` | Shared summary and Polymarket feed status | Exposed, read-only | Provider readiness and recorder health are visible without implying live readiness. |
| Detailed Polymarket/Kalshi operations | None | Account, watched-market, trade, signal, discovery, and provider-specific routes | API-only | Phase 6 unified deterministic evidence, explicitly uncalibrated probability proxies, execution replay, reconciliation, and settlement. Provider-specific mutation controls remain API/runbook-only pending hardened operator workflows. |
| Options research and lifecycle evidence | `/options`, `/orders`, `/portfolio` | Chain plus shared order/position/trade routes | Exposed, read-only operations | Chain, liquidity, IV, Greeks, OCC identity, intent, multiplier, and leg groups are visible. Paper execution is automation-driven; live, uncovered-short, credit-spread, and manual order controls remain disabled. |
| Backtest evidence | `/backtests` | Config, run list, and strategy-scoped divergence routes | Exposed, read-only | Definitions, windows, prompt/simulation versions, exact input hashes, and persisted runs are visible. Fill assumptions, next-bar execution, divergence, and segmented calibration are implemented; run/edit controls remain deliberately excluded. |
| Decision journal | `/journal` | Journal list/detail routes | Exposed, read-only | Risk reasoning, decision state, LLM provenance, and paper/live order references are visible. |
| Decision replay | `/replay/decisions/{id}` | Replay workbench route | Exposed, read-only | Persisted lifecycle events and payloads are ordered and shown; no synthetic events are inferred. |
| Discovery and signal evaluation | None | Discovery run/results, signal evaluated/triggers/watchlist | API-only | Triggering discovery can consume providers and create downstream candidates. Keep it automation/API operated until quotas, provenance, review, and safe promotion are one cohesive workflow. |
| Reliability and metrics | Existing readiness, risk, automation, and runbook surfaces | Health, readiness, risk, automation, Prometheus metrics | Split | Operator readiness is exposed. High-cardinality metrics and alert administration remain API/Prometheus-only and are completed in Phase 8. |
| Agent memory | None | List, search, and delete memory | API-only | Memory content may contain sensitive prompt context; deletion changes future decisions. A UI requires provenance, retention, relevance, and confirmation controls first. |
| Prompt administration | None | Get/update prompts | API-only | Prompt mutation changes decision behavior. It remains deployment/API controlled until versioning, diff, validation, rollback, and audit controls exist. |
| API key administration | None | List/create/revoke keys | API-only | Key creation returns one-time credentials and revocation is destructive. Use the authenticated API until a hardened reveal, copy, expiry, confirmation, and audit workflow exists. |
| Universe and watchlist | None | Universe list, watchlist, refresh, and scan routes | API-only | Refresh/scan consumes data-provider capacity and can affect discovery. Keep it scheduled/API operated pending quota, freshness, and provenance UX. |
| Market calendars | None | Earnings, economic, filings, analysis, and IPO routes | API-only | Raw provider calendars remain research APIs until normalization, source attribution, timezone, stale-state, and partial-provider behavior are consistent. |
| Surfers bot operations | None | Separate bot/runtime documented in `docs/surfers-bot.md` | API/runtime-only | It has no authenticated Augr operator contract. Do not add a cosmetic dashboard until ownership, health, commands, and failure semantics are integrated. |

## Required UI behavior

Every exposed query owns its state independently. One unavailable dependency
must not hide valid sibling data. `501` is rendered as feature unavailable,
empty data is distinguished from failure, stale or last-updated context is
shown where timestamps exist, and unknown enum values remain visible as text.
No current Phase 4 surface can place a live order or enable live trading.

## Promotion rule for API-only capabilities

A capability moves to `Exposed` only with typed response validation, current
shell navigation, accessibility and responsive review, explicit loading/empty/
error/unavailable/stale behavior, fixture-backed tests, and a documented safety
boundary for mutations. A button that merely invokes an endpoint does not meet
this bar.
