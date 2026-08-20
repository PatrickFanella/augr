# Milestone 6 Daily Operator Brief and Incident Inbox Plan

> **For agentic workers:** Execute test-first in coherent commits. The brief is
> immutable operator evidence only; it cannot acknowledge incidents, notify,
> schedule, trade, alter risk, or change review/promotion state.

**Goal:** Complete OVR-607 with exactly one deterministic brief per operating
day/timezone that explains performance, decisions, reconciliation drift, risk,
costs, supervisor admissions, and required attention from exact retained
evidence. Its incident inbox preserves unresolved facts without inventing
resolution or silently dropping unknowns.

**Architecture:** Add `internal/operatorbrief` and schema 101. A brief binds one
exact OVR-605 assessment plus its OVR-207 reconciliation, one exact OVR-606 cost
report plus its OVR-603 review summary, and one exact OVR-304 portfolio
evaluation. Five closed sections (`performance`, `decisions`, `drift`, `risk`,
`costs`) each retain a status, headline, explanation, exact evidence ID/digest,
and deterministic facts. Incidents are derived from supervisor attention,
halted work classes, unknown cost lines, incomplete cost coverage, and
unavailable/negative performance. They are open evidence records, not mutable
tickets. No AI-generated interpretation is authoritative.

**Boundary:** Local synthetic PostgreSQL only. No email/chat/pager delivery,
acknowledgement, incident closure, provider fetch, shared migration, brake/risk
mutation, automatic flatten/cancellation, review/promotion change, scheduling,
allocation, settlement, broker route, deployment, or live trade.

## Contract decisions

1. Operating day uses one pinned timezone and must match both generation time
   and the bound supervisor day. DST is resolved before identity construction.
2. Every brief contains exactly five ordered sections. Missing evidence is an
   `unavailable`/`incomplete` status and incident, never omitted prose.
3. Section facts are closed, canonical key/value evidence. Explanations are
   retained text but cannot change statuses, admissions, totals, review
   authority, or incident derivation.
4. Performance binds one exact OVR-304 evaluation. Decisions bind the exact
   OVR-603 summary already referenced by the OVR-606 report. Drift binds the
   exact OVR-207 run from OVR-605. Risk binds the exact OVR-605 assessment.
   Costs bind the exact OVR-606 report.
5. Every non-pass supervisor check becomes an incident. Every halted work class
   becomes an incident. Every unknown cost line becomes an incident. Duplicate
   facts converge under stable keys; none is auto-resolved.
6. The brief reports eligibility and authoritative decision facts but never
   invokes them. It has no callback for a brake, order, promotion, schedule,
   notification, acknowledgement, or incident state change.
7. One brief is content-addressed and unique per operating day/timezone.
   Identical writers converge; changed reuse conflicts and requires explicit
   new upstream daily evidence, not an in-place edit.
8. Schema 101 is append-only, empty-only reversible, and grants no operational
   authority.

## Task 1: Brief, sections, and incidents

- [x] Implement closed sections/statuses, canonical facts and explanations,
  exact evidence bindings, deterministic ordering/identity, and incident
  derivation.
- [x] Prove healthy day, halted exposure, reconciliation drift, risk failure,
  incomplete costs, unavailable performance, DST/day checks, permutation
  convergence, semantic change, and no mutation/notification surface.
- [x] Bind exact OVR-304 evaluation, OVR-603 summary, OVR-207 reconciliation,
  OVR-605 assessment, and OVR-606 cost report evidence.
- [ ] Commit and push the domain slice after focused race tests.

## Task 2: Schema 101 and retained qualification

- [x] Persist immutable briefs, sections, facts, incidents, blockers, and exact
  parent references with database reconstruction.
- [x] Prove eight-writer convergence, changed daily conflict, every-stage
  rollback, restart, append-only behavior, direct-SQL forgery rejection,
  nonempty rollback refusal, and empty `101 -> 100 -> 101`.
- [x] Retain one baseline brief that honestly surfaces unavailable performance
  and one attention brief showing halted exposure, unknown infrastructure cost,
  and independent safe-work eligibility.
- [ ] Commit and push persistence after focused/database race tests.

## Task 3: Closure

- [ ] Add inspection/recovery/rollback runbook evidence with exact IDs/digests
  and explicit notification/acknowledgement/authority limits.
- [ ] Run repository-wide backend/static and pinned frontend gates, diff review,
  and isolated kill-switched schema-101 health/API/rollback/backup/restore/reapply.
- [ ] Commit/push verified slices, fetch, and prove `0 0` before Milestone 7.

## Acceptance evidence to record

- [ ] One brief explains performance, decisions, drift, risk, costs, admissions,
  and required attention from exact retained evidence.
- [ ] Unknown, unavailable, failed, drifting, and halted facts remain visible as
  deterministic open incidents and are never automatically acknowledged.
- [ ] The brief/inbox cannot notify, mutate incidents, flatten/cancel, clear
  brakes, alter risk, review/promote, schedule, allocate, settle, or trade.
- [ ] Local qualification is `VERIFIED_LOCAL`; delivery, shared migration,
  cutover, deployment, and all operational mutations are `BLOCKED_EXTERNAL`.
