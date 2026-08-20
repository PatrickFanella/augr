# Milestone 6 Autonomous Daily Supervisor Plan

> **For agentic workers:** Execute test-first in coherent commits. Keep the
> supervisor an additive evidence and admission boundary; do not activate the
> legacy schedulers or mutate production risk state.

**Goal:** Complete OVR-605 with a deterministic daily supervisor that consumes
exact OVR-207 reconciliation and OVR-604 job/lease evidence, halts every path
that can add exposure when a required dependency is missing, stale, failed, or
drifting, and keeps only reviewed protective exits, settlements, and
reconciliation work eligible.

**Architecture:** Add `internal/dailysupervisor` and schema 99. A versioned
policy classifies closed dependency checks and closed job mutation classes into
`new_exposure`, `protective_exit`, `settlement`, `reconciliation`, and
`evidence_only`. One immutable assessment binds the exact operating day,
readiness inputs, latest complete reconciliation run/digest, risk/brake
snapshot, and OVR-604 catalog/occurrence evidence. It emits an immutable action
matrix and attention items. No AI output participates. New exposure requires
every mandatory check to pass; unknown is failure. Protective exits,
settlements, and reconciliation remain eligible only when their own narrower
safety dependencies pass. The supervisor never flattens automatically, clears
a brake, changes a risk limit, promotes a strategy, or creates an execution
intent. A later explicit cutover may make schedulers consume these assessments.

**Boundary:** Local synthetic PostgreSQL only. No shared migration, provider
call, scheduler activation, brake mutation, cancellation, flattening,
allocation, settlement, broker route, deployment, or live trade.

## Contract decisions

1. The operating day and all freshness cutoffs use a pinned timezone and
   policy version; DST ambiguity is resolved before identity construction.
2. Mandatory checks are database/schema, ledger/projection, market/reference
   data, risk/brake state, latest reconciliation, and leased scheduler health.
   Missing, stale, unknown, incomplete, or failed evidence is never pass.
3. Any mandatory failure sets `new_exposure=halted`. Reconciliation drift or
   incomplete local/provider facts also halt new exposure with exact incidents.
4. Protective exits are reduce-only and position-proven. They remain eligible
   through data/research/provider degradation only when database, schema,
   ledger, risk, and route safety are known. They never become a generic order
   bypass.
5. Settlement and reconciliation use separate gates and may remain eligible
   while new exposure is halted. A dependency required for either work class
   still fails that class closed.
6. The supervisor never automatically flattens or cancels protective exits.
   Attention actions are evidence, not commands.
7. One assessment is content-addressed. Same evidence converges; changed reuse
   conflicts. Supersession is append-only and strictly later for the same day.
8. OVR-604 effect keys bind the supervisor occurrence and any later action;
   two instances cannot create a second assessment or action identity.
9. Schema 99 starts empty, is empty-only reversible, and grants no scheduler,
   risk, execution, provider, or operator authority.

## Task 1: Policy, assessment, and action matrix

- [x] Implement closed checks/work classes, exact evidence references,
  deterministic action derivation, attention items, and daily supersession.
- [x] Prove all-pass, provider outage, stale/corrupt data, reconciliation drift,
  ledger/risk failure, settlement-only failure, unknown input, DST/day identity,
  permutation convergence, semantic conflict, and no automatic flatten path.
- [x] Bind exact OVR-207 run/digest and OVR-604 occurrence/effect evidence.
- [ ] Commit and push the domain slice after focused race tests.

## Task 2: Schema 99 and retained qualification

- [x] Persist immutable policy/assessment/check/reference/action/attention and
  supersession graphs with database reconstruction and authority checks.
- [x] Prove eight-writer convergence, changed retry conflict, every-stage
  rollback, restart, stale/fork rejection, append-only behavior, direct-SQL
  forgery rejection, nonempty rollback refusal, and empty `99 -> 98 -> 99`.
- [x] Retain an all-pass day and a dependency-failure day proving new exposure
  halts while safe exits/reconciliation/settlement remain independently gated.
- [ ] Commit and push the persistence slice after focused/database races.

## Task 3: Closure

- [ ] Add inspection/recovery/rollback runbook evidence with exact IDs/digests
  and explicit activation/authority limits.
- [ ] Run repository-wide backend/static and pinned frontend gates, diff review,
  and isolated kill-switched schema-99 health/API/rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before OVR-606.

## Acceptance evidence to record

- [ ] Every dependency failure deterministically halts new exposure.
- [ ] Protective exits, settlements, and reconciliation use narrower explicit
  gates and cannot be disabled merely because new exposure is halted.
- [ ] No supervisor method or schema path can flatten, clear brakes, alter risk,
  promote, allocate, construct intents/orders, settle, or call providers.
- [ ] Local qualification is `VERIFIED_LOCAL`; cutover and all external or
  financial mutations remain `BLOCKED_EXTERNAL`.
