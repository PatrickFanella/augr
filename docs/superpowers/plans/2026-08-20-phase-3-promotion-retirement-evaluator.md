# Phase 3 Promotion and Retirement Evaluator Plan

**Goal:** Complete OVR-306 with immutable deterministic lifecycle decisions
whose status transitions are computed from exact OVR-305 evidence and a
reviewed policy. No UI, AI workflow, scheduler, deployment process, or operator
can write a passing outcome directly.

**Architecture:** Add an `internal/promotion` bounded context and additive
migration 81. A content-addressed policy pins the deployment-state graph,
required OVR-305 candidate gates, pass and failure actions, decision reasons,
and evidence freshness semantics. An evaluation reloads one exact proposed
OVR-302 deployment, one exact completed OVR-305 family assessment, and the
candidate row matching the deployment's strategy version. It emits an
append-only decision plus a serialized deployment lifecycle event. PostgreSQL
independently validates parent hashes, candidate membership, prior-state
continuity, gate observations, policy result, and normalized reconstruction.
The new state is research governance evidence only: no scheduler or runtime
reads it in this milestone.

**Migration:** `000081_promotion_retirement_evaluator`

## Scope and non-goals

In scope:

- exact policy, deployment, robustness assessment, candidate, and gate pins;
- deterministic `proposed -> shadow` eligibility when every required gate
  passes under the complete OVR-305 search family;
- deterministic hold or retirement results under policy-pinned failures;
- serialized state continuity with one accepted decision per deployment head;
- explicit `approved`, `held`, and `retired` outcomes and bounded reasons;
- append-only normalized evidence, exact reload, retry/restart convergence,
  recovery injection, retained qualification, and empty-only rollback.

Out of scope:

- scheduling or starting a shadow/paper/live process;
- editing the immutable OVR-302 deployment assignment or strategy version;
- transitions beyond `shadow` without future OVR-702/703 evidence;
- capital allocation, risk-limit changes, live activation, provider calls,
  order submission, UI mutation, AI approval, or production cutover;
- treating synthetic local evidence as real promotion authority.

## Locked contracts

1. Every decision binds the exact deployment ID/SHA-256, version, account,
   capital binding, budget, mode, risk policy, and proposed state.
2. Every decision binds one completed OVR-305 assessment ID/SHA-256, family,
   policy, mode, exact candidate version, and normalized candidate gates.
3. The deployment version must be present exactly once in the assessment's
   declared complete family. Cross-version, cross-mode, partial-family, or
   missing-candidate evidence fails closed.
4. Required gate names and required states are policy identity. The evaluator
   recomputes its outcome from the candidate gates; callers cannot provide an
   outcome boolean or next state.
5. The reviewed local policy approves only `proposed -> shadow` when all
   required OVR-305 gates pass. A failure holds the deployment at `proposed`;
   it does not silently retire a candidate from one synthetic assessment.
6. Retirement is a separate policy version/action with explicit gate failure
   criteria and never deletes the deployment, strategy, assessment, reports,
   orders, positions, or ledger evidence.
7. Decisions serialize per deployment. The first decision's prior state is the
   immutable deployment's `proposed`; later decisions must name the exact prior
   decision and prior next state. Forks and stale writers fail closed.
8. Identical writers converge. A changed payload under an accepted identity,
   missing child, forged gate, reordered evidence, or normalized/canonical
   divergence fails at commit and reload.
9. There is no writable current-status column. Current state is a deterministic
   projection of the serialized decision chain, with exact history available.
10. No scheduler, deployment worker, API/UI control, provider role, runtime
    trigger, legacy backfill, or execution grant is added. Future cutover must
    explicitly adopt this projection after independent review.
11. AI may recommend or cite a decision but cannot create, alter, approve, or
    apply one. Only the deterministic evaluator owns result construction.
12. Migration 81 is additive, lock-first, and empty-only reversible.

## Task 1: Policy and deterministic decision domain

- [ ] Add immutable policy identity, required-gate set, pass/failure action,
  bounded reason codes, and canonical restoration.
- [ ] Add decision inputs bound to exact deployment and robustness parents.
- [ ] Derive candidate membership, observed gates, outcome, prior/next state,
  and transition kind without caller-supplied verdicts.
- [ ] Prove approved, held, retired, missing/duplicate gate, version/mode cross,
  partial family, tamper, clone, ordering, and stable-identity cases.
- [ ] Commit and push the domain/calculator slice after focused races.

## Task 2: Migration 81 append-only lifecycle evidence

- [ ] Add policy, required-gate, decision, observed-gate, and serialized
  deployment-lifecycle-event tables.
- [ ] Independently reconstruct canonical bytes, IDs/hashes, exact parent
  hashes, candidate population, observed gates, policy result, and state chain.
- [ ] Reject mutation/deletion, omission/reordering, forged results, stale
  state heads, forks, changed retries, and incomplete graphs.
- [ ] Add no mutable current pointer, scheduler/runtime trigger, activation
  worker, provider writer, risk mutation, execution grant, or legacy backfill.
- [ ] Add empty-only rollback and bump `RequiredSchemaVersion` to 81 after real
  PostgreSQL migration tests pass.
- [ ] Commit and push the migration slice after focused database races.

## Task 3: Repository, service, recovery, and qualification

- [ ] Reload exact deployment/assessment parents, register policy, evaluate,
  atomically append the decision/event, and reconstruct normalized rows.
- [ ] List history only by explicit deployment, version, assessment, or family;
  expose a deterministic state projection but no best/current candidate query.
- [ ] Prove eight-writer convergence, serialized competing writers, restart,
  every-child interruption rollback, changed retry conflict, and clean replay.
- [ ] Retain approved, held, and retired local decisions with cross-mode and AI/
  UI/scheduler authority separation.
- [ ] Add a runbook for policy review, gate inspection, transition projection,
  failure preservation, rollback, and explicit non-activation boundaries.
- [ ] Apply fresh `1 -> 81`, retain a complete decision chain, prove nonempty
  rollback refusal, and separately rehearse empty `81 -> 80 -> 81`.
- [ ] Run focused/database races, backend and pinned frontend gates, diff review,
  and isolated kill-switched schema-81 health/API/rollback/reapply smoke.
- [ ] Commit/push verified slices, fetch, and prove `0 0` divergence before
  milestone 4.

## Acceptance evidence to record

- [ ] A deployment state change is reproducible from exact policy and immutable
  evidence links; no caller supplies the pass boolean or next state.
- [ ] Failed, missing, partial-family, cross-version, and cross-mode evidence
  cannot approve a transition.
- [ ] Decision history is append-only, serialized, restart-safe, and does not
  activate scheduling, allocation, deployment, or execution.
- [ ] AI/UI/operator recommendations cannot bypass the deterministic evaluator.
- [ ] Local qualification is `VERIFIED_LOCAL`; real strategy evidence,
  independent review, lifecycle cutover, scheduling, deployment, and production
  activation remain `BLOCKED_EXTERNAL`.
