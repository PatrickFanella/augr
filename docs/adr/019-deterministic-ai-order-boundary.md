---
title: "ADR-019: Deterministic boundary between AI research and orders"
description: "AI may propose evidence-bearing ideas but cannot directly activate strategies, size risk, or submit orders."
status: "accepted"
updated: "2026-08-14"
tags: [adr, ai, risk, strategies, automation]
---

# ADR-019: Deterministic boundary between AI research and orders

- **Status:** accepted
- **Date:** 2026-08-14
- **Deciders:** Project owner, Engineering
- **Technical Story:** [Augr total overhaul plan](../superpowers/plans/2026-08-14-total-overhaul-plan.md)
- **Supersedes:** [ADR-009: Human review gate before live trading](009-human-review-gate.md)

## Context

The audit found that generated strategies could create themselves as active scheduled jobs. This turns model output into an execution permission and creates a growing fleet of correlated, weakly evidenced strategies. The project should run unattended in paper mode, but autonomy cannot mean that probabilistic text directly controls capital or risk limits.

## Decision

Place a deterministic, typed boundary between generative research and execution.

- AI and generative workflows may summarize evidence, propose hypotheses, draft rule configurations, and explain decisions.
- Generated strategies are persisted only as paper, inactive, unscheduled `idea` artifacts with `auto_activation_blocked=true`.
- AI output cannot activate a strategy, select an account, change risk limits, classify its own evidence as valid, set a promotion result, or submit/cancel an order.
- Schema validation, point-in-time data checks, reproducible backtests, passive controls, multiple-testing corrections, capacity checks, risk policy, and promotion gates are deterministic code with versioned inputs.
- Paper promotion may be unattended when every deterministic gate passes. Human review is not required for routine scored-paper operation.
- The emergency brake remains independent of model and scheduler health and permits only execution-manager-verified reduce-only exits.
- Live trading stays disabled during the overhaul. Any future live activation policy—including whether operation can be unattended—requires a separate ADR after scored paper and shadow evidence exist.

## Consequences

### Positive

- Generative workflows remain useful without granting them execution authority.
- New model providers or prompts cannot bypass risk and evidence contracts.
- Paper research can run unattended while remaining reproducible and fail-closed.

### Negative

- Fewer generated ideas will become active, and many legacy strategies will be quarantined.
- More typed validation and provenance are required between research and execution.

### Neutral

- Deterministic gates reduce false confidence; they do not guarantee that a promoted strategy will remain profitable.
