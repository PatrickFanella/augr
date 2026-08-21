---
title: "ADR-016: Immutable double-entry economic ledger"
description: "Make balanced economic events the source of truth for every paper and future live account."
status: "accepted"
updated: "2026-08-14"
tags: [adr, ledger, accounting, reconciliation]
---

# ADR-016: Immutable double-entry economic ledger

- **Status:** accepted
- **Date:** 2026-08-14
- **Deciders:** Project owner, Engineering
- **Technical Story:** [Augr total overhaul plan](../superpowers/plans/2026-08-14-total-overhaul-plan.md)

## Context

Cash, positions, fees, settlements, and realized P&L are currently mutated across broker, order-manager, and venue-specific paths. The profitability audit found zero recorded fees, unmarked positions, and no durable account boundary. That makes results difficult to reconcile, replay, or compare after a deposit. Augr must eventually support account sizes from $500 to more than $5 million without resetting history or silently changing accounting meaning.

## Decision

Introduce one append-only, double-entry economic ledger as the authoritative source for cash, collateral, liabilities, inventory, realized P&L, fees, interest, borrow, settlement, assignment, deposits, and withdrawals.

- Every posting has an account, currency or instrument unit, signed decimal amount, event type, immutable idempotency key, origin, and effective and observed timestamps.
- Every transaction balances by currency and instrument unit. PostgreSQL `NUMERIC` values are authoritative; binary floating point is not.
- Raw venue events are stored before normalization. Ledger transactions reference those events and the related intent, order, fill, settlement, or capital flow.
- Market value and unrealized P&L are projections over immutable marks, not rewrites of historical cost.
- Deposits and withdrawals are capital-flow events. They do not reset time-weighted return history.
- Existing `orders`, `trades`, and `positions` remain compatibility read models until replay from the ledger matches them for 30 consecutive daily reconciliations.
- A duplicate or retried source event must produce no additional postings.

## Consequences

### Positive

- Cash and P&L become explainable and replayable across equities, options, crypto, and event contracts.
- Broker reconciliation and capital additions no longer require ad hoc corrections.
- Copy-trading activity can carry an explicit origin without contaminating strategy results.

### Negative

- The migration requires dual-writing, projection comparison, and carefully tested cutover logic.
- Venue-specific settlement, assignment, margin, and fee rules must be encoded explicitly.

### Neutral

- The ledger records economic truth; it does not choose strategies or guarantee profitability.
