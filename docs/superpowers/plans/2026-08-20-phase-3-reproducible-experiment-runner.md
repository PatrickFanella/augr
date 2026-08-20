# Phase 3 Reproducible Experiment Runner Plan

**Goal:** Complete OVR-303 with one deterministic, restart-safe experiment
runner whose clean replay of an exact OVR-302 declaration reproduces decision,
intent, order, fill, basic execution-metric, outcome, and aggregate hashes while
using OVR-301 point-in-time evidence, OVR-204's common simulation venue, and the
exact OVR-206 account/capital policy.

**Architecture:** Add an `internal/experimentrun` bounded context and additive
migration 78. A registered program adapter is an explicit deterministic
implementation for one exact strategy version and decision-contract identity;
OVR-303 does not compile arbitrary source/configuration. The runner loads one
declared experiment and all pinned artifacts, reconstructs the exact manifest,
asks the matching adapter for an ordered canonical replay plan, validates every
step against manifest observation/source hashes and the declared account/mode/
policy/window, creates deterministic OVR-203 intent/order identities, evaluates
through the unmodified OVR-204 venue, persists raw-first fills through the
OVR-203/103 repository, and derives an immutable result from reloaded evidence.
Separate append-only attempt events describe started/completed/failed execution;
they do not alter the content-addressed result. Identical retries or a clean
database replay converge to the same result and economic graph.

**Migration:** `000078_reproducible_experiment_runs`

## Scope and non-goals

In scope:

- exact program-adapter identity bound to one immutable strategy version;
- canonical ordered replay plans derived only from the pinned manifest;
- explicit observation-to-decision and decision-to-intent lineage;
- deterministic intent, order, simulation fill, normalization, ledger, and
  terminal-outcome replay through existing common boundaries;
- append-only attempt lifecycle with deterministic completed or failed result
  evidence;
- basic execution metrics required to prove replay equality: step, decision,
  intent, order, transition, fill, rejected/no-op counts, exact filled quantity,
  exact fee total, and ordered aggregate/outcome hashes;
- interruption recovery, identical retry, clean rerun, scored/stress isolation,
  and failure evidence without partial completed results;
- exact persistence/reload, migration rollback, retained golden qualification,
  documentation, and local gates.

Out of scope:

- trade-level returns, realized/unrealized P&L, portfolio statistics, bar-return
  win rate, benchmark-relative performance, or report scoring (OVR-304);
- walk-forward folds, bootstrap, perturbation, search correction, or robustness
  approval (OVR-305);
- promotion, retirement, deployment activation, or status authority (OVR-306);
- production strategy implementations and benchmark/candidate programs
  (OVR-401 through OVR-405);
- compiling arbitrary Go, generated code, prompts, or mutable legacy strategy
  configuration (OVR-601);
- leased distributed scheduling, automation, or retries across workers
  (OVR-604);
- provider fetching, mutable cache reads, shared migration, scheduler
  activation, deployment, live trading, or legacy backtest cutover.

## Locked contracts

1. The runner accepts only an OVR-302 experiment in `declared` state and a
   program whose version ID and decision-contract identity exactly match the
   persisted strategy version. Missing or ambiguous adapters fail closed.
2. Program input is the exact reconstructed OVR-301 manifest within the declared
   UTC evaluation window. The adapter cannot query wall-clock time, providers,
   mutable caches, or undeclared data through the runner interface.
3. A replay plan is canonical, bounded, strictly ordered, and content-addressed.
   Each step pins one manifest partition content hash and observation source
   key/content hash/availability time. Reordering or changing any step changes
   the plan identity.
4. Every executable decision pins its deterministic decision bytes and produces
   zero or one canonical execution intent. No-op/rejection is explicit; absence
   is never silently treated as success.
5. Intent/order IDs and idempotency keys derive from experiment, plan, step, and
   decision identity. A retry never invents a replacement economic identity.
6. Every request must match the declared account, scored/stress environment,
   exact simulation policy, canonical instrument and effective venue contract,
   decision/route snapshot, and capital binding. Budget/capital admission is
   rechecked before the first write.
7. Simulation uses OVR-204 unchanged. Raw fill evidence is persisted before the
   atomic OVR-103/203 normalization, ledger, fill, binding, and lifecycle graph.
8. The runner reloads persisted lifecycle/economic evidence before deriving the
   result. It never trusts only in-memory simulator output.
9. A result identity covers experiment, version, manifest, quality, program,
   runner, plan, ordered step outcomes, basic execution metrics, and aggregate/
   outcome hashes. Wall-clock attempt timestamps are excluded.
10. Attempts use append-only events. A failed/interrupted attempt can coexist
    with a later completed retry but cannot create, overwrite, or claim a
    completed result. One attempt has one terminal state.
11. Scored and stress runs remain distinct through experiment mode, account
    namespace/evidence class, result mode, and every lifecycle/economic parent.
    Stress results cannot satisfy scored or promotion evidence.
12. Parent, child, attempt, and result rows are append-only. Identical writers
    converge; mismatched reuse conflicts; deferred constraints reject incomplete
    graphs. Migration 78 adds no current pointer, grant, scheduler, or backfill.

## Task 1: Canonical program, plan, and result domain

- [x] Add explicit program identity and a narrow deterministic `Program`
  interface bound to one exact OVR-302 version and decision contract.
- [x] Add canonical replay-plan/step/decision/intent specifications with strict
  manifest observation references, ordering, bounds, exact decimals/times, and
  content-addressed identities.
- [x] Add immutable attempt-event, step-outcome, execution-metric, and run-result
  domains with restore validation and clone-safe accessors.
- [x] Prove input reorder convergence where order is semantic-free, semantic
  reordering divergence where sequence matters, tamper rejection, scored/stress
  separation, and exact deterministic hashes.
- [x] Commit and push the pure domain slice after focused race tests.

## Task 2: Deterministic runner and fail-closed orchestration

- [x] Load and validate the exact experiment/version/manifest/quality/simulation/
  capital/account/reference graph before executing a program.
- [x] Validate every plan step against pinned manifest evidence and the declared
  window; reject undeclared source hashes, future availability, duplicates,
  invalid order, missing instrument/venue mechanics, and nondeterministic
  program identity.
- [x] Build deterministic OVR-203 intent/order aggregates and evaluate every
  executable step through the existing OVR-204 common simulation venue.
- [x] Persist raw-first fills and all transitions through existing repositories,
  reload completed lifecycles, and derive step/outcome hashes only from reloaded
  evidence.
- [x] Emit deterministic completed result or explicit failed attempt evidence;
  context cancellation and injected failures never create a partial completed
  result.
- [x] Commit and push the runner slice after golden and negative-path races.

## Task 3: Migration 78 append-only run evidence

- [ ] Add program registrations, replay plans/steps, run attempts/events, step
  outcomes, results, ordered aggregate/outcome hashes, and exact metric tables.
- [ ] Reconstruct canonical bytes, deterministic IDs/hashes, counts, order,
  parent scope, terminal states, mode/account namespace, and result completeness
  in deferred PostgreSQL constraints.
- [ ] Reject mutation/deletion, forged completion, mixed scored/stress evidence,
  child omission/reordering, mismatched lifecycle IDs, and changed retries.
- [ ] Add no current/best result pointer, scheduler, lease, writer grant,
  promotion field, legacy backfill, or runtime trigger.
- [ ] Add lock-first empty-only rollback and bump `RequiredSchemaVersion` to 78
  only after isolated real-PostgreSQL tests pass.
- [ ] Commit and push the migration slice after focused database races.

## Task 4: PostgreSQL repository and recovery

- [ ] Add a narrow repository for atomic program/plan registration, attempt
  events, completed/failed result graphs, exact reload, and experiment result
  listing without best/current selection.
- [ ] Revalidate every cross-aggregate pin in one transaction and reconstruct
  normalized children rather than trusting canonical JSON alone.
- [ ] Prove identical/eight-writer convergence, changed retry conflict, injected
  rollback at every child stage, interruption then retry, and restart reload.
- [ ] Prove clean rerun in a fresh database reproduces plan, decision, intent,
  order, transition, fill, metric, outcome, and final-result hashes.
- [ ] Commit and push the repository/recovery slice after database races.

## Task 5: Golden runner qualification

- [ ] Add one explicit fixture program adapter for qualification only; bind it
  to the retained OVR-302 golden version without exposing a general compiler.
- [ ] Run scored and stress golden plans through common simulation and lifecycle
  persistence; prove mode isolation and exact within-mode replay.
- [ ] Cover no-op, rejection, stale/missing manifest evidence, partial fill,
  multiple fills, fee, latency, cancellation, failed persistence, cancellation,
  and restart paths without invented economic effects.
- [ ] Retain two clean runs/retries and one injected failed attempt with complete
  lineage and reproducibility evidence.
- [ ] Commit and push the golden runner slice after focused races.

## Task 6: Documentation, qualification, review, and synchronization

- [ ] Add a runbook covering adapter registration, execution preflight, attempt/
  result inspection, replay comparison, scored/stress handling, interruption,
  preservation, no-promotion response, and empty-only rollback.
- [ ] Apply migrations `1 -> 78` to fresh loopback databases, retain a complete
  golden result, prove clean-database reproduction, nonempty rollback refusal,
  and separate empty `78 -> 77 -> 78`.
- [ ] Run focused/database races, standard backend build/race/vet/lint/format/
  vulnerability gates, pinned Node 22 frozen install/audit/tests/lint/build,
  `git diff --check`, and an isolated kill-switched schema-78 health smoke.
- [ ] Complete final diff review with no unresolved P0/P1, commit verified
  slices, push/fetch, and prove `0 0` divergence before OVR-304.

## Acceptance evidence to record after implementation

- [ ] A clean rerun reproduces every plan, decision, intent, order, transition,
  fill, metric, outcome, aggregate, and final-result identity/hash.
- [ ] Every run consumes only exact declaration-pinned dataset/policy/account/
  capital/reference evidence and fails closed on mismatch or future data.
- [ ] Raw-first economic persistence and restart recovery preserve the existing
  OVR-103/203/204 invariants without replacement orders or duplicate effects.
- [ ] Scored/stress run evidence stays physically and semantically isolated;
  stress output cannot become promotion evidence.
- [ ] Failed/interrupted attempts remain explicit and cannot masquerade as a
  complete result; retries append attempt evidence and converge economically.
- [ ] Migration 78 is additive, append-only, empty-only reversible, and leaves
  legacy backtests, schedulers, deployments, and live execution untouched.
- [ ] Local qualification is `VERIFIED_LOCAL`; real strategy adapters, licensed
  data, protected runner infrastructure, promotion, deployment, and production
  cutover remain `BLOCKED_EXTERNAL`.
