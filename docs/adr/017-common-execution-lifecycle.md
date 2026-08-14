---
title: "ADR-017: Common intent and execution lifecycle"
description: "Use one idempotent state machine for backtest, paper, shadow, and future live execution."
status: "accepted"
updated: "2026-08-14"
tags: [adr, execution, lifecycle, simulation]
---

# ADR-017: Common intent and execution lifecycle

- **Status:** accepted
- **Date:** 2026-08-14
- **Deciders:** Project owner, Engineering
- **Technical Story:** [Augr total overhaul plan](../superpowers/plans/2026-08-14-total-overhaul-plan.md)

## Context

Backtests, the in-memory paper broker, external paper venues, and venue-specific execution paths currently make different assumptions about fills and state transitions. A strategy can therefore appear profitable in one path without proving that the same timestamped intent could have survived routing, partial fills, fees, cancellation, restart, and reconciliation.

## Decision

Adopt a single append-only lifecycle for every execution environment. An intent describes the desired economic change and evidence; it is not a broker order.

```text
proposed -> allocated -> risk_approved -> routed -> working
                                      \-> risk_rejected
working -> partially_filled -> filled
working -> cancelled | expired | rejected
any nonterminal state -> failed_reconciliation
```

- Intent and order creation require deterministic idempotency keys.
- Every transition records source and receive timestamps, actor, reason, evidence snapshot, environment, account, strategy version, and simulation or venue policy version.
- Backtest, `paper_scored`, `paper_stress`, shadow, external paper, and future live paths use the same transition rules. They differ only in venue capabilities and fill source.
- Simulation consumes executable bid/ask, depth, latency, calendar, tick, lot, fee, and settlement policies. Missing required data fails closed; a missing spread is never zero.
- Reconciliation compares local state with the venue or simulation event log and emits classified drift events rather than silently correcting history.
- Restart at any transition must neither lose nor duplicate an order or economic event.

## Consequences

### Positive

- Paper and backtest evidence become comparable to future routed execution.
- Partial fills, retries, restarts, and venue differences are tested in one place.
- Economic events can link cleanly to the ledger selected in ADR-016.

### Negative

- Existing order managers and brokers require adapters during migration.
- Strict stale-data and capability checks will turn some previous “trades” into deliberate rejections.

### Neutral

- This lifecycle can prove operational fidelity; it cannot create alpha by itself.
