---
title: "Release readiness and recovery drills"
status: "canonical"
updated: "2026-07-12"
---

# Release readiness and recovery drills

Live trading stays disabled throughout this gate. Query
`GET /api/v1/release/readiness` with an authenticated operator token. Every
required paper capability must be ready; `live_execution` remains a separate,
non-required, blocked capability until a broker/market/strategy/capital-tier
activation is explicitly approved.

Run the automated gate from the repository root:

```sh
./scripts/release-gate.sh
```

Validate Prometheus rules with `promtool check rules
monitoring/prometheus/alerts.yml` (or the matching Prometheus container image).
Do not set `RELEASE_DRILLS_VERIFIED=true` until the evidence table below is
complete for the deployment being promoted. The flag only records operator
attestation; it does not bypass any capability check or enable live trading.

| Drill | Required evidence | Recovery criterion |
|---|---|---|
| Restart | rolling-restart steps, schema gate output, paper account bootstrap logs | runtime returns healthy with kill-switch and durable positions restored |
| Dependency outage | broker and LLM outage tests/runbooks, alert delivery | deterministic/paper paths fail closed or fall back as documented |
| Stale data | snapshot freshness tests and provider last-success metric | entry is rejected and stale source is identified |
| Order rejection | order-manager rejection test and journal/replay row | no position appears; rejection remains explainable |
| Partial fill | fill-engine/broker partial-fill tests and reconciliation result | filled quantity, cash, trade, and position agree |
| Reconciliation | Alpaca, Polymarket, Kalshi, and options reconciliation tests | zero unexplained drift; any deliberate fixture drift alerts |
| Kill switch | API/file/env and mid-run cancellation tests | new orders stop and active execution is cancelled safely |
| WebSocket reconnect | API smoke/reconnect tests | authenticated reconnect resumes without corrupting persisted state |
| Prediction settlement | shared settler and provider-job tests | 0/1 payout, P&L, closed decision, and replay outcome agree |
| Options expiration | expiry workflow tests | worthless and intrinsic cash settlement persist correctly |
| Options assignment | explicit paper assignment-boundary test | no underlying shares are fabricated; paper options cash-settle and live assignment remains blocked |

For a real soak, run at least one complete scheduler cycle for each enabled
paper market, inspect `/api/v1/automation/status`, `/api/v1/risk/cockpit`, the
decision journal/replay, and Prometheus alerts, then attach timestamps and query
outputs to the release record. Any reconciliation drift, incomplete decision
journal, missing settlement, or unexplained alert fails the release.
