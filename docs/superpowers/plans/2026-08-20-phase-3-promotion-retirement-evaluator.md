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

- [x] Add immutable policy identity, required-gate set, pass/failure action,
  bounded reason codes, and canonical restoration.
- [x] Add decision inputs bound to exact deployment and robustness parents.
- [x] Derive candidate membership, observed gates, outcome, prior/next state,
  and transition kind without caller-supplied verdicts.
- [x] Prove approved, held, retired, missing/duplicate gate, version/mode cross,
  partial family, tamper, clone, ordering, and stable-identity cases.
- [x] Commit and push the domain/calculator slice after focused races.

## Task 2: Migration 81 append-only lifecycle evidence

- [x] Add policy, required-gate, decision, observed-gate, and serialized
  deployment-lifecycle-event tables.
- [x] Independently reconstruct canonical bytes, IDs/hashes, exact parent
  hashes, candidate population, observed gates, policy result, and state chain.
- [x] Reject mutation/deletion, omission/reordering, forged results, stale
  state heads, forks, changed retries, and incomplete graphs.
- [x] Add no mutable current pointer, scheduler/runtime trigger, activation
  worker, provider writer, risk mutation, execution grant, or legacy backfill.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 81 after real
  PostgreSQL migration tests pass.
- [x] Commit and push the migration slice after focused database races.

## Task 3: Repository, service, recovery, and qualification

- [x] Reload exact deployment/assessment parents, register policy, evaluate,
  atomically append the decision/event, and reconstruct normalized rows.
- [x] List history only by explicit deployment, version, assessment, or family;
  expose a deterministic state projection but no best/current candidate query.
- [x] Prove eight-writer convergence, serialized competing writers, restart,
  every-child interruption rollback, changed retry conflict, and clean replay.
- [x] Retain approved, held, and retired local decisions with cross-mode and AI/
  UI/scheduler authority separation.
- [x] Add a runbook for policy review, gate inspection, transition projection,
  failure preservation, rollback, and explicit non-activation boundaries.
- [x] Apply fresh `1 -> 81`, retain a complete decision chain, prove nonempty
  rollback refusal, and separately rehearse empty `81 -> 80 -> 81`.
- [x] Run focused/database races, backend and pinned frontend gates, diff review,
  and isolated kill-switched schema-81 health/API/rollback/reapply smoke.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  milestone 4.

## Acceptance evidence to record

- [x] A deployment state change is reproducible from exact policy and immutable
  evidence links; no caller supplies the pass boolean or next state.
- [x] Failed, missing, partial-family, cross-version, and cross-mode evidence
  cannot approve a transition.
- [x] Decision history is append-only, serialized, restart-safe, and does not
  activate scheduling, allocation, deployment, or execution.
- [x] AI/UI/operator recommendations cannot bypass the deterministic evaluator.
- [x] Local qualification is `VERIFIED_LOCAL`; real strategy evidence,
  independent review, lifecycle cutover, scheduling, deployment, and production
  activation remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

- `VERIFIED_LOCAL`: retained isolated schema `augr_ovr306_qual_20260820`
  contains approved decision `42770ff3-e06e-d7f1-e581-f893903e655d`
  (`df6f0ae3e19d697111d74631cbab5f165ad7db16b8cbb31495f806cdc2aa7783`),
  held child `91f14fcc-c097-17b8-2ec7-2511cf9b4238`
  (`0a5b359f1e6a1dea61235d3f21dba02591f7988aade1860855809609be0320da`),
  and retired decision `514c4e55-c541-2de6-756b-fc33a05f68b4`
  (`169a57f34141a3674ca0e182941235b8e54644401979bcaaa9b0d87983790620`).
- The approved/held chain projects `proposed -> shadow -> shadow` for deployment
  `67013d70-d862-e412-1130-4697b970ad55`. The separate failed assessment
  `672ef184-0685-a71c-755e-a2d48187462f` deterministically projects deployment
  `27f6ac23-e51e-59b9-83a5-ba2b20c8b873` from `proposed -> retired`.
- Retained normalized counts are `2/3/3/5/3` policy/required-gate/decision/
  observed-gate/lifecycle-event rows. Eight-writer convergence, competing
  initial-head rejection, restart reload, normalized forgery rejection,
  every-child interruption rollback, append-only refusal, and nonempty
  migration refusal passed.
- Empty real-PostgreSQL rehearsal passed `80 -> 81 -> 80 -> 81`. The isolated
  production image passed fresh `1 -> 81`, schema verification, authenticated
  read-only API health, rollback to 60, backup/restore, reapply through 81, and
  restarted health with scheduler/live/provider authority absent.
- All Go package races without external database dependencies passed. Focused
  OVR-305/306 real-PostgreSQL races passed. Build, vet, golangci-lint, gofumpt,
  and govulncheck passed with zero reachable vulnerabilities.
- Pinned Node `22.23.2` frozen install, high-severity audit, 162 tests, lint,
  and production build passed. The audit retains one low-severity Windows-only
  esbuild development-server advisory.
- `BLOCKED_EXTERNAL`: real candidate data, independent statistical/lifecycle
  review, scheduler adoption, allocation, shadow/paper/live runtime activation,
  shared migration, deployment, and production cutover.
