---
title: "ADR-009: Human review gate before live trading"
description: "Architecture decision record."
status: "superseded"
updated: "2026-08-14"
tags: [adr]
---

# ADR-009: Human review gate before live trading

- **Status:** superseded by [ADR-019](019-deterministic-ai-order-boundary.md)
- **Date:** 2026-03-27
- **Deciders:** Engineering
- **Technical Story:** [#112](https://github.com/PatrickFanella/get-rich-quick/issues/112)

## Context

The kill switch (API toggle, file flag, environment variable) is the primary manual control. There is no pre-trade approval flow — once the pipeline produces a signal and the risk engine approves it, the order executes immediately. For live trading with real capital, large or unusual orders should require human confirmation.

## Decision

Implement a **threshold-based approval gate** between signal extraction and order submission. The gate applies only to live trading; paper trading is unaffected.

### Threshold tiers

| Condition                        | Action                                       | Timeout                                        |
| -------------------------------- | -------------------------------------------- | ---------------------------------------------- |
| Paper trading (any size)         | Auto-execute, no gate                        | —                                              |
| Live, order notional < $5K       | Auto-execute + notify via webhook            | —                                              |
| Live, order notional $5K–$25K    | Queue + notify, auto-approve if no rejection | 5 minutes                                      |
| Live, order notional > $25K      | Queue + require explicit approval            | No timeout (blocks until approved or rejected) |
| Any SELL of full position (live) | Queue + notify, auto-approve if no rejection | 2 minutes                                      |

### Implementation approach

1. **`ApprovalGate` interface** — sits between the pipeline's final signal and the order manager. Paper mode uses a no-op passthrough. Live mode checks order notional against thresholds.
2. **Queue backed by PostgreSQL** — not Redis. Orders pending approval must survive process restarts. A new `pending_approvals` table with `order_id`, `notional`, `threshold_tier`, `created_at`, `expires_at`, `status` (pending/approved/rejected/expired).
3. **Notification via webhook** — HTTP POST to a configured URL (Telegram Bot API, Discord webhook, or Slack incoming webhook). The notification includes ticker, side, quantity, notional value, and a deep link to approve/reject via the API.
4. **Auto-approve timeout** — for the $5K–$25K tier, a background goroutine checks `expires_at` and auto-approves if no rejection was received. This prevents blocking 24/7 crypto trading overnight when the operator is asleep.

### Why not gate paper trading?

Paper trading validation needs to run unattended for 60 days. Gating paper trades would defeat the purpose of autonomous validation. The kill switch remains available as an emergency stop for paper mode.

### Latency impact

- Paper trading: 0ms additional latency.
- Live, <$5K: 0ms (notification is async).
- Live, $5K–$25K: 0–5 minutes (auto-approves after timeout).
- Live, >$25K: Unbounded (requires human action).

This is acceptable because the system's edge is analytical (LLM reasoning), not speed. Signals are valid for minutes to hours, not milliseconds.

## Consequences

- Live trading cannot execute large orders without human awareness. This prevents a single bad LLM output from committing significant capital.
- The auto-approve timeout for mid-tier orders means the system remains 24/7 operational for moderate-sized trades even when the operator is unavailable.
- Requires the API layer (#74) and a notification webhook integration. The approval gate should be implemented after the REST API is in place.
- The PostgreSQL-backed queue adds one table and one background goroutine. Minimal complexity increase.
