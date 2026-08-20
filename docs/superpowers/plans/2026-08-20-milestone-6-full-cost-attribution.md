# Milestone 6 Full Cost Attribution Plan

> **For agentic workers:** Execute test-first in coherent commits. Cost evidence
> is additive reporting only; it must not mutate ledger economics, promotion,
> deployment, scheduling, risk, or execution state.

**Goal:** Complete OVR-606 with deterministic, immutable cost statements for
one exact OVR-603 evidence-review case/summary. Every model, data, fee, rebate,
and infrastructure line is explicitly `actual`, `estimated`, or `unknown`, and
known net cost is reconstructed without treating missing facts as zero.

**Architecture:** Add `internal/costattribution` and schema 100. A report binds
the exact evidence-review case/summary, its OVR-301 manifest and hypothesis,
an account, reporting window, currency, and a closed ordered cost-line set.
Actual model cost is checked against retained model provenance. Actual fees and
rebates bind immutable OVR-102 ledger transactions and exact economic posting
amounts. Data and infrastructure estimates require content-addressed evidence,
a named method, and an explanation. Unknown lines have no amount and require a
reason. Totals retain actual cost, estimated cost, actual rebates, estimated
rebates, known net cost, and unknown count separately. A report is evidence for
review/operator consumers only; it cannot change promotion authority.

**Boundary:** Local synthetic PostgreSQL only. No invoice/provider fetch,
shared migration, cost-center mutation, ledger write by the attribution
repository, promotion/deployment change, scheduler activation, allocation,
settlement, broker route, deployment, or live trade.

## Contract decisions

1. Categories are exactly `model`, `data`, `fee`, `rebate`, and
   `infrastructure`; every report contains at least one line for each.
2. Status is exactly `actual`, `estimated`, or `unknown`. Actual and estimated
   lines carry a canonical nonnegative amount. Unknown lines carry no amount
   and can never contribute zero to totals.
3. Estimated lines require an evidence ID/digest, method key, method digest,
   and explanation. Actual lines require retained evidence. Unknown lines
   require a reason and have no invented evidence or method.
4. Actual model cost must equal the bound hypothesis model-provenance cost and
   currency. Actual fees/rebates must equal the relevant immutable ledger
   posting magnitude for the bound account and reporting window.
5. Fee/rebate signs are semantic, never caller-controlled: fees add and rebates
   subtract. Known net cost is actual plus estimated costs, less actual plus
   estimated rebates. Negative known net cost is allowed and remains a rebate.
6. Multiple currencies are never silently converted. Every known line matches
   the report currency; future conversion requires a separate exact FX artifact.
7. One report is content-addressed. Identical writers converge; reuse of the
   same review summary with changed content conflicts. Persistence is append-
   only and reconstructs all line/totals/completeness facts.
8. Cost attribution does not retroactively turn an OVR-603 unknown review check
   into pass and has no promotion/lifecycle mutation surface.
9. Schema 100 is empty-only reversible and grants no external or financial
   authority.

## Task 1: Cost-line and report domain

- [x] Implement closed categories/statuses, evidence/method contracts, exact
  decimals, deterministic ordering/identity, totals, and completeness state.
- [x] Prove actual/estimated/unknown handling, missing-category rejection,
  unknown-is-not-zero, fee/rebate signs, currency isolation, permutation
  convergence, semantic change, and no promotion/ledger mutation surface.
- [x] Bind exact OVR-603 case/summary, OVR-301 manifest/hypothesis, account, and
  reporting window evidence.
- [ ] Commit and push the domain slice after focused race tests.

## Task 2: Schema 100 and retained qualification

- [ ] Persist immutable reports, lines, evidence, methods, totals, and
  completeness with database reconstruction and exact parent checks.
- [ ] Verify model provenance and ledger fee/rebate actuals in PostgreSQL;
  reject forged amounts, parents, categories, statuses, and totals.
- [ ] Prove eight-writer convergence, changed retry conflict, every-stage
  rollback, restart, append-only behavior, direct-SQL forgery rejection,
  nonempty rollback refusal, and empty `100 -> 99 -> 100`.
- [ ] Retain one incomplete report with explicit infrastructure unknown and one
  complete-with-estimates report; preserve exact actual/estimated subtotals.
- [ ] Commit and push persistence after focused/database race tests.

## Task 3: Closure

- [ ] Add inspection/recovery/rollback runbook evidence with exact IDs/digests
  and explicit authority/external-source limits.
- [ ] Run repository-wide backend/static and pinned frontend gates, diff review,
  and isolated kill-switched schema-100 health/API/rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-607.

## Acceptance evidence to record

- [ ] Every required category is actual, estimated, or explicitly unknown.
- [ ] Actual model and ledger economics are independently reconstructed;
  estimated amounts retain methodology; unknown never becomes zero.
- [ ] Cost evidence cannot mutate promotion, ledger, deployment, scheduler,
  risk, allocation, settlement, provider, intent, order, or trade state.
- [ ] Local qualification is `VERIFIED_LOCAL`; external acquisition, shared
  migration, cutover, deployment, and financial mutations are `BLOCKED_EXTERNAL`.
