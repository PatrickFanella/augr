---
title: "ADR-017: Common intent and execution lifecycle"
description: "Use one idempotent state machine for backtest, paper, shadow, and future live execution."
status: "accepted"
updated: "2026-08-20"
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
routed | working -> partially_filled -> filled
routed | working -> filled
routed | working | partially_filled -> cancelled | expired | rejected
any nonterminal state -> failed_reconciliation
```

- Intent and order creation require deterministic idempotency keys.
- Every transition records source and receive timestamps, actor, reason, evidence snapshot, environment, account, strategy version, and simulation or venue policy version.
- Backtest, `paper_scored`, `paper_stress`, shadow, external paper, and future live paths use the same transition rules. They differ only in venue capabilities and fill source.
- Simulation consumes executable bid/ask, depth, latency, calendar, tick, lot, fee, and settlement policies. Missing required data fails closed; a missing spread is never zero.
- Reconciliation compares local state with the venue or simulation event log and emits classified drift events rather than silently correcting history.
- Restart at any transition must neither lose nor duplicate an order or economic event.

## Local implementation

OVR-203 implements this decision additively in `internal/execution/lifecycle`
and migration 71. One account-scoped deterministic intent permits one immutable
order command and one immutable external binding. State is replayed only from
append-only events serialized by the intent row; `routed`, `working`, and
`partially_filled` are the only restart-recovery states.

A first provider observation may establish the binding and report a partial or
complete fill in one `fill_acknowledged` transition. Every accepted fill is
committed atomically with its raw-evidence normalization, ledger transaction,
optional first binding, and lifecycle event. Ordinary revisions conflict;
explicit correction and bust identities append `failed_reconciliation` without
altering economics. Revision identity is anchored to the original provider
execution ID and discriminator while the later observation ID remains exact
payload evidence. Cumulative fill quantity exists only on fill events in both
the domain and database contracts. See the
[common execution lifecycle runbook](../runbooks/common-execution-lifecycle.md).

OVR-204 now supplies the first common-lifecycle simulator locally. One immutable
content-addressed policy governs quote/depth eligibility, latency, sessions,
depth participation, exact fees, and each deterministic level fill. Every fill
creates raw source evidence first and then uses the OVR-203 atomic fill writer;
restart reloads the policy bytes by the order's recorded version and skips an
already-recorded source identity. Canonical backtest and internal-paper
adapters therefore share the same request, transitions, policy version, and
economic outcome hash within one ADR-018 mode. See the
[common simulation venue runbook](../runbooks/common-simulation-venue.md).

OVR-205 now supplies additive Alpaca Trading API v2 and Kalshi Trade API v2
adapters locally. Each route is governed by one independently reconstructed,
content-addressed venue policy. Stable lifecycle order IDs become provider
client IDs; exact provider bytes are journaled before interpretation; and every
authoritative fill continues through the same OVR-103/203 normalization,
ledger, binding, fill, and event graph. Alpaca stream fills are notices while
account-activity `FILL` identities are economic. Kalshi accepts only
`resting`, `canceled`, and `executed`; executed is evidence-only until exact
fill IDs already account for the full order. Current/historical recovery,
ambiguous submission, cancellation commands, corrections, busts, unknown
states, and contradictions all fail closed without a guessed order or fill.
See the [Alpaca and Kalshi common-lifecycle runbook](../runbooks/alpaca-kalshi-common-lifecycle.md).

This local implementation does not activate a writer, scheduler, provider
credential, or external venue route and does not cut over legacy runtime paths.
Production activation, external-paper fidelity, writer identity, protected
database migration, reconciliation, alerting, and cutover remain separate
reviewed decisions.

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
