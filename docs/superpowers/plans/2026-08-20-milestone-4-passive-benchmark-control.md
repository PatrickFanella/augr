# Milestone 4 Passive Benchmark Control Plan

**Goal:** Complete OVR-401 by making the passive benchmark and cash control
explicit, immutable, and reproducible for every evaluation admitted to the new
benchmark-control boundary. The result reports opportunity cost without
changing any OVR-302 experiment or OVR-304 evaluation identity.

**Architecture:** Add an `internal/benchmark` bounded context and additive
migration 82. A content-addressed declaration binds one exact experiment to a
benchmark instrument, dataset manifest, return/cash conventions, observation
frequency, and ordered point-in-time benchmark evidence. A deterministic
service reloads the exact experiment and completed OVR-304 evaluation, verifies
that its benchmark curve and cash returns exactly match the declaration, and
emits an immutable opportunity-cost report. PostgreSQL independently validates
the complete graph and reconstructs canonical evidence. This is research
control evidence only; it cannot select, promote, schedule, allocate, or trade.

**Migration:** `000082_passive_benchmark_control`

## Scope and non-goals

In scope:

- immutable experiment-scoped benchmark identity and exact source evidence;
- passive buy-and-hold, total-return-index, and explicit per-period cash
  conventions with no hidden benchmark substitutions;
- exact timestamp/value/cash alignment with an OVR-304 evaluation;
- deterministic strategy, benchmark, and cash total returns plus opportunity
  costs and terminal wealth differences;
- append-only normalized persistence, exact reload, concurrency convergence,
  recovery injection, retained qualification, and empty-only rollback.

Out of scope:

- fetching market data or inventing benchmark observations;
- revising OVR-302 or OVR-304 canonical schemas or historical identities;
- choosing a benchmark based on evaluation performance;
- ranking strategies, promoting candidates, allocating capital, scheduling,
  live activation, provider calls, order submission, or UI/AI authority;
- treating synthetic local qualification as investable evidence.

## Locked contracts

1. A declaration binds exactly one declared experiment, its manifest, one
   benchmark instrument, benchmark kind, weighting, distribution treatment,
   cash convention, frequency, window, initial notional, and ordered evidence.
2. Benchmark evidence is point-in-time, strictly ordered, spans the experiment
   evaluation window, uses canonical decimals, and pins source ID/SHA-256 for
   every observation. A caller cannot provide derived returns.
3. The benchmark cannot be selected after seeing an evaluation: the service
   accepts only a declaration whose experiment/window/manifest match the exact
   evaluation parent and whose full curve/cash series match every observation.
4. Opportunity-cost outputs are derived only from the exact evaluation and
   declaration. Callers cannot provide returns, pass/fail status, rankings, or
   recommendations.
5. Strategy total return is after-cost terminal equity over initial equity.
   Benchmark total return is terminal benchmark value over initial value. Cash
   total return compounds the explicit per-period cash returns. Opportunity
   costs are benchmark-minus-strategy and cash-minus-strategy.
6. Values and returns use deterministic decimal arithmetic and the declaration's
   bounded scale. No floating-point result enters canonical identity.
7. Identical writers converge. Changed retries, missing/reordered children,
   normalized/canonical divergence, forged parents, and partial graphs fail.
8. Persistence is append-only. There is no latest/best benchmark query, mutable
   experiment pointer, writable score, current winner, or silent backfill.
9. AI/UI/operator callers can cite completed evidence but cannot alter inputs or
   derived outputs, select winners, or bypass exact-parent validation.
10. Migration 82 is additive, lock-first, and empty-only reversible.

## Task 1: Declaration and opportunity-cost domain

- [x] Add immutable benchmark declaration, ordered observation evidence,
  canonical restoration, and exact experiment-bound validation.
- [x] Add deterministic opportunity-cost report construction from one exact
  OVR-304 evaluation and declaration.
- [x] Prove benchmark/cash calculations, curve alignment, parent mismatch,
  timestamp/value/source tamper, ordering, clone, and stable identity cases.
- [x] Commit and push the domain/calculator slice after focused races.

## Task 2: Migration 82 append-only evidence

- [x] Add declaration, observation, and opportunity-cost-report tables.
- [x] Independently reconstruct canonical bytes, IDs/hashes, exact parent
  hashes, normalized children, and all derived values.
- [x] Reject mutation/deletion, omission/reordering, changed retries, forged
  parents/results, and incomplete graphs.
- [x] Add no selector, scheduler/runtime trigger, promotion/allocation mutation,
  provider writer, execution grant, UI authority, or legacy backfill.
- [x] Add empty-only rollback and bump `RequiredSchemaVersion` to 82 only after
  real PostgreSQL migration tests pass.
- [x] Commit and push the migration slice after focused database races.

## Task 3: Repository, service, recovery, and qualification

- [x] Reload exact experiment/evaluation parents, register declarations,
  evaluate, atomically append reports, and reconstruct normalized rows.
- [x] List evidence only by explicit experiment, evaluation, declaration, or
  benchmark instrument; expose no best/latest/winner query.
- [x] Prove eight-writer convergence, restart reload, every-stage interruption
  rollback, changed retry conflict, normalized forgery rejection, and replay.
- [x] Retain a local benchmark declaration/report and record exact IDs/hashes,
  row counts, return values, and authority separation.
- [x] Add a runbook for declaration review, evidence inspection, report replay,
  failures, rollback, and explicit non-activation boundaries.
- [x] Apply fresh `1 -> 82`, retain evidence, prove nonempty rollback refusal,
  and separately rehearse empty `82 -> 81 -> 82`.
- [x] Run focused/database races, backend and pinned frontend gates, diff review,
  and isolated kill-switched schema-82 health/API/rollback/reapply smoke.
- [x] Commit/push verified slices, fetch, and prove `0 0` divergence before
  OVR-402.

## Acceptance evidence to record

- [x] Every evaluation admitted to OVR-401 reports opportunity cost against an
  experiment-declared benchmark and explicit cash control.
- [x] Benchmark identity, curve, cash series, manifest, window, and source
  evidence are exact and reproducible; substitution or partial evidence fails.
- [x] Reports are append-only, restart-safe, independently reconstructed, and
  do not select, promote, schedule, allocate, deploy, or execute.
- [x] AI/UI/operator recommendations cannot bypass deterministic derivation.
- [x] Local qualification is `VERIFIED_LOCAL`; real benchmark data, independent
  review, shared migration, strategy/runtime adoption, and production activation
  remain `BLOCKED_EXTERNAL`.

## Qualification record — 2026-08-20

- `VERIFIED_LOCAL`: retained isolated schema `augr_ovr401_qual_20260820`
  contains declaration `2a67bc1d-294b-ad4d-88ab-791d010fac44`
  (`f2f676d65a4bc0ae6d7314046dcae55b8b8942674198bd6909b92eade3da6e66`)
  and report `aed8cbe3-6bfd-517d-625a-84ae06d3ff7c`
  (`1e82d44857854f3e09535fae17c45643d2f663a6b60eb13f0b81c58ab3413625`).
- The report binds evaluation `0cb25923-2ffe-dd7d-91f7-06bf3a80e7de`,
  experiment `7fbf9b4c-44a2-75e7-ac7d-544f5a13e46a`, manifest
  `e3d60e30-1355-0370-2fd3-0f1eecd7fda8`, and benchmark instrument
  `496e937a-39aa-4684-b5f8-1bdd735c7d2a`. Retained normalized counts are
  `1/6/1` declaration/observation/report rows.
- Derived returns are strategy `0.006000000000`, benchmark `0.005000000000`,
  and cash `0.000005000010`; opportunity costs are benchmark
  `-0.001000000000` and cash `-0.005994999990`. No caller supplied these
  values, a pass/fail status, a winner, or a lifecycle action.
- Eight-writer convergence, exact restart reload, every-stage rollback,
  normalized forgery rejection, append-only refusal, curve/source/parent
  substitution rejection, and nonempty migration refusal passed. Empty real
  PostgreSQL rehearsal passed `82 -> 81 -> 82`.
- All 4,519 Go race-tested cases passed. Backend build, vet, golangci-lint,
  gofumpt, and govulncheck passed with zero reachable vulnerabilities. Pinned
  Node `22.23.2` frozen install, high-severity audit, 162 tests, lint, and build
  passed; one low-severity Windows-only esbuild development-server advisory
  remains. One initial full frontend run exposed a transient risk-console test
  failure; its isolated rerun and the complete 162-test rerun passed.
- The isolated production image passed fresh migrations `1 -> 82`, schema
  verification, authenticated read-only API health, rollback to 60,
  backup/restore, reapplication through 82, and restarted health with scheduler,
  live trading, provider automation, and benchmark runtime adoption disabled.
- `BLOCKED_EXTERNAL`: real benchmark selection and data, independent review,
  shared migration, runtime adoption, candidate comparison, allocation,
  scheduling, deployment, and production activation.
