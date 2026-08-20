# Phase 3 Strategy Version, Experiment, and Deployment Model Plan

**Goal:** Complete OVR-302 with immutable strategy families and versions,
reproducible experiment declarations that pin OVR-301 evidence, and inert
deployment assignments so editing configuration always creates a new version
without changing a legacy or historical record.

**Architecture:** Add an `internal/strategycatalog` bounded context and additive
migration 77. A family is a durable immutable thesis. A version is a
content-addressed family child that pins canonical configuration, source and
compiler identities, deterministic decision contract, and required dataset
kinds. An experiment declaration pins one exact version, manifest, quality
result, simulation policy, capital policy, evaluation window, mode, and seed;
it does not execute. A deployment assignment pins one version to an account,
capital binding, budget, schedule, timezone, and risk-policy reference, but is
created only as `proposed` and has no activation transition in this slice.
Legacy mutable strategies may be explicitly mapped to a family only as
`legacy_unvalidated`. PostgreSQL independently enforces every identity, parent
scope, immutable graph, and no-cutover boundary.

**Migration:** `000077_strategy_catalog_experiments`

## Scope and non-goals

In scope:

- immutable family identity for one durable thesis;
- immutable, content-addressed strategy versions;
- canonical code/config/compiler/decision/data-contract identity;
- experiment declarations that pin exact dataset manifest and quality-result
  IDs plus simulation and capital policy versions;
- scored-versus-stress admission, including fail-closed quarantine handling;
- proposed deployment assignments to explicit accounts and exact capital
  bindings with budgets, schedules, timezone, and risk-policy references;
- append-only registration/declaration/proposal lifecycle evidence;
- explicit `legacy_unvalidated` mapping for old mutable `strategies` rows;
- exact persistence, restart replay, concurrency, interruption, and empty-only
  rollback.

Out of scope:

- running an experiment, generating signals/intents/fills, or computing metrics
  (OVR-303/304);
- walk-forward, bootstrap, perturbation, or search-adjustment gates (OVR-305);
- approving, activating, pausing, promoting, or retiring a version/deployment
  (OVR-306);
- a generative strategy compiler (OVR-601);
- changing legacy strategy API/UI/scheduler behavior or mutable tables;
- automatically treating a legacy strategy/backtest as a version or experiment;
- inventing missing source commits, datasets, quality results, policies, risk
  evidence, or provenance;
- shared/production migration, provider calls, live trading, scheduler
  activation, deployment, or read/write cutover.

## Locked contracts

1. A family uses a stable normalized slug and deterministic UUID. Its exact
   name, thesis, and declared asset classes are immutable after registration.
2. A version identity covers family ID, compiler kind/version, exact source
   commit and source-tree SHA-256, config schema and canonical JSON bytes,
   decision-contract version, and sorted unique required dataset kinds.
3. Configuration must be one canonical JSON object. Equivalent reordered or
   whitespace-varied JSON is rejected at the boundary; a caller must canonicalize
   before registration. Any accepted config change produces a distinct version
   ID and digest. No version update API or SQL mutation exists.
4. A version may have many experiments and deployments. Neither owns or mutates
   its family/version parent.
5. Every new experiment pins exactly one version, OVR-301 manifest, matching
   quality result, simulation policy version, capital policy version, UTC
   microsecond evaluation window, deterministic seed, and mode.
6. `paper_scored` experiment declarations require a nonquarantined quality
   result and `promotion_evidence` capital policy. `paper_stress` declarations
   require `synthetic_stress`; they may retain quarantined datasets but must
   record that fact in their canonical identity. Missing or mismatched evidence
   is rejected, never inferred.
7. Experiment state in OVR-302 is only `declared`. OVR-303 owns execution-state
   events and results; declaration cannot imply a completed or reproducible run.
8. A deployment assignment pins an exact version to one account and its exact
   immutable capital-policy binding, canonical positive budget, cron text,
   timezone, and nonempty risk-policy version.
9. Deployment state in OVR-302 is only `proposed`, with activation authority
   reserved for the future promotion evaluator. No API, repository method,
   migration, trigger, or current pointer can activate it.
10. A legacy strategy mapping captures the legacy strategy ID, family ID, and
    exact mutable-row snapshot digest as `legacy_unvalidated`. It does not create
    a version, experiment, approval, or deployment and is never automatic.
11. Parent rows and lifecycle evidence are append-only. Identical retries
    converge; changed payload under the same stable identity returns
    `ErrIdempotencyConflict`; concurrent writers leave one complete graph.
12. Migration 77 locks source/evidence tables first, adds no writer grant or
    runtime pointer, refuses nonempty rollback, and never changes or backfills a
    legacy strategy/backtest row.

## Task 1: Strategy family and immutable version domain

- [x] Add `internal/strategycatalog/family.go` with bounded slug/name/thesis,
  asset-class vocabulary, deterministic identity, canonical bytes, restore,
  validation, and clone-safe getters.
- [x] Add `internal/strategycatalog/version.go` with compiler/source/config/
  decision/data-contract identity, deterministic ordering, exact hashes,
  canonical bytes, restore, and validation.
- [x] Prove reordered inputs converge, noncanonical config is rejected, config
  editing creates a new identity, source/compiler/data-contract changes create
  new identities, and tampering cannot restore.
- [x] Commit and push the family/version slice after focused race tests.

## Task 2: Experiment declarations and fail-closed admission

- [x] Add `internal/strategycatalog/experiment.go` with scored/stress modes,
  exact version/manifest/quality/simulation/capital pins, evaluation window,
  seed, dataset-quarantine fact, canonical declaration state, and identity.
- [x] Keep cross-aggregate admission in the repository: quality must match the
  manifest; dataset kinds must satisfy the version contract; exact simulation
  and capital policies must exist; scored/stress evidence class must match.
- [x] Reject scored quarantine, unknown/missing/mismatched evidence, non-UTC or
  sub-microsecond windows, invalid ranges, mutable canonical state, and implicit
  legacy inputs.
- [x] Commit and push the experiment slice after focused race tests.

## Task 3: Proposed deployment and legacy mapping domain

- [x] Add `internal/strategycatalog/deployment.go` for exact version/account/
  capital-binding/budget/schedule/timezone/risk-policy assignments. Lock state to
  `proposed` and activation authority to the future promotion evaluator.
- [x] Add `internal/strategycatalog/legacy.go` for explicit
  `legacy_unvalidated` snapshot mappings only; require an exact legacy-row
  SHA-256 and expose no validation/promotion conversion.
- [x] Add deterministic registration/declaration/proposal lifecycle evidence
  without general transition authority.
- [x] Prove assignment changes create new identities and no domain constructor
  can claim active/approved/completed state.
- [x] Commit and push the deployment/legacy slice after focused race tests.

## Task 4: Migration 77 append-only catalog graph

- [x] Add family, version, version-kind, experiment, deployment, legacy-mapping,
  and lifecycle-evidence tables with exact foreign keys and repeated parent
  scope where needed.
- [x] Reconstruct canonical bytes, counts, sorted children, hashes, deterministic
  IDs, family/version scope, experiment evidence matches, capital environment/
  evidence class, deployment account/binding/budget, and locked initial states
  in deferred constraints.
- [x] Reject update/delete, direct-SQL forged scope/identity/state, quarantined
  scored experiments, missing required dataset kinds, and active deployments.
- [x] Add indexes for family/version lineage, experiment evidence, account
  deployment proposals, and explicit legacy mapping lookup. Add no current
  pointer, writer grant, scheduler, or legacy backfill.
- [x] Add lock-first empty-only rollback and bump `RequiredSchemaVersion` to 77
  only after isolated migration tests pass.
- [ ] Commit and push the migration slice after focused real-PostgreSQL races.

## Task 5: PostgreSQL repository and restart replay

- [x] Add `internal/repository/postgres/strategy_catalog.go` and a narrow
  repository interface for atomic registration/declaration/proposal/mapping and
  exact reload.
- [x] Validate every cross-aggregate pin inside one transaction. Repository
  reload must reconstruct canonical parents and relational children rather than
  trust stored JSON alone.
- [x] Prove identical and eight-writer retries converge, stable-family changed
  payload conflicts, injected stage failures roll back fully, and restart reload
  reproduces every ID/digest.
- [x] Retain a golden family with two config-derived versions, one admitted
  scored experiment, one explicit stress declaration, one proposed deployment,
  and one separately mapped `legacy_unvalidated` strategy.
- [x] Commit and push the repository slice after focused database races.

## Task 6: Documentation, qualification, review, and synchronization

- [x] Add a runbook covering version creation, exact experiment/deployment
  inspection, scored/stress boundaries, legacy mapping, preservation, no-
  activation response, and empty-only rollback.
- [x] Apply migrations `1 -> 77` to a fresh loopback database and retain the
  golden catalog. Prove config edit yields a second version, exact reload,
  nonempty rollback refusal, and separate empty `77 -> 76 -> 77`.
- [ ] Run focused and repository-wide backend races/build/vet/lint/format/
  vulnerability gates, pinned Node 22 frozen install/audit/tests/lint/build,
  and `git diff --check`.
- [ ] Run one isolated kill-switched schema-77 health smoke with live trading
  and all schedulers false, no credentials, evidence counts unchanged, and zero
  in-flight shutdown.
- [ ] Complete a final diff review with no unresolved P0/P1, commit verified
  slices, push/fetch, and prove `0 0` divergence before OVR-303.

## Acceptance evidence to record after implementation

- [x] Editing accepted configuration creates a new immutable version while the
  old family/version/experiment/deployment evidence remains unchanged.
- [x] Every new experiment declaration pins matching exact OVR-301 manifest and
  quality evidence plus simulation/capital policy versions.
- [x] Scored quarantine or evidence-class mismatch fails closed; stress evidence
  remains synthetic and cannot become promotion evidence.
- [x] Deployments remain inert proposals with explicit account, budget, capital,
  schedule, timezone, and risk-policy identity; no activation path exists.
- [x] Legacy mappings remain explicitly unvalidated and create no version or
  experiment automatically.
- [x] Migration 77 is additive, append-only, empty-only reversible, and leaves
  legacy runtime behavior and data untouched.
- [ ] Local qualification is labeled `VERIFIED_LOCAL`; shared migration,
  production data, experiment execution, promotion, activation, and cutover
  remain `BLOCKED_EXTERNAL`.
