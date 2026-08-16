---
title: "ADR-018: Isolate scored and stress paper modes"
description: "Separate promotion-quality paper evidence from unlimited synthetic stress testing."
status: "accepted"
updated: "2026-08-14"
tags: [adr, paper-trading, experiments, risk]
---

# ADR-018: Isolate scored and stress paper modes

- **Status:** accepted
- **Date:** 2026-08-14
- **Deciders:** Project owner, Engineering
- **Technical Story:** [Augr total overhaul plan](../superpowers/plans/2026-08-14-total-overhaul-plan.md)
- **Supersedes:** [ADR-006: Paper trading slippage and fee assumptions](006-paper-trading-assumptions.md)

## Context

Augr needs both honest paper evidence and deliberately unrealistic high-capacity chaos testing. Combining them makes return rankings meaningless: unlimited buying power can hide capital constraints, while zero-cost fills can manufacture profit. The initial operating account is $100,000 with margin capability; future deposits may range from $500 to more than $5 million.

## Decision

Define exactly two paper evaluation identities:

| Mode | Evidence class | Purpose | Promotion eligible |
| --- | --- | --- | --- |
| `paper_scored` | `promotion_evidence` | Broker-realistic evaluation at declared capital and constraints | Yes |
| `paper_stress` | `synthetic_stress` | Chaos, throughput, capacity, and emergency-brake testing | Never |

- Mode, evidence class, and storage namespace are required attributes of every paper account, experiment, result, metric, API response, and UI view.
- Scored and stress data cannot share an account or storage namespace and cannot appear in one ranking population.
- The default scored profile starts with $100,000, a requested 2x buying-power policy, 5 bps slippage, and 0.01% fee rate. These are conservative Phase 0 defaults, not a claim of venue parity.
- `paper_stress` may request multiplier `0` to represent future unlimited buying power. No output from that profile may be used for promotion, Sharpe ranking, or profitability claims.
- Margin enforcement, deposits, and tiered capital profiles are implemented with the account and ledger work. Until then, the profile records the requested policy and the current broker applies declared starting capital and execution costs.
- Venue-specific quote, fee, borrow, assignment, settlement, and market-impact policies replace ADR-006's fixed market table as the common simulator is built.

## Local implementation

OVR-204's canonical internal-paper adapter accepts only a valid matching
`paper_scored` or `paper_stress` account/lifecycle pair. The simulator retains
the account environment, evidence class, and storage namespace in every fill's
source namespace and evidence. The canonical outcome hash also includes those
three fields. Backtest and paper hashes therefore match only inside the same
mode; otherwise identical scored and stress fills intentionally produce
different canonical bytes and hashes.

This does not promote legacy paper output. The in-memory compatibility broker
and relocated float/bar models remain labeled `backtest-input-v1`; an unpriced
market order now fails closed instead of inventing `$1.00`. No runtime writer,
scheduler, or shared database has been activated. See the
[common simulation venue runbook](../runbooks/common-simulation-venue.md).

## Consequences

### Positive

- Stress tests can be as extreme as useful without contaminating investment evidence.
- Dashboards and metrics can state exactly which economic assumptions produced a result.
- Capital-tier testing becomes possible without redefining return history.

### Negative

- Existing unlabeled results are legacy evidence and cannot be promoted automatically.
- Storage, APIs, metrics, and UI filters must carry the mode dimension consistently.

### Neutral

- Scored mode is more honest, but it is still simulation rather than a promise of live fills.
