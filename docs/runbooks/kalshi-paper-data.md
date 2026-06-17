---
title: "Kalshi paper/data setup"
date: 2026-06-15
tags: [runbook, operations, kalshi, trading]
type: runbook
---

# Kalshi paper/data setup

## Context

Use this runbook to configure Kalshi for paper/data-first operation. This plan only enables configuration, validation, and read-side readiness. Live Kalshi order submission is not enabled by this plan.

## Paper/data defaults

```text
KALSHI_DEMO=true
KALSHI_API_BASE_URL=https://external-api.demo.kalshi.co/trade-api/v2
```

These defaults are enough for paper/data wiring that uses public or stubbed data paths.

## Optional authenticated demo/future live environment

```text
KALSHI_API_KEY_ID=<demo key id>
KALSHI_PRIVATE_KEY_PEM_B64=<base64 encoded RSA private key PEM>
```

## Notes

- Keep `KALSHI_DEMO=true` for demo and paper/data workflows.
- The API key ID and private key are only needed for authenticated demo reads or future live work.
- Do not enable live Kalshi order submission in this phase.
