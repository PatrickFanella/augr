---
title: "Kalshi paper/data setup"
date: 2026-06-15
tags: [runbook, operations, kalshi, trading]
type: runbook
---

# Kalshi paper/data setup

## Context

Use this runbook to configure Kalshi for paper/data-first operation. This plan only enables configuration, validation, and read-side readiness. Live Kalshi order submission is not enabled by this plan.
The runtime may wire a live adapter when credentials exist, but paper remains the
default unless the explicit live gates and a non-paper strategy are configured.

Related live activation checklist: [`kalshi-live-readiness.md`](./kalshi-live-readiness.md).

## Paper/data defaults

```text
KALSHI_DEMO=true
KALSHI_API_BASE_URL=https://external-api.demo.kalshi.co/trade-api/v2
```

These defaults are enough for paper/data wiring that uses public or stubbed data paths.

Keep these defaults in place for the normal setup. Discovery should continue to
create or reuse **paper** strategies only.

## Optional authenticated demo/future live environment

```text
KALSHI_API_KEY_ID=<demo key id>
KALSHI_PRIVATE_KEY_PEM_B64=<base64 encoded RSA private key PEM>
```

## Notes

- Keep `KALSHI_DEMO=true` for demo and paper/data workflows.
- The API key ID and private key are only needed for authenticated demo reads or future live work.
- Do not enable live Kalshi order submission in this phase; credentials alone do not switch strategies out of paper.
- Do not set `is_paper=false` from this runbook.
- For live-readiness steps, use the linked live runbook instead of changing the paper/data defaults here.

## Discovery automation

- The scheduled automation job is `kalshi_discovery`.
- Default cadence is hourly at minute 15: `15 * * * *`.
- The job fetches open Kalshi markets, stores recent snapshots, upserts screened watched markets, and creates/reuses active **paper** strategies only.
- Strategy reuse is keyed by `market_type=kalshi` and Kalshi market `ticker`, so repeated runs should not create duplicate paper strategies for the same market.
- Discovery does not create live strategies.
- The conservative job defaults currently fetch at most 50 markets, deploy at most 1 paper strategy per run, and require minimum conviction `0.70`.

## Dashboard and API checks

- Dashboard route: `/kalshi`.
- Summary API: `GET /api/v1/kalshi/summary`.
- The summary payload includes enabled watched markets, latest snapshots, latest discovery status, and active paper strategy count.
- A healthy page may still show empty arrays before the first discovery run; that is expected.

## Operator validation

```bash
rtk go test ./internal/kalshidiscovery ./internal/automation ./internal/api ./cmd/tradingagent -run 'Kalshi|Automation|Discovery|Server|Strategy' -count=1
cd web && npm test -- --run src/pages/kalshi-page.test.tsx src/components/layout/app-shell.test.tsx
cd web && npm run build
```

Deploy-time checks after migrations and app restart:

```bash
curl -sf http://10.0.0.56:3030/healthz
curl -sf http://10.0.0.56:3029/kalshi
```
